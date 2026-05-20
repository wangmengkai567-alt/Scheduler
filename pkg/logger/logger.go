// Package logger 提供基于 zap 的结构化日志封装
// 设计意图：统一日志格式，简化使用，支持生产/开发模式切换
package logger

import (
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// 全局日志实例
// 使用 sync.Once 确保日志只初始化一次（并发安全）
var (
	globalLogger *zap.Logger
	sugar        *zap.SugaredLogger
	once         sync.Once
	initialized  bool
)

// Init 初始化全局日志实例
// 必须在程序启动时调用一次，多次调用仅首次生效
// 参数：level 日志级别，支持 "debug"/"info"/"warn"/"error"
//
// 设计说明：
// - 通过环境变量 APP_ENV 判断运行模式
// - production 模式使用 JSON 格式，便于日志系统解析
// - 开发模式使用人类可读格式，便于调试
func Init(level string) {
	once.Do(func() {
		var logger *zap.Logger
		var err error

		// 根据环境变量选择日志格式
		env := os.Getenv("APP_ENV")
		if env == "production" {
			// 生产模式：JSON 格式，结构化输出
			logger, err = createProductionLogger(level)
		} else {
			// 开发模式：人类可读格式，带颜色
			logger, err = createDevelopmentLogger(level)
		}

		if err != nil {
			// 初始化失败是致命错误，使用 nop logger 避免 panic
			// 但仍然记录错误信息到 stderr
			os.Stderr.WriteString("logger init failed: " + err.Error() + "\n")
			globalLogger = zap.NewNop()
			sugar = globalLogger.Sugar()
			initialized = true
			return
		}

		globalLogger = logger
		sugar = logger.Sugar()
		initialized = true
	})
}

// createProductionLogger 创建生产环境日志器
// 输出 JSON 格式，包含 timestamp、level、caller、message 字段
func createProductionLogger(level string) (*zap.Logger, error) {
	// 解析日志级别
	zapLevel := parseLevel(level)

	// 生产环境编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey, // 不记录函数名，减少日志量
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder, // 小写级别名
		EncodeTime:     zapcore.ISO8601TimeEncoder,    // ISO8601 时间格式
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder, // 短路径 caller
	}

	// 创建核心组件
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON 编码器
		zapcore.AddSync(os.Stdout),            // 输出到 stdout
		zapLevel,
	)

	// 创建 logger，添加 caller 信息
	return zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1), // 跳过一层，显示调用方位置
	), nil
}

// createDevelopmentLogger 创建开发环境日志器
// 输出人类可读格式，带颜色和完整信息
func createDevelopmentLogger(level string) (*zap.Logger, error) {
	// 解析日志级别
	zapLevel := parseLevel(level)

	// 开发环境编码器配置
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalColorLevelEncoder, // 带颜色的大写级别
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// 创建核心组件
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(encoderConfig), // Console 编码器
		zapcore.AddSync(os.Stdout),
		zapLevel,
	)

	return zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.Development(), // 开发模式，使 DPanic 级别打印堆栈
	), nil
}

// parseLevel 将字符串级别转换为 zapcore.Level
// 无效级别默认为 info
func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

// L 返回全局 zap.Logger 实例
// 用于需要高性能的结构化日志场景
// 如果未初始化，返回 nop logger 避免 panic
func L() *zap.Logger {
	if !initialized || globalLogger == nil {
		return zap.NewNop()
	}
	return globalLogger
}

// S 返回全局 zap.SugaredLogger 实例
// 提供更灵活的日志接口，支持 printf 风格
func S() *zap.SugaredLogger {
	if !initialized || sugar == nil {
		return zap.NewNop().Sugar()
	}
	return sugar
}

// Sync 刷新日志缓冲区
// 应在程序退出前调用，确保所有日志写入
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

// =============================================================================
// 包级日志函数
// =============================================================================
//
// 设计说明：为什么用包级函数而不是传递 *zap.Logger 实例？
//
// 优点：
// 1. 使用简单：无需在每个函数中传递 logger 参数，直接调用 logger.Info() 即可
// 2. 减少样板代码：避免构造函数、方法接收者中都需要添加 logger 字段
// 3. 零依赖：调用方不需要了解 zap 的具体类型，降低耦合
// 4. 适合微服务：大多数服务只需一个全局日志实例，包级函数完全满足需求
//
// 缺点（权衡）：
// 1. 不适合需要多个不同配置 logger 的场景（本项目不需要）
// 2. 单元测试难以替换（可通过重构为接口解决，但增加复杂度）
//
// 结论：对于本项目这样只有单一日志配置的场景，包级函数是最简洁的选择
// =============================================================================

// Debug 记录调试级别日志
// 用于开发阶段追踪详细信息，生产环境通常不输出
func Debug(msg string, fields ...zap.Field) {
	L().Debug(msg, fields...)
}

// Info 记录信息级别日志
// 用于记录正常的业务流程信息
func Info(msg string, fields ...zap.Field) {
	L().Info(msg, fields...)
}

// Warn 记录警告级别日志
// 用于记录需要关注但不影响系统运行的问题
func Warn(msg string, fields ...zap.Field) {
	L().Warn(msg, fields...)
}

// Error 记录错误级别日志
// 用于记录错误信息，但系统仍可继续运行
func Error(msg string, fields ...zap.Field) {
	L().Error(msg, fields...)
}

// Fatal 记录致命错误日志并退出程序
// 仅用于不可恢复的错误，程序会立即退出
func Fatal(msg string, fields ...zap.Field) {
	L().Fatal(msg, fields...)
}

// =============================================================================
// 辅助函数
// =============================================================================

// With 创建带有预设字段的子 logger
// 用于在特定上下文中添加公共字段
// 例如：logger.With(zap.String("request_id", id))
func With(fields ...zap.Field) *zap.Logger {
	return L().With(fields...)
}

// Named 创建具名 logger
// 用于区分不同模块的日志来源
// 例如：logger.Named("scheduler")
func Named(name string) *zap.Logger {
	return L().Named(name)
}

// IsInitialized 返回日志是否已初始化
// 用于检查初始化状态
func IsInitialized() bool {
	return initialized
}
