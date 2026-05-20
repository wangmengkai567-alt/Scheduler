// Package scheduler 提供定时任务调度功能
// 设计意图：基于 robfig/cron 实现任务调度，支持秒级 cron 表达式
package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"gtask-scheduler/internal/model"
	mysqlstore "gtask-scheduler/internal/store/mysql"
	"gtask-scheduler/internal/worker"
	"gtask-scheduler/pkg/logger"
)

// Scheduler 任务调度器
// 负责定时扫描数据库中的任务并提交给 Worker Pool 执行
// 并发安全：通过 mutex 保护共享状态
type Scheduler struct {
	cron       *cron.Cron                 // cron 调度器
	taskStore  mysqlstore.TaskStoreInterface // 任务存储
	workerPool *worker.Pool               // Worker Pool
	entryMap   map[uint]cron.EntryID      // 任务 ID 到 cron EntryID 的映射
	mu         sync.RWMutex               // 保护 entryMap 的读写锁
	running    bool                       // 是否运行中
	ctx        context.Context            // 用于取消
	cancel     context.CancelFunc         // 取消函数
}

// SchedulerOption 调度器配置选项
type SchedulerOption func(*Scheduler)

// WithTimezone 设置调度器时区
func WithTimezone(loc *time.Location) SchedulerOption {
	return func(s *Scheduler) {
		s.cron = cron.New(cron.WithLocation(loc), cron.WithSeconds())
	}
}

// NewScheduler 创建调度器
// 参数：
//   - taskStore: 任务存储，用于获取和更新任务
//   - workerPool: Worker Pool，用于执行任务
//   - opts: 可选配置
func NewScheduler(taskStore mysqlstore.TaskStoreInterface, workerPool *worker.Pool, opts ...SchedulerOption) *Scheduler {
	ctx, cancel := context.WithCancel(context.Background())

	s := &Scheduler{
		cron:       cron.New(cron.WithSeconds()), // 支持秒级 cron
		taskStore:  taskStore,
		workerPool: workerPool,
		entryMap:   make(map[uint]cron.EntryID),
		ctx:        ctx,
		cancel:     cancel,
	}

	// 应用可选配置
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Start 启动调度器
// 1. 启动 cron 调度器
// 2. 从数据库加载所有待执行的任务
// 3. 启动结果处理协程
func (s *Scheduler) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	logger.Info("starting scheduler")

	// 启动 cron 调度器
	s.cron.Start()

	// 从数据库加载任务
	// 注意：loadTasks 内部会调用 scheduleTask，而 scheduleTask 需要获取锁
	// 因此不能在持有锁的情况下调用 loadTasks，否则会造成死锁
	if err := s.loadTasks(); err != nil {
		s.cron.Stop()
		return err
	}

	// 启动结果处理协程
	go s.processResults()

	s.mu.Lock()
	s.running = true
	count := len(s.entryMap)
	s.mu.Unlock()

	logger.Info("scheduler started", zap.Int("tasks", count))

	return nil
}

// loadTasks 从数据库加载待执行的任务
// 只加载 pending 状态的任务
func (s *Scheduler) loadTasks() error {
	// 获取所有活跃任务
	tasks, err := s.taskStore.ListActive(s.ctx)
	if err != nil {
		return err
	}

	for _, task := range tasks {
		if err := s.scheduleTask(task); err != nil {
			logger.Error("failed to schedule task",
				zap.Uint("task_id", task.ID),
				zap.Error(err),
			)
		}
	}

	return nil
}

// scheduleTask 调度单个任务
// 将任务添加到 cron 调度器中
func (s *Scheduler) scheduleTask(task *model.Task) error {
	// 检查任务是否已调度
	s.mu.RLock()
	_, exists := s.entryMap[task.ID]
	s.mu.RUnlock()

	if exists {
		return nil
	}

	// 创建任务执行的闭包
	// 每次执行时从数据库重新获取任务，确保数据最新
	job := s.createJob(task)

	// 添加到 cron 调度器
	entryID, err := s.cron.AddFunc(task.CronExpr, job)
	if err != nil {
		return err
	}

	// 记录映射关系
	s.mu.Lock()
	s.entryMap[task.ID] = entryID
	s.mu.Unlock()

	logger.Info("task scheduled",
		zap.Uint("task_id", task.ID),
		zap.String("name", task.Name),
		zap.String("cron", task.CronExpr),
	)

	return nil
}

// createJob 创建任务执行的闭包
// 每次触发时提交任务到 Worker Pool
func (s *Scheduler) createJob(task *model.Task) func() {
	return func() {
		// 从数据库重新获取任务，确保状态最新
		// 避免：任务已被删除或禁用，但仍在执行
		currentTask, err := s.taskStore.GetByID(s.ctx, int64(task.ID))
		if err != nil {
			logger.Error("failed to get task for execution",
				zap.Uint("task_id", task.ID),
				zap.Error(err),
			)
			return
		}

		// 检查任务是否可执行
		if !currentTask.IsRunnable() {
			logger.Debug("task is not runnable, skipping",
				zap.Uint("task_id", task.ID),
				zap.Int("status", int(currentTask.Status)),
			)
			return
		}

		// 更新下次执行时间
		nextRun := s.calculateNextRun(currentTask.CronExpr)
		if nextRun != nil {
			_ = s.taskStore.UpdateNextRunAt(s.ctx, int64(currentTask.ID), *nextRun)
		}

		// 提交到 Worker Pool
		job := worker.Job{
			TaskID:  currentTask.ID,
			Name:    currentTask.Name,
			Payload: currentTask.Payload,
		}

		if !s.workerPool.Submit(job) {
			logger.Warn("failed to submit task to worker pool",
				zap.Uint("task_id", currentTask.ID),
			)
		}
	}
}

// calculateNextRun 计算下次执行时间
func (s *Scheduler) calculateNextRun(cronExpr string) *time.Time {
	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return nil
	}
	next := schedule.Next(time.Now())
	return &next
}

// processResults 处理任务执行结果
// 从 Worker Pool 的结果通道读取结果并更新数据库
func (s *Scheduler) processResults() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case result, ok := <-s.workerPool.Results():
			if !ok {
				return
			}

			// 根据结果更新任务状态
			var status model.TaskStatus

			if result.Success {
				status = model.StatusCompleted
			} else {
				status = model.StatusFailed
			}

			if err := s.taskStore.UpdateStatus(s.ctx, int64(result.TaskID), status); err != nil {
				logger.Error("failed to update task status",
					zap.Uint("task_id", result.TaskID),
					zap.Error(err),
				)
			}
		}
	}
}

// AddTask 添加任务到调度器
// 用于动态添加新任务
func (s *Scheduler) AddTask(task *model.Task) error {
	return s.scheduleTask(task)
}

// RemoveTask 从调度器移除任务
// 用于动态删除或暂停任务
func (s *Scheduler) RemoveTask(taskID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entryID, exists := s.entryMap[taskID]
	if !exists {
		return nil
	}

	s.cron.Remove(entryID)
	delete(s.entryMap, taskID)

	logger.Info("task removed from scheduler", zap.Uint("task_id", taskID))
	return nil
}

// Stop 停止调度器
// 优雅停止 cron 调度器
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	logger.Info("stopping scheduler")

	// 取消 context，停止结果处理协程
	s.cancel()

	// 停止 cron 调度器
	// cron.Stop() 会等待当前正在执行的任务完成
	ctx := s.cron.Stop()
	<-ctx.Done()

	s.running = false
	logger.Info("scheduler stopped")
}

// IsRunning 返回调度器是否运行中
func (s *Scheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// TaskCount 返回已调度的任务数量
func (s *Scheduler) TaskCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entryMap)
}
