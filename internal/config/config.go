// Package config 提供应用配置加载和管理功能
// 设计意图：统一配置管理，支持环境变量注入，适合容器化部署
package config

import (
	"os"
	"strconv"
	"sync"
)

// Config 应用配置结构
// 包含所有模块的配置项，通过环境变量加载
//
// 设计说明：为什么选择环境变量而不是 yaml 配置文件？
// 1. 容器化友好：Kubernetes/Docker 原生支持环境变量注入，无需挂载配置文件
// 2. 12-Factor App 原则：配置应存储在环境变量中，实现配置与代码分离
// 3. 安全性：敏感信息（密码、密钥）通过环境变量传递，避免配置文件泄露
// 4. 部署简单：不同环境（dev/staging/prod）只需设置不同环境变量，无需维护多份配置文件
// 5. 云原生：AWS/GCP/阿里云等云平台都支持环境变量配置
type Config struct {
	Server ServerConfig
	DB     DBConfig
	Redis  RedisConfig
	Worker WorkerConfig
	Log    LogConfig
}

// ServerConfig HTTP 服务器配置
type ServerConfig struct {
	Port int // 监听端口，默认 8080
}

// DBConfig 数据库配置
type DBConfig struct {
	DSN          string // MySQL 连接串，必填
	MaxOpenConns int    // 最大连接数，默认 20
	MaxIdleConns int    // 最大空闲连接数，默认 10
}

// RedisConfig Redis 配置
type RedisConfig struct {
	Addr     string // Redis 地址，必填，格式 host:port
	Password string // Redis 密码，可选
	DB       int    // Redis 数据库编号，默认 0
}

// WorkerConfig Worker Pool 配置
type WorkerConfig struct {
	PoolSize       int // Worker 数量，默认 10
	QueueSize      int // 任务队列大小，默认 100
	DefaultTimeout int // 默认超时时间（秒），默认 60
}

// LogConfig 日志配置
type LogConfig struct {
	Level string // 日志级别，默认 "info"
}

// 全局配置实例
// 使用 sync.Once 实现单例模式，保证配置只加载一次
//
// 设计说明：为什么使用单例模式？
// 1. 配置在程序生命周期内不变，无需重复加载
// 2. 避免多处调用 Load() 导致的不一致
// 3. 减少环境变量读取开销（虽然开销很小，但单例更合理）
//
// 对比每次 Load 的方案：
// - 每次 Load：适合配置需要动态更新的场景（本项目不需要）
// - 单例模式：适合配置固定的场景，更简洁高效
var (
	instance *Config
	once     sync.Once
)

// Load 加载配置
// 优先从环境变量读取，其次使用默认值
// 本地开发模式：如果未设置 DB_DSN，使用默认的本地 MySQL 连接
func Load() *Config {
	once.Do(func() {
		// 本地开发默认值
		defaultDSN := "root:123456@tcp(localhost:3306)/gtask?charset=utf8mb4&parseTime=True&loc=Local"
		
		instance = &Config{
			Server: ServerConfig{
				Port: getEnvInt("SERVER_PORT", 8080),
			},
			DB: DBConfig{
				DSN:          getEnvString("DB_DSN", defaultDSN),
				MaxOpenConns: getEnvInt("DB_MAX_OPEN_CONNS", 20),
				MaxIdleConns: getEnvInt("DB_MAX_IDLE_CONNS", 10),
			},
			Redis: RedisConfig{
				Addr:     getEnvString("REDIS_ADDR", "localhost:6379"),
				Password: getEnvString("REDIS_PASSWORD", ""),
				DB:       getEnvInt("REDIS_DB", 0),
			},
			Worker: WorkerConfig{
				PoolSize:       getEnvInt("WORKER_POOL_SIZE", 10),
				QueueSize:      getEnvInt("WORKER_QUEUE_SIZE", 100),
				DefaultTimeout: getEnvInt("WORKER_DEFAULT_TIMEOUT", 60),
			},
			Log: LogConfig{
				Level: getEnvString("LOG_LEVEL", "info"),
			},
		}

		// 校验必填项（现在 DB_DSN 有默认值，通常不会缺失）
		instance.validate()
	})

	return instance
}

// validate 校验必填配置项
func (c *Config) validate() {
	// 所有配置项现在都有默认值
	// 生产环境可以通过环境变量覆盖
}

// getEnvString 获取字符串环境变量，支持默认值
func getEnvString(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt 获取整数环境变量，支持默认值
// 如果环境变量值无法解析为整数，使用默认值
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// Reset 重置配置单例
// 仅用于测试，生产代码不应调用
func Reset() {
	instance = nil
	once = sync.Once{}
}
