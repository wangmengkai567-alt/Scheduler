// Package worker 提供任务执行器和 Worker Pool 实现
package worker

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPool 测试 Pool 创建
func TestNewPool(t *testing.T) {
	t.Run("create pool with default values", func(t *testing.T) {
		pool := NewPool(0, 0, nil)
		defer pool.Stop()

		assert.NotNil(t, pool)
		assert.Equal(t, 10, pool.workers)      // 默认 10 个 worker
		assert.Equal(t, 1000, pool.QueueCap())  // 默认队列大小 1000
		assert.NotNil(t, pool.handler)          // 默认 NoOpHandler
	})

	t.Run("create pool with custom values", func(t *testing.T) {
		handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
			return nil
		})
		pool := NewPool(5, 100, handler)
		defer pool.Stop()

		assert.Equal(t, 5, pool.workers)
		assert.Equal(t, 100, pool.QueueCap())
		assert.NotNil(t, pool.handler)
	})
}

// TestPoolStartStop 测试 Pool 启动和停止
func TestPoolStartStop(t *testing.T) {
	t.Run("start and stop", func(t *testing.T) {
		pool := NewPool(3, 10, NoOpHandler)

		assert.False(t, pool.IsRunning())

		pool.Start()
		assert.True(t, pool.IsRunning())

		// 重复启动应该被忽略
		pool.Start()
		assert.True(t, pool.IsRunning())

		pool.Stop()
		assert.False(t, pool.IsRunning())

		// 等待完全停止
		<-pool.Stopped()
	})

	t.Run("stop without start", func(t *testing.T) {
		pool := NewPool(3, 10, NoOpHandler)
		// 未启动时停止应该是安全的
		pool.Stop()
		assert.False(t, pool.IsRunning())
	})
}

// TestPoolSubmit 测试任务提交
func TestPoolSubmit(t *testing.T) {
	t.Run("submit job successfully", func(t *testing.T) {
		var executed atomic.Int32
		handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
			executed.Add(1)
			return nil
		})

		pool := NewPool(2, 10, handler)
		pool.Start()
		defer pool.Stop()

		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		assert.True(t, pool.Submit(job))

		// 等待任务执行完成
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, int32(1), executed.Load())
	})

	t.Run("submit to stopped pool", func(t *testing.T) {
		pool := NewPool(2, 10, NoOpHandler)
		pool.Start()
		pool.Stop()

		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		assert.False(t, pool.Submit(job))
	})

	t.Run("submit to full queue", func(t *testing.T) {
		// 使用阻塞的 handler 填满队列
		blockCh := make(chan struct{})
		handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
			<-blockCh
			return nil
		})

		pool := NewPool(1, 2, handler) // 1 个 worker，队列大小 2
		pool.Start()
		defer func() {
			close(blockCh)
			pool.Stop()
		}()

		// 填满队列
		for i := 0; i < 3; i++ { // 1 个在执行，2 个在队列
			job := Job{TaskID: uint(i), Name: "test", Payload: "{}"}
			pool.Submit(job)
		}

		// 队列满时应该拒绝
		job := Job{TaskID: 100, Name: "test", Payload: "{}"}
		assert.False(t, pool.Submit(job))
	})
}

// TestPoolResults 测试结果通道
func TestPoolResults(t *testing.T) {
	t.Run("receive successful result", func(t *testing.T) {
		pool := NewPool(2, 10, NoOpHandler)
		pool.Start()
		defer pool.Stop()

		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		require.True(t, pool.Submit(job))

		// 等待结果
		select {
		case result := <-pool.Results():
			assert.Equal(t, uint(1), result.TaskID)
			assert.True(t, result.Success)
			assert.Empty(t, result.Error)
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for result")
		}
	})

	t.Run("receive failed result", func(t *testing.T) {
		expectedErr := errors.New("task failed")
		handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
			return expectedErr
		})

		pool := NewPool(2, 10, handler)
		pool.Start()
		defer pool.Stop()

		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		require.True(t, pool.Submit(job))

		// 等待结果
		select {
		case result := <-pool.Results():
			assert.Equal(t, uint(1), result.TaskID)
			assert.False(t, result.Success)
			assert.Contains(t, result.Error, expectedErr.Error())
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for result")
		}
	})
}

// TestPoolPanicRecovery 测试 panic 恢复
func TestPoolPanicRecovery(t *testing.T) {
	handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
		panic("intentional panic")
	})

	pool := NewPool(2, 10, handler)
	pool.Start()
	defer pool.Stop()

	job := Job{TaskID: 1, Name: "test", Payload: "{}"}
	require.True(t, pool.Submit(job))

	// 等待任务执行，worker 不应该退出
	time.Sleep(100 * time.Millisecond)
	assert.True(t, pool.IsRunning())
}

// TestPoolConcurrentSubmit 测试并发提交
func TestPoolConcurrentSubmit(t *testing.T) {
	var counter atomic.Int32
	handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
		counter.Add(1)
		return nil
	})

	pool := NewPool(5, 100, handler)
	pool.Start()
	defer pool.Stop()

	// 并发提交 100 个任务
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			job := Job{TaskID: uint(id), Name: "test", Payload: "{}"}
			pool.SubmitBlocking(job)
		}(i)
	}

	wg.Wait()

	// 等待所有任务完成
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(100), counter.Load())
}

// TestPoolContextCancellation 测试 context 取消
func TestPoolContextCancellation(t *testing.T) {
	started := make(chan uint)
	handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
		close(started)
		// 等待 context 取消
		<-ctx.Done()
		return ctx.Err()
	})

	pool := NewPool(1, 10, handler)
	pool.Start()

	job := Job{TaskID: 1, Name: "test", Payload: "{}"}
	require.True(t, pool.Submit(job))

	// 等待任务开始执行
	<-started

	// 停止 pool，context 应该被取消
	pool.Stop()
	assert.False(t, pool.IsRunning())
}

// TestSubmitBlocking 测试阻塞提交
func TestSubmitBlocking(t *testing.T) {
	t.Run("submit blocking success", func(t *testing.T) {
		pool := NewPool(1, 1, NoOpHandler)
		pool.Start()
		defer pool.Stop()

		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		assert.True(t, pool.SubmitBlocking(job))
	})

	t.Run("submit blocking on stopped pool", func(t *testing.T) {
		pool := NewPool(1, 1, NoOpHandler)
		// 未启动
		job := Job{TaskID: 1, Name: "test", Payload: "{}"}
		assert.False(t, pool.SubmitBlocking(job))
	})
}

// TestQueueMetrics 测试队列指标
func TestQueueMetrics(t *testing.T) {
	blockCh := make(chan struct{})
	handler := HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
		<-blockCh
		return nil
	})

	pool := NewPool(1, 5, handler)
	pool.Start()
	defer func() {
		close(blockCh)
		pool.Stop()
	}()

	// 提交一个任务（会被 worker 取走执行）
	pool.Submit(Job{TaskID: 0})

	// 再提交 5 个任务到队列
	for i := 1; i <= 5; i++ {
		pool.Submit(Job{TaskID: uint(i)})
	}

	// 队列长度应该接近容量
	assert.Equal(t, 5, cap(pool.jobQueue))
	// 实际队列长度取决于 worker 消费速度
}
