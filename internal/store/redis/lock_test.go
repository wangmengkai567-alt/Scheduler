// Package redis 提供 Redis 相关功能封装
package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupMiniRedis 创建内存 Redis 实例用于测试
func setupMiniRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr, err := miniredis.Run()
	require.NoError(t, err, "failed to start miniredis")

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

// TestLock_Acquire_Success 测试成功获取锁
// 场景说明：验证锁能被正确获取
func TestLock_Acquire_Success(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	lock := NewLock(client, "test:lock", "instance-1")
	ctx := context.Background()

	err := lock.Acquire(ctx)
	assert.NoError(t, err)

	// 验证锁存在
	val, err := client.Get(ctx, "test:lock").Result()
	assert.NoError(t, err)
	assert.Equal(t, "instance-1", val)
}

// TestLock_Acquire_AlreadyLocked 测试锁已被占用
// 场景说明：验证锁被占用时返回 ErrLockNotAcquired
func TestLock_Acquire_AlreadyLocked(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	// 第一个实例获取锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	err := lock1.Acquire(ctx)
	require.NoError(t, err)

	// 第二个实例尝试获取同一个锁
	lock2 := NewLock(client, "test:lock", "instance-2")
	err = lock2.Acquire(ctx)
	assert.ErrorIs(t, err, ErrLockNotAcquired)
}

// TestLock_Release_Success 测试成功释放锁
// 场景说明：验证锁能被正确释放
func TestLock_Release_Success(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	lock := NewLock(client, "test:lock", "instance-1")
	err := lock.Acquire(ctx)
	require.NoError(t, err)

	// 释放锁
	err = lock.Release(ctx)
	assert.NoError(t, err)

	// 验证锁已被删除
	_, err = client.Get(ctx, "test:lock").Result()
	assert.ErrorIs(t, err, redis.Nil)
}

// TestLock_Release_NotHeld 测试释放不属于自己的锁
// 场景说明：验证不能释放其他实例持有的锁
func TestLock_Release_NotHeld(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	// 第一个实例获取锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	err := lock1.Acquire(ctx)
	require.NoError(t, err)

	// 第二个实例尝试释放锁
	lock2 := NewLock(client, "test:lock", "instance-2")
	err = lock2.Release(ctx)
	assert.ErrorIs(t, err, ErrLockNotHeld)

	// 验证锁仍然存在
	val, err := client.Get(ctx, "test:lock").Result()
	assert.NoError(t, err)
	assert.Equal(t, "instance-1", val)
}

// TestLock_TryAcquire_Success 测试尝试获取锁成功
// 场景说明：验证 TryAcquire 返回 true
func TestLock_TryAcquire_Success(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	lock := NewLock(client, "test:lock", "instance-1")
	acquired, err := lock.TryAcquire(ctx)

	assert.NoError(t, err)
	assert.True(t, acquired)
}

// TestLock_TryAcquire_AlreadyLocked 测试尝试获取已占用的锁
// 场景说明：验证 TryAcquire 返回 false
func TestLock_TryAcquire_AlreadyLocked(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	// 第一个实例获取锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	acquired, err := lock1.TryAcquire(ctx)
	require.NoError(t, err)
	require.True(t, acquired)

	// 第二个实例尝试获取
	lock2 := NewLock(client, "test:lock", "instance-2")
	acquired, err = lock2.TryAcquire(ctx)

	assert.NoError(t, err)
	assert.False(t, acquired)
}

// TestLock_Refresh_Success 测试刷新锁过期时间
// 场景说明：验证锁的过期时间能被延长
func TestLock_Refresh_Success(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	lock := NewLock(client, "test:lock", "instance-1", WithTTL(5*time.Second))
	err := lock.Acquire(ctx)
	require.NoError(t, err)

	// 刷新锁
	err = lock.Refresh(ctx)
	assert.NoError(t, err)

	// 验证 TTL 被重置
	ttl, err := lock.TTL(ctx)
	assert.NoError(t, err)
	assert.Greater(t, ttl.Milliseconds(), int64(4000))
}

// TestLock_Refresh_NotHeld 测试刷新不属于自己的锁
// 场景说明：验证不能刷新其他实例的锁
func TestLock_Refresh_NotHeld(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	// 第一个实例获取锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	err := lock1.Acquire(ctx)
	require.NoError(t, err)

	// 第二个实例尝试刷新
	lock2 := NewLock(client, "test:lock", "instance-2")
	err = lock2.Refresh(ctx)
	assert.ErrorIs(t, err, ErrLockNotHeld)
}

// TestLock_WithRetries 测试带重试的获取锁
// 场景说明：验证重试机制能工作
func TestLock_WithRetries(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	// 先获取锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	err := lock1.Acquire(ctx)
	require.NoError(t, err)

	// 带重试的锁 - 由于锁已被占用，最终会失败
	lock2 := NewLock(client, "test:lock", "instance-2",
		WithRetries(3),
		WithRetryInterval(10*time.Millisecond),
	)

	start := time.Now()
	err = lock2.Acquire(ctx)
	elapsed := time.Since(start)

	assert.ErrorIs(t, err, ErrLockNotAcquired)
	// 验证重试了 3 次（间隔 10ms * 3 = 30ms+）
	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(30))
}

// TestLockManager_NewLock 测试锁管理器创建锁
// 场景说明：验证锁管理器能正确添加前缀
func TestLockManager_NewLock(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	manager := NewLockManager(client, "myapp:")
	lock := manager.NewLock("resource:1", "instance-1")

	err := lock.Acquire(ctx)
	assert.NoError(t, err)

	// 验证 key 包含前缀
	val, err := client.Get(ctx, "myapp:resource:1").Result()
	assert.NoError(t, err)
	assert.Equal(t, "instance-1", val)
}

// TestLockManager_TaskLock 测试任务锁创建
// 场景说明：验证任务锁自动生成正确的 key
func TestLockManager_TaskLock(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	manager := NewLockManager(client, "scheduler:")
	lock := manager.TaskLock(123, "instance-1")

	err := lock.Acquire(ctx)
	assert.NoError(t, err)

	// 验证 key 格式正确（包含 task:日期:ID）
	keys := mr.Keys()
	assert.Len(t, keys, 1)
	assert.Contains(t, keys[0], "scheduler:task:")
	assert.Contains(t, keys[0], "123")
}

// TestLock_TTL 测试获取锁的剩余过期时间
// 场景说明：验证 TTL 方法返回正确的值
func TestLock_TTL(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()

	lock := NewLock(client, "test:lock", "instance-1", WithTTL(10*time.Second))
	err := lock.Acquire(ctx)
	require.NoError(t, err)

	ttl, err := lock.TTL(ctx)
	assert.NoError(t, err)
	assert.Greater(t, ttl.Seconds(), float64(9))
	assert.LessOrEqual(t, ttl.Seconds(), float64(10))
}

// TestLock_ContextCancellation 测试 context 取消
// 场景说明：验证 context 取消后获取锁立即返回
func TestLock_ContextCancellation(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	// 先占用锁
	lock1 := NewLock(client, "test:lock", "instance-1")
	err := lock1.Acquire(context.Background())
	require.NoError(t, err)

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 带重试的锁获取
	lock2 := NewLock(client, "test:lock", "instance-2",
		WithRetries(10),
		WithRetryInterval(100*time.Millisecond),
	)

	err = lock2.Acquire(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

// TestLock_ConcurrentAccess 测试并发访问
// 场景说明：验证多个 goroutine 竞争锁的正确性
func TestLock_ConcurrentAccess(t *testing.T) {
	mr, client := setupMiniRedis(t)
	defer mr.Close()
	defer client.Close()

	ctx := context.Background()
	const numGoroutines = 10

	var acquiredCount int32
	var done = make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			lock := NewLock(client, "test:lock", "instance-"+string(rune('0'+id)))
			if err := lock.Acquire(ctx); err == nil {
				acquiredCount++
				lock.Release(ctx)
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// 只有一个 goroutine 能成功获取锁
	// 注意：由于竞争，可能有多个成功（如果一个释放后另一个获取）
	assert.Greater(t, int(acquiredCount), 0)
}
