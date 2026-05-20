// Package redis 提供 Redis 相关功能封装
// 设计意图：实现分布式锁，保证多实例部署时的任务唯一性
package redis

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// Lock 分布式锁
// 基于 Redis SET NX EX 实现，保证原子性
// 并发安全：通过 Redis 单线程特性保证
type Lock struct {
	client   *redis.Client // Redis 客户端
	key      string        // 锁的 key
	value    string        // 锁的值（用于安全释放）
	ttl      time.Duration // 锁的过期时间
	retries  int           // 获取锁的重试次数
	interval time.Duration // 重试间隔
}

// 锁相关的错误
var (
	ErrLockNotAcquired = errors.New("lock not acquired")
	ErrLockNotHeld     = errors.New("lock not held by this instance")
)

// LockOption 锁配置选项
type LockOption func(*Lock)

// WithTTL 设置锁的过期时间
// 建议根据任务执行时间设置，避免任务未完成锁就过期
func WithTTL(ttl time.Duration) LockOption {
	return func(l *Lock) {
		l.ttl = ttl
	}
}

// WithRetries 设置获取锁的重试次数
// 用于实现自旋锁效果
func WithRetries(retries int) LockOption {
	return func(l *Lock) {
		l.retries = retries
	}
}

// WithRetryInterval 设置重试间隔
func WithRetryInterval(interval time.Duration) LockOption {
	return func(l *Lock) {
		l.interval = interval
	}
}

// NewLock 创建分布式锁
// 参数：
//   - client: Redis 客户端
//   - key: 锁的 key，建议使用业务前缀，如 "task:lock:123"
//   - value: 锁的值，用于标识锁的持有者，建议使用唯一标识如 UUID
//   - opts: 可选配置
func NewLock(client *redis.Client, key, value string, opts ...LockOption) *Lock {
	lock := &Lock{
		client:   client,
		key:      key,
		value:    value,
		ttl:      30 * time.Second, // 默认 30 秒过期
		retries:  0,                // 默认不重试
		interval: 100 * time.Millisecond,
	}

	for _, opt := range opts {
		opt(lock)
	}

	return lock
}

// Acquire 尝试获取锁
// 使用 SET NX EX 原子操作，确保只有一个客户端能获取锁
// 返回 ErrLockNotAcquired 表示获取失败
func (l *Lock) Acquire(ctx context.Context) error {
	// 如果设置了重试，则循环尝试
	for i := 0; i <= l.retries; i++ {
		// SET key value NX EX ttl
		// NX: 只在 key 不存在时设置
		// EX: 设置过期时间（秒）
		acquired, err := l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
		if err != nil {
			return err
		}

		if acquired {
			return nil
		}

		// 未获取到锁，如果还有重试次数则等待
		if i < l.retries {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(l.interval):
				continue
			}
		}
	}

	return ErrLockNotAcquired
}

// TryAcquire 尝试获取锁，不重试
// 用于快速判断是否能获取锁的场景
func (l *Lock) TryAcquire(ctx context.Context) (bool, error) {
	return l.client.SetNX(ctx, l.key, l.value, l.ttl).Result()
}

// Release 释放锁
// 使用 Lua 脚本保证原子性，只释放自己持有的锁
// 避免：A 的锁过期后被 B 获取，A 误释放了 B 的锁
func (l *Lock) Release(ctx context.Context) error {
	// Lua 脚本：只有 value 匹配时才删除
	// KEYS[1]: lock key
	// ARGV[1]: expected value
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("DEL", KEYS[1])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{l.key}, l.value).Int()
	if err != nil {
		return err
	}

	if result == 0 {
		return ErrLockNotHeld
	}

	return nil
}

// Refresh 刷新锁的过期时间
// 用于任务执行时间超过预期时延长锁的有效期
// 使用 Lua 脚本保证原子性
func (l *Lock) Refresh(ctx context.Context) error {
	script := `
		if redis.call("GET", KEYS[1]) == ARGV[1] then
			return redis.call("PEXPIRE", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := l.client.Eval(ctx, script, []string{l.key}, l.value, int(l.ttl.Milliseconds())).Int()
	if err != nil {
		return err
	}

	if result == 0 {
		return ErrLockNotHeld
	}

	return nil
}

// TTL 获取锁的剩余过期时间
func (l *Lock) TTL(ctx context.Context) (time.Duration, error) {
	return l.client.TTL(ctx, l.key).Result()
}

// LockManager 锁管理器
// 提供锁的创建和管理功能
type LockManager struct {
	client *redis.Client
	prefix string // key 前缀
}

// NewLockManager 创建锁管理器
func NewLockManager(client *redis.Client, prefix string) *LockManager {
	return &LockManager{
		client: client,
		prefix: prefix,
	}
}

// NewLock 创建锁
// 自动添加前缀，简化使用
func (m *LockManager) NewLock(key, value string, opts ...LockOption) *Lock {
	fullKey := m.prefix + key
	return NewLock(m.client, fullKey, value, opts...)
}

// TaskLock 为任务创建分布式锁
// 便捷方法，自动生成锁的 key
func (m *LockManager) TaskLock(taskID uint, instanceID string) *Lock {
	key := "task:" + time.Now().Format("20060102") + ":" + itoa(taskID)
	return m.NewLock(key, instanceID, WithTTL(60*time.Second))
}

// itoa 快速整数转字符串
func itoa(i uint) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	// 简单实现，生产环境可用 strconv.Itoa
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
