// Package main 提供 Redis 分布式锁争抢验证
// 运行方式：go run scripts/bench_lock.go
// 验证目标：并发多次触发同一任务，Redis 分布式锁保证仅执行 1 次
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// 配置
const (
	GoroutineCount = 10  // 并发 goroutine 数量
	TestRounds     = 100 // 测试轮数
)

// 模拟任务执行器
type TaskExecutor struct {
	mu          sync.Mutex
	execCount   int32
	executedBy  int32
	lastExecAt  time.Time
}

func NewTaskExecutor() *TaskExecutor {
	return &TaskExecutor{}
}

func (e *TaskExecutor) Execute(instanceID int32) bool {
	// 记录执行
	count := atomic.AddInt32(&e.execCount, 1)
	if count == 1 {
		atomic.StoreInt32(&e.executedBy, instanceID)
		e.mu.Lock()
		e.lastExecAt = time.Now()
		e.mu.Unlock()
		return true
	}
	return false
}

func (e *TaskExecutor) GetExecCount() int32 {
	return atomic.LoadInt32(&e.execCount)
}

// 模拟分布式锁
type DistributedLock struct {
	client   *redis.Client
	key      string
	value    string
	acquired bool
}

func NewDistributedLock(client *redis.Client, key, value string) *DistributedLock {
	return &DistributedLock{
		client: client,
		key:    key,
		value:  value,
	}
}

func (l *DistributedLock) TryAcquire(ctx context.Context) (bool, error) {
	result, err := l.client.SetNX(ctx, l.key, l.value, 10*time.Second).Result()
	if err != nil {
		return false, err
	}
	l.acquired = result
	return result, nil
}

func (l *DistributedLock) Release(ctx context.Context) error {
	if !l.acquired {
		return nil
	}
	// 使用 Lua 脚本安全释放
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`
	_, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Result()
	return err
}

func main() {
	fmt.Println("🔒 Redis 分布式锁争抢验证")
	fmt.Printf("配置: %d 并发 goroutine, %d 测试轮数\n", GoroutineCount, TestRounds)
	fmt.Println()

	// 连接真实 Redis
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer client.Close()

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		fmt.Printf("❌ 连接 Redis 失败: %v\n", err)
		fmt.Println("请确保 Redis 已启动: redis-server")
		os.Exit(1)
	}
	fmt.Println("✅ Redis 连接成功")
	fmt.Println()

	// 统计数据
	var (
		totalAttempts  int32
		successCount   int32
		duplicateCount int32
		roundResults   []int32
	)

	fmt.Println("🚀 开始测试...")
	fmt.Println()

	for round := 1; round <= TestRounds; round++ {
		taskID := fmt.Sprintf("task:%d", round)
		executor := NewTaskExecutor()

		var wg sync.WaitGroup
		var roundSuccess int32
		var roundDuplicate int32

		// 并发尝试获取锁并执行
		for i := 0; i < GoroutineCount; i++ {
			wg.Add(1)
			go func(instanceID int) {
				defer wg.Done()

				atomic.AddInt32(&totalAttempts, 1)

				instanceValue := fmt.Sprintf("instance-%d", instanceID)
				lock := NewDistributedLock(client, taskID, instanceValue)

				acquired, err := lock.TryAcquire(ctx)
				if err != nil {
					fmt.Printf("  ❌ 获取锁失败: %v\n", err)
					return
				}

				if acquired {
					// 获取到锁，执行任务
					executed := executor.Execute(int32(instanceID))
					if executed {
						atomic.AddInt32(&roundSuccess, 1)
					} else {
						// 不应该发生，但如果发生了说明有并发问题
						atomic.AddInt32(&roundDuplicate, 1)
					}

					// 模拟任务执行时间
					time.Sleep(10 * time.Millisecond)

					// 释放锁
					lock.Release(ctx)
				}
				// 没获取到锁的 goroutine 直接退出
			}(i)
		}

		wg.Wait()

		execCount := executor.GetExecCount()
		roundResults = append(roundResults, execCount)

		if execCount == 1 {
			atomic.AddInt32(&successCount, 1)
		} else if execCount > 1 {
			atomic.AddInt32(&duplicateCount, 1)
		}
	}

	// 输出报告
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("📊 Redis 分布式锁验证报告")
	fmt.Println("============================================================")
	fmt.Printf("🔧 配置:\n")
	fmt.Printf("   并发数:     %d goroutines\n", GoroutineCount)
	fmt.Printf("   测试轮数:   %d\n", TestRounds)
	fmt.Printf("   总尝试次数: %d\n", atomic.LoadInt32(&totalAttempts))
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("📈 结果:\n")
	fmt.Printf("   正确执行:   %d 次 (仅 1 个 goroutine 执行)\n", atomic.LoadInt32(&successCount))
	fmt.Printf("   重复执行:   %d 次 (超过 1 个 goroutine 执行)\n", atomic.LoadInt32(&duplicateCount))
	fmt.Printf("   成功率:     %.2f%%\n", float64(atomic.LoadInt32(&successCount))/float64(TestRounds)*100)
	fmt.Println("============================================================")

	// 分析执行分布
	executionDistribution := make(map[int32]int)
	for _, count := range roundResults {
		executionDistribution[count]++
	}
	fmt.Printf("\n📊 执行次数分布:\n")
	for execCount, occurrences := range executionDistribution {
		fmt.Printf("   执行 %d 次: %d 轮\n", execCount, occurrences)
	}

	fmt.Println()

	// 简历格式输出
	fmt.Println("============================================================")
	if atomic.LoadInt32(&duplicateCount) == 0 {
		fmt.Println("✅ 验证通过！")
		fmt.Println()
		fmt.Println("📝 简历格式:")
		fmt.Printf("   并发 %d 次触发同一任务，Redis 分布式锁保证仅执行 1 次，零重复执行\n", GoroutineCount)
	} else {
		fmt.Println("⚠️ 发现重复执行！")
		fmt.Printf("   重复执行次数: %d\n", atomic.LoadInt32(&duplicateCount))
	}
	fmt.Println("============================================================")
}
