// Package main 提供 Worker Pool 性能压测
// 运行方式：go run scripts/bench_pool.go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"gtask-scheduler/internal/worker"
)

// 压测配置
const (
	WorkerCount    = 10               // worker 数量
	QueueSize      = 1000             // 队列大小
	TestDuration   = 10 * time.Second // 压测时长
	WarmupDuration = 2 * time.Second  // 预热时长
)

// 统计数据
type Stats struct {
	mu           sync.Mutex
	latencies    []time.Duration
	totalTasks   int64
	successTasks int64
	failedTasks  int64
}

func NewStats() *Stats {
	return &Stats{
		latencies: make([]time.Duration, 0, 100000),
	}
}

func (s *Stats) RecordLatency(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latencies = append(s.latencies, d)
}

func (s *Stats) IncrTotal() {
	atomic.AddInt64(&s.totalTasks, 1)
}

func (s *Stats) IncrSuccess() {
	atomic.AddInt64(&s.successTasks, 1)
}

func (s *Stats) IncrFailed() {
	atomic.AddInt64(&s.failedTasks, 1)
}

func (s *Stats) Report() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.latencies) == 0 {
		fmt.Println("\n⚠️ 没有收集到延迟数据")
		return
	}

	// 排序计算百分位
	sort.Slice(s.latencies, func(i, j int) bool {
		return s.latencies[i] < s.latencies[j]
	})

	total := atomic.LoadInt64(&s.totalTasks)
	failed := atomic.LoadInt64(&s.failedTasks)

	// 计算统计指标
	p50 := s.latencies[len(s.latencies)*50/100]
	p90 := s.latencies[len(s.latencies)*90/100]
	p95 := s.latencies[len(s.latencies)*95/100]
	p99 := s.latencies[len(s.latencies)*99/100]
	avg := time.Duration(0)
	for _, l := range s.latencies {
		avg += l
	}
	avg = time.Duration(int64(avg) / int64(len(s.latencies)))

	// 计算实际 QPS（使用延迟记录数作为成功处理的任务数）
	successCount := int64(len(s.latencies))
	actualQPS := float64(successCount) / TestDuration.Seconds()

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📊 Worker Pool 性能压测报告")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("🔧 配置: Worker=%d, QueueSize=%d, Duration=%v\n", WorkerCount, QueueSize, TestDuration)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("📈 吞吐量:\n")
	fmt.Printf("   QPS:        %.0f req/s\n", actualQPS)
	fmt.Printf("   总任务数:   %d\n", total)
	fmt.Printf("   成功:       %d\n", successCount)
	fmt.Printf("   失败:       %d\n", failed)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("⏱️  延迟统计:\n")
	fmt.Printf("   平均:       %v\n", avg)
	fmt.Printf("   P50:        %v\n", p50)
	fmt.Printf("   P90:        %v\n", p90)
	fmt.Printf("   P95:        %v\n", p95)
	fmt.Printf("   P99:        %v\n", p99)
	fmt.Printf("   最大:       %v\n", s.latencies[len(s.latencies)-1])
	fmt.Println(strings.Repeat("=", 60))

	// 简历格式输出
	fmt.Println("\n📝 简历格式:")
	fmt.Printf("   Worker Pool 在并发度 %d 下，任务吞吐量达到 %.0f req/s，P99 延迟 %.2fms\n",
		WorkerCount, actualQPS, float64(p99.Microseconds())/1000)
}

// 记录延迟的 handler
// 模拟真实任务处理：包含一定的工作负载
type benchmarkHandler struct {
	stats *Stats
}

func (h *benchmarkHandler) Handle(ctx context.Context, taskID uint, payload string) error {
	// 模拟真实任务处理时间
	// 方案1: 简单计算（CPU 密集型）
	// sum := 0
	// for i := 0; i < 1000; i++ {
	// 	sum += i
	// }
	
	// 方案2: 模拟 IO 等待（IO 密集型，更接近真实场景）
	// 休眠 1-5 微秒，模拟网络/数据库延迟
	time.Sleep(time.Duration(1+taskID%5) * time.Microsecond)
	
	h.stats.IncrSuccess()
	return nil
}

// 带时间记录的包装器
type timedHandler struct {
	inner  worker.JobHandler
	stats  *Stats
}

func (h *timedHandler) Handle(ctx context.Context, taskID uint, payload string) error {
	start := time.Now()
	err := h.inner.Handle(ctx, taskID, payload)
	latency := time.Since(start)
	h.stats.RecordLatency(latency)
	return err
}

func main() {
	fmt.Println("🚀 Worker Pool 性能压测")
	fmt.Printf("配置: %d workers, queue size %d, duration %v\n", WorkerCount, QueueSize, TestDuration)

	stats := NewStats()

	// 创建 handler
	baseHandler := &benchmarkHandler{stats: stats}
	timedHandler := &timedHandler{inner: baseHandler, stats: stats}

	// 创建 worker pool
	pool := worker.NewPool(WorkerCount, QueueSize, timedHandler)
	pool.Start()
	defer pool.Stop()

	// 预热
	fmt.Printf("\n🔥 预热中 (%v)...\n", WarmupDuration)
	warmupCtx, warmupCancel := context.WithTimeout(context.Background(), WarmupDuration)
	warmupTasks := int64(0)
	go func() {
		for warmupCtx.Err() == nil {
			job := worker.Job{
				TaskID:  uint(atomic.AddInt64(&warmupTasks, 1)),
				Name:    "warmup-task",
				Payload: `{"type":"warmup"}`,
			}
			pool.Submit(job)
		}
	}()
	time.Sleep(WarmupDuration)
	warmupCancel()
	fmt.Printf("预热完成，提交 %d 个任务\n", warmupTasks)

	// 重置统计
	stats = NewStats()
	timedHandler.stats = stats
	baseHandler.stats = stats

	// 正式压测
	fmt.Printf("\n⚡ 压测中 (%v)...\n", TestDuration)

	// 监听中断信号
	ctx, cancel := context.WithTimeout(context.Background(), TestDuration)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigCh:
			fmt.Println("\n收到中断信号，停止压测...")
			cancel()
		case <-ctx.Done():
		}
	}()

	// 统计提交数量
	submitCount := int64(0)
	rejectCount := int64(0)
	taskID := uint(0)

	for ctx.Err() == nil {
		taskID++
		job := worker.Job{
			TaskID:  taskID,
			Name:    fmt.Sprintf("bench-task-%d", taskID),
			Payload: `{"type":"benchmark"}`,
		}

		if pool.Submit(job) {
			atomic.AddInt64(&submitCount, 1)
			stats.IncrTotal()
		} else {
			atomic.AddInt64(&rejectCount, 1)
		}

		// 避免提交太快导致全部被拒绝
		// 模拟真实场景下一定的请求间隔
		time.Sleep(10 * time.Microsecond)
	}

	fmt.Printf("提交完成: 成功 %d, 拒绝 %d\n", atomic.LoadInt64(&submitCount), atomic.LoadInt64(&rejectCount))

	// 等待任务完成
	time.Sleep(500 * time.Millisecond)

	// 输出报告
	stats.Report()
}
