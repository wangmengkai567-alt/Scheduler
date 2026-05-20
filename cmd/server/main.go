// Package main 程序入口
// 设计意图：完成依赖注入和组件初始化，启动 HTTP 服务
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"gtask-scheduler/internal/api/handler"
	"gtask-scheduler/internal/api/middleware"
	"gtask-scheduler/internal/config"
	"gtask-scheduler/internal/model"
	"gtask-scheduler/internal/scheduler"
	redisstore "gtask-scheduler/internal/store/redis"
	mysqlstore "gtask-scheduler/internal/store/mysql"
	"gtask-scheduler/internal/worker"
	"gtask-scheduler/pkg/logger"
)

// 优雅退出超时时间常量
// 设计说明：为什么设置为 10 秒？
// 1. 足够长：允许进行中的请求完成，避免强制中断导致数据不一致
// 2. 足够短：不会让客户端等待过久，保证服务快速下线
// 3. 实践经验：大多数 HTTP 请求在 5-10 秒内完成
// 4. 容器环境：Kubernetes 默认给 30 秒，10 秒留有余地
const gracefulShutdownTimeout = 10 * time.Second

func main() {
	// ============================================================
	// 依赖初始化顺序：Logger → Config → DB → Redis → Pool → Scheduler → HTTP
	//
	// 设计说明：为什么这个顺序很重要？
	// 1. Logger 最先初始化：后续所有组件的日志都能正常记录
	// 2. Config 紧随其后：其他组件依赖配置
	// 3. DB 在 Redis 之前：数据库是核心依赖，Redis 是可选依赖
	// 4. Redis 在 Pool 之前：分布式锁需要 Redis 客户端
	// 5. Pool 在 Scheduler 之前：调度器需要提交任务到 Pool
	// 6. HTTP 最后：服务启动前所有依赖必须就绪
	// ============================================================

	// 1. 加载配置（如果必填项缺失会 panic）
	cfg := config.Load()

	// 2. 初始化日志（最先初始化，确保后续日志能记录）
	logger.Init(cfg.Log.Level)
	defer logger.Sync()

	logger.Info("starting gtask-scheduler",
		zap.Int("port", cfg.Server.Port),
		zap.String("log_level", cfg.Log.Level),
	)

	// 3. 初始化数据库
	db, err := initDatabase(cfg)
	if err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	logger.Info("database connected",
		zap.Int("max_open_conns", cfg.DB.MaxOpenConns),
		zap.Int("max_idle_conns", cfg.DB.MaxIdleConns),
	)

	// 4. 执行数据库迁移
	if err := mysqlstore.Migrate(db); err != nil {
		logger.Fatal("failed to migrate database", zap.Error(err))
	}
	logger.Info("database migrated", zap.Strings("tables", []string{"tasks", "task_exec_logs"}))

	// 5. 初始化 Redis（可选，失败不影响核心功能）
	rdb, err := initRedis(cfg)
	if err != nil {
		logger.Warn("failed to init redis, distributed lock disabled", zap.Error(err))
		rdb = nil
	} else {
		logger.Info("redis connected", zap.String("addr", cfg.Redis.Addr))
	}

	// 6. 初始化分布式锁管理器（如果 Redis 可用）
	var _ *redisstore.LockManager // 预留给分布式锁使用
	instanceID := generateInstanceID()
	if rdb != nil {
		_ = redisstore.NewLockManager(rdb, "gtask:")
		logger.Info("distributed lock enabled", zap.String("instance_id", instanceID))
	}

	// 7. 创建任务存储
	taskStore := mysqlstore.NewTaskStore(db)

	// 8. 创建并注册 JobHandler
	jobHandler := NewExamplePrintHandler()
	logger.Info("job handler registered", zap.String("name", "example_print"))

	// 9. 初始化 Worker Pool
	workerPool := worker.NewPool(
		cfg.Worker.PoolSize,
		cfg.Worker.QueueSize,
		jobHandler,
	)

	// 10. 初始化调度器
	sched := scheduler.NewScheduler(taskStore, workerPool)

	// 11. 启动 Worker Pool（先于调度器启动）
	workerPool.Start()
	logger.Info("worker pool started",
		zap.Int("workers", cfg.Worker.PoolSize),
		zap.Int("queue_size", cfg.Worker.QueueSize),
	)

	// 12. 启动调度器
	if err := sched.Start(); err != nil {
		logger.Fatal("failed to start scheduler", zap.Error(err))
	}
	logger.Info("scheduler started")

	// 13. 创建 HTTP 服务
	server := createServer(cfg, taskStore)

	// 14. 启动 HTTP 服务（非阻塞）
	go func() {
		addr := fmt.Sprintf(":%d", cfg.Server.Port)
		logger.Info("http server starting", zap.String("addr", addr))

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("http server error", zap.Error(err))
		}
	}()

	logger.Info("gtask-scheduler started successfully")

	// ============================================================
	// 优雅退出流程
	// 设计说明：
	// 1. 停止接收新请求（HTTP Shutdown）
	// 2. 停止调度器（不再产生新任务）
	// 3. 停止 Worker Pool（等待进行中任务完成）
	// 4. 关闭外部连接（Redis、DB）
	// ============================================================

	// 等待终止信号
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	logger.Info("received shutdown signal, starting graceful shutdown",
		zap.String("signal", sig.String()),
	)

	// 创建超时上下文
	ctx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	defer cancel()

	// 1. 停止 HTTP 服务（不再接收新请求）
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("http server shutdown error", zap.Error(err))
	} else {
		logger.Info("http server stopped")
	}

	// 2. 停止调度器
	sched.Stop()
	logger.Info("scheduler stopped")

	// 3. 停止 Worker Pool
	workerPool.Stop()
	logger.Info("worker pool stopped")

	// 4. 关闭 Redis 连接
	if rdb != nil {
		if err := rdb.Close(); err != nil {
			logger.Error("redis close error", zap.Error(err))
		} else {
			logger.Info("redis connection closed")
		}
	}

	// 5. 关闭数据库连接
	sqlDB, _ := db.DB()
	if sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
			logger.Error("database close error", zap.Error(err))
		} else {
			logger.Info("database connection closed")
		}
	}

	logger.Info("gtask-scheduler exited gracefully")
}

// initDatabase 初始化数据库连接
func initDatabase(cfg *config.Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DB.DSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 获取底层 sql.DB 用于配置连接池
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 配置连接池
	sqlDB.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.DB.MaxIdleConns)

	// 测试连接
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

// initRedis 初始化 Redis 连接
func initRedis(cfg *config.Config) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return rdb, nil
}

// createServer 创建 HTTP 服务
func createServer(cfg *config.Config, taskStore mysqlstore.TaskStoreInterface) *http.Server {
	// 设置 Gin 运行模式（通过环境变量 APP_ENV 判断）
	env := os.Getenv("APP_ENV")
	if env == "production" {
		gin.SetMode(gin.ReleaseMode)
	} else {
		gin.SetMode(gin.DebugMode)
	}

	// 创建路由
	router := gin.New()

	// 添加中间件
	router.Use(gin.Recovery())
	router.Use(middleware.RequestLogger())

	// 健康检查
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// 注册业务路由
	taskHandler := handler.NewTaskHandler(taskStore)
	api := router.Group("/api/v1")
	taskHandler.RegisterRoutes(api)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
}

// generateInstanceID 生成实例唯一标识
// 格式：hostname-pid，用于分布式锁的持有者标识
func generateInstanceID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}

// ============================================================
// JobHandler 实现
// ============================================================

// ExamplePrintHandler 示例任务处理器
// 打印任务信息，用于测试和演示
type ExamplePrintHandler struct{}

// NewExamplePrintHandler 创建示例处理器
func NewExamplePrintHandler() worker.JobHandler {
	return &ExamplePrintHandler{}
}

// Handle 实现 JobHandler 接口
func (h *ExamplePrintHandler) Handle(ctx context.Context, taskID uint, payload string) error {
	logger.Info("executing task",
		zap.Uint("task_id", taskID),
		zap.String("payload", payload),
	)

	// 模拟任务执行
	// 实际项目中这里可能是：
	// - 发送 HTTP 请求
	// - 写入数据库
	// - 发送消息到队列
	// - 调用外部服务

	return nil
}

// ============================================================
// 辅助函数
// ============================================================

// _ 确保接口实现
var _ worker.JobHandler = (*ExamplePrintHandler)(nil)

// 确保 model.Task 实现了必要的接口
var _ = model.Task{}
