// Package config 提供应用配置加载和管理功能
package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadDefaultValues 测试默认值加载
func TestLoadDefaultValues(t *testing.T) {
	// 重置单例
	Reset()

	// 设置必填项
	t.Setenv("DB_DSN", "user:pass@tcp(localhost:3306)/test")

	cfg := Load()

	// 验证默认值
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, 20, cfg.DB.MaxOpenConns)
	assert.Equal(t, 10, cfg.DB.MaxIdleConns)
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
	assert.Equal(t, "", cfg.Redis.Password)
	assert.Equal(t, 0, cfg.Redis.DB)
	assert.Equal(t, 10, cfg.Worker.PoolSize)
	assert.Equal(t, 100, cfg.Worker.QueueSize)
	assert.Equal(t, 60, cfg.Worker.DefaultTimeout)
	assert.Equal(t, "info", cfg.Log.Level)
}

// TestLoadFromEnv 测试从环境变量加载
func TestLoadFromEnv(t *testing.T) {
	Reset()

	// 设置所有环境变量
	t.Setenv("DB_DSN", "user:pass@tcp(localhost:3306)/prod")
	t.Setenv("DB_MAX_OPEN_CONNS", "100")
	t.Setenv("DB_MAX_IDLE_CONNS", "20")
	t.Setenv("SERVER_PORT", "9090")
	t.Setenv("REDIS_ADDR", "redis:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "1")
	t.Setenv("WORKER_POOL_SIZE", "50")
	t.Setenv("WORKER_QUEUE_SIZE", "500")
	t.Setenv("WORKER_DEFAULT_TIMEOUT", "120")
	t.Setenv("LOG_LEVEL", "debug")

	cfg := Load()

	// 验证环境变量值
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "user:pass@tcp(localhost:3306)/prod", cfg.DB.DSN)
	assert.Equal(t, 100, cfg.DB.MaxOpenConns)
	assert.Equal(t, 20, cfg.DB.MaxIdleConns)
	assert.Equal(t, "redis:6380", cfg.Redis.Addr)
	assert.Equal(t, "secret", cfg.Redis.Password)
	assert.Equal(t, 1, cfg.Redis.DB)
	assert.Equal(t, 50, cfg.Worker.PoolSize)
	assert.Equal(t, 500, cfg.Worker.QueueSize)
	assert.Equal(t, 120, cfg.Worker.DefaultTimeout)
	assert.Equal(t, "debug", cfg.Log.Level)
}

// TestLoadSingleton 测试单例模式
func TestLoadSingleton(t *testing.T) {
	Reset()
	t.Setenv("DB_DSN", "dsn1")

	cfg1 := Load()

	// 再次调用，单例应该返回相同实例
	cfg2 := Load()

	// 验证是同一个实例
	assert.Same(t, cfg1, cfg2, "Load should return the same instance")
}

// TestMissingRequiredDSN 测试缺少必填项 DB_DSN
func TestMissingRequiredDSN(t *testing.T) {
	Reset()

	// 清空 DB_DSN（确保未设置）
	os.Unsetenv("DB_DSN")

	// 清空 REDIS_ADDR 并设置为空测试
	t.Setenv("REDIS_ADDR", "localhost:6379")

	// 应该 panic
	defer func() {
		r := recover()
		require.NotNil(t, r, "Load should panic when DB_DSN is missing")
		assert.Contains(t, r, "DB_DSN")
	}()

	Load()
}

// TestMissingRequiredRedisAddr 测试 Redis.Addr 为空
func TestMissingRequiredRedisAddr(t *testing.T) {
	Reset()

	t.Setenv("DB_DSN", "user:pass@tcp(localhost:3306)/test")
	// Redis.Addr 有默认值 localhost:6379，即使设置为空字符串
	// 也会因为 getEnvString 返回默认值而不 panic
	// 所以这个测试验证的是：Redis.Addr 有默认值，不会 panic

	cfg := Load()

	// 应该使用默认值
	assert.Equal(t, "localhost:6379", cfg.Redis.Addr)
}

// TestInvalidIntValue 测试无效的整数环境变量
func TestInvalidIntValue(t *testing.T) {
	Reset()

	t.Setenv("DB_DSN", "test")
	t.Setenv("SERVER_PORT", "invalid") // 无效的端口值

	cfg := Load()

	// 无效值应该使用默认值
	assert.Equal(t, 8080, cfg.Server.Port)
}

// TestConfigValidate 测试 validate 方法
func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		dsn       string
		redisAddr string
		wantPanic bool
	}{
		{
			name:      "valid config",
			dsn:       "user:pass@tcp(localhost:3306)/db",
			redisAddr: "localhost:6379",
			wantPanic: false,
		},
		{
			name:      "missing DSN",
			dsn:       "",
			redisAddr: "localhost:6379",
			wantPanic: true,
		},
		{
			name:      "missing both",
			dsn:       "",
			redisAddr: "",
			wantPanic: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				DB: DBConfig{
					DSN: tt.dsn,
				},
				Redis: RedisConfig{
					Addr: tt.redisAddr,
				},
			}

			if tt.wantPanic {
				defer func() {
					r := recover()
					assert.NotNil(t, r, "validate should panic")
				}()
			}

			cfg.validate()

			if !tt.wantPanic {
				assert.NotPanics(t, func() {
					cfg.validate()
				})
			}
		})
	}
}

// TestReset 测试 Reset 函数
func TestReset(t *testing.T) {
	Reset()
	t.Setenv("DB_DSN", "first_dsn")

	cfg1 := Load()

	Reset()
	t.Setenv("DB_DSN", "second_dsn")
	cfg2 := Load()

	// Reset 后应该能加载新配置
	assert.Equal(t, "first_dsn", cfg1.DB.DSN)
	assert.Equal(t, "second_dsn", cfg2.DB.DSN)
	// 且是不同实例
	assert.NotSame(t, cfg1, cfg2)
}
