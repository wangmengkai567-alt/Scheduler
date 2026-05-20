// Package worker 提供任务执行器和 Worker Pool 实现
// 设计意图：通过固定数量的 worker 处理任务，控制并发度，避免资源耗尽
package worker

import (
	"context"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"

	"gtask-scheduler/pkg/logger"
)

// Pool Worker Pool 结构
// 管理固定数量的 worker goroutine，处理任务队列中的任务
// 并发安全：通过 channel 和 sync.WaitGroup 保证
type Pool struct {
	jobQueue  chan Job         // 任务队列，缓冲大小可配置
	resultCh  chan JobResult   // 结果队列，用于通知外部
	handler   JobHandler       // 任务处理器
	workers   int              // worker 数量
	wg        sync.WaitGroup   // 等待所有 worker 退出
	ctx       context.Context  // 用于取消所有 worker
	cancel    context.CancelFunc // 取消函数
	running   atomic.Bool      // 是否运行中（原子操作保证并发安全）
	stoppedCh chan struct{}    // 用于通知 pool 完全停止
}

// NewPool 创建 Worker Pool
// 参数：
//   - workers: worker 数量，建议根据 CPU 核心数设置
//   - queueSize: 任务队列大小，建议根据任务产生速率设置
//   - handler: 任务处理器，不能为 nil
func NewPool(workers, queueSize int, handler JobHandler) *Pool {
	if handler == nil {
		handler = NoOpHandler
	}
	if workers <= 0 {
		workers = 10 // 默认 10 个 worker
	}
	if queueSize <= 0 {
		queueSize = 1000 // 默认队列大小 1000
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		jobQueue:  make(chan Job, queueSize),
		resultCh:  make(chan JobResult, queueSize),
		handler:   handler,
		workers:   workers,
		ctx:       ctx,
		cancel:    cancel,
		stoppedCh: make(chan struct{}),
	}
}

// Start 启动 Worker Pool
// 创建指定数量的 worker goroutine 开始消费任务
// 并发安全：通过 atomic.Bool 保证只能启动一次
func (p *Pool) Start() {
	// CAS 操作保证只启动一次
	if !p.running.CompareAndSwap(false, true) {
		logger.Warn("worker pool already running")
		return
	}

	logger.Info("starting worker pool", zap.Int("workers", p.workers))

	// 启动 worker goroutine
	// 每个 worker 独立消费任务队列，直到收到停止信号
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker 是实际执行任务的 goroutine
// 持续从 jobQueue 获取任务，直到 context 被取消
func (p *Pool) worker(id int) {
	defer p.wg.Done()

	// 使用 Named logger 区分不同 worker
	log := logger.Named("worker").With(zap.Int("id", id))
	log.Debug("worker started")

	for {
		select {
		case <-p.ctx.Done():
			// 收到停止信号，退出 goroutine
			log.Debug("worker stopped")
			return

		case job, ok := <-p.jobQueue:
			if !ok {
				// channel 关闭，退出 goroutine
				log.Debug("job queue closed, worker exiting")
				return
			}

			log.Debug("processing job", zap.Uint("task_id", job.TaskID), zap.String("name", job.Name))

			// 执行任务
			result := p.executeJob(job)

			// 发送结果，使用 select 防止阻塞
			// 如果 resultCh 已满或已关闭，丢弃结果避免阻塞 worker
			select {
			case p.resultCh <- result:
			default:
				log.Warn("result channel full, dropping result", zap.Uint("task_id", job.TaskID))
			}
		}
	}
}

// executeJob 执行单个任务
// 包含 panic 恢复，防止单个任务失败导致 worker 退出
func (p *Pool) executeJob(job Job) JobResult {
	// 使用 defer + recover 捕获 panic
	// 防止单个任务 panic 导致整个 worker 退出
	defer func() {
		if r := recover(); r != nil {
			logger.Error("job panic recovered",
				zap.Uint("task_id", job.TaskID),
				zap.Any("panic", r),
			)
		}
	}()

	err := p.handler.Handle(p.ctx, job.TaskID, job.Payload)
	if err != nil {
		logger.Error("job failed",
			zap.Uint("task_id", job.TaskID),
			zap.Error(err),
		)
		return JobResult{
			TaskID:  job.TaskID,
			Success: false,
			Error:   err.Error(),
		}
	}

	return JobResult{
		TaskID:  job.TaskID,
		Success: true,
	}
}

// Submit 提交任务到队列
// 非阻塞：如果队列满，返回 false
// 并发安全：channel 本身是并发安全的
func (p *Pool) Submit(job Job) bool {
	if !p.running.Load() {
		logger.Warn("pool not running, job rejected", zap.Uint("task_id", job.TaskID))
		return false
	}

	select {
	case p.jobQueue <- job:
		return true
	default:
		// 队列满，拒绝任务
		logger.Warn("job queue full, job rejected", zap.Uint("task_id", job.TaskID))
		return false
	}
}

// SubmitBlocking 阻塞提交任务
// 会一直等待直到队列有空位或 pool 停止
func (p *Pool) SubmitBlocking(job Job) bool {
	if !p.running.Load() {
		return false
	}

	select {
	case p.jobQueue <- job:
		return true
	case <-p.ctx.Done():
		return false
	}
}

// Results 返回结果 channel
// 调用方应持续读取，否则会导致 worker 阻塞
func (p *Pool) Results() <-chan JobResult {
	return p.resultCh
}

// Stop 优雅停止 Worker Pool
// 1. 设置停止标志，拒绝新任务
// 2. 取消 context，通知所有 worker 停止
// 3. 等待所有 worker 处理完当前任务后退出
// 4. 关闭 channel，释放资源
func (p *Pool) Stop() {
	// CAS 操作保证只停止一次
	if !p.running.CompareAndSwap(true, false) {
		return
	}

	logger.Info("stopping worker pool")

	// 取消 context，通知所有 worker 停止
	p.cancel()

	// 等待所有 worker 退出
	// 这确保正在执行的任务能够完成
	p.wg.Wait()

	// 关闭 channel，释放资源
	// 必须在所有 worker 退出后关闭，避免向已关闭的 channel 发送数据
	close(p.jobQueue)
	close(p.resultCh)

	// 通知停止完成
	close(p.stoppedCh)

	logger.Info("worker pool stopped")
}

// Stopped 返回停止通知 channel
// 用于外部等待 pool 完全停止
func (p *Pool) Stopped() <-chan struct{} {
	return p.stoppedCh
}

// IsRunning 返回 pool 是否在运行
func (p *Pool) IsRunning() bool {
	return p.running.Load()
}

// QueueLen 返回当前队列长度
// 用于监控和负载评估
func (p *Pool) QueueLen() int {
	return len(p.jobQueue)
}

// QueueCap 返回队列容量
func (p *Pool) QueueCap() int {
	return cap(p.jobQueue)
}
