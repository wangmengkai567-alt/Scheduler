// Package middleware 提供 HTTP 中间件
// 设计意图：统一请求日志记录，支持链路追踪
package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"gtask-scheduler/pkg/logger"
)

// RequestLogger 请求日志中间件
// 记录每个请求的方法、路径、状态码、耗时、客户端 IP、请求 ID
//
// 设计说明：为什么用 c.Next() 之后而不是之前记录日志？
// 1. 获取响应状态码：状态码在 handler 处理后才确定，c.Next() 之前无法获取
// 2. 计算准确耗时：只有 handler 执行完成后才能得到真实的请求处理时间
// 3. 捕获 handler 错误：c.Errors 在 c.Next() 之后才能获取到处理过程中的错误
// 4. 记录完整上下文：handler 可能修改响应信息，需要等待处理完成
//
// request_id 在分布式系统中的作用（链路追踪）：
// 1. 请求唯一标识：在日志、监控、告警中关联同一请求的所有记录
// 2. 跨服务追踪：通过 HTTP Header 传递，在微服务调用链中追踪请求
// 3. 问题定位：通过 request_id 快速搜索相关日志，定位问题根因
// 4. 性能分析：结合链路追踪系统（如 Jaeger、Zipkin）分析请求耗时分布
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取或生成 request_id
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = generateRequestID()
		}

		// 将 request_id 存入 gin.Context，供 handler 层取用
		c.Set("request_id", requestID)

		// 将 request_id 写入响应 Header
		c.Header("X-Request-ID", requestID)

		// 获取请求路径（不含 query string）
		path := c.Request.URL.Path

		// 健康检查路径跳过日志记录
		if path == "/ping" && c.Request.Method == "GET" {
			c.Next()
			return
		}

		// 记录请求开始时间
		startTime := time.Now()

		// 处理请求
		// c.Next() 会执行后续的中间件和 handler
		// 执行完成后才会继续下面的代码
		c.Next()

		// 计算请求耗时
		costMs := time.Since(startTime).Milliseconds()

		// 获取响应状态码
		status := c.Writer.Status()

		// 构建日志字段
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.Int("status", status),
			zap.Int64("cost_ms", costMs),
			zap.String("client_ip", c.ClientIP()),
			zap.String("request_id", requestID),
		}

		// 如果有错误，添加错误信息
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		// 根据状态码选择日志级别
		// 状态码 >= 500 用 Error 级别
		// 状态码 >= 400 用 Warn 级别
		// 其余用 Info 级别
		switch {
		case status >= 500:
			logger.Error("request completed", fields...)
		case status >= 400:
			logger.Warn("request completed", fields...)
		default:
			logger.Info("request completed", fields...)
		}
	}
}

// generateRequestID 生成请求 ID
// 使用 UUID v4 格式，便于分布式系统追踪
func generateRequestID() string {
	return uuidV4()
}

// uuidV4 生成 UUID v4 格式的字符串
// 格式：xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
// 其中 4 表示版本，y 为 8/9/a/b 之一
func uuidV4() string {
	// 使用时间戳和随机数生成
	// 生产环境建议使用 github.com/google/uuid 库
	now := time.Now()
	nanos := now.UnixNano()

	// 基于时间戳生成伪 UUID
	// 格式：8-4-4-4-12
	hex := make([]byte, 32)
	timeToHex(nanos, hex[0:16])

	// 添加随机部分
	randomToHex(hex[16:32])

	// 设置版本号（第 13 个字符为 '4'）
	hex[12] = '4'

	// 设置变体（第 17 个字符为 '8'/'9'/'a'/'b'）
	variant := hex[16]
	if variant >= 'c' {
		hex[16] = 'b'
	} else if variant >= '8' {
		// 保持原值
	} else {
		hex[16] = '8' + (variant - '0')%4
	}

	return string(hex[0:8]) + "-" + string(hex[8:12]) + "-" + string(hex[12:16]) + "-" + string(hex[16:20]) + "-" + string(hex[20:32])
}

// timeToHex 将时间戳转换为十六进制字符串
func timeToHex(nanos int64, buf []byte) {
	const hexDigits = "0123456789abcdef"
	for i := 15; i >= 0; i-- {
		buf[i] = hexDigits[nanos&0xf]
		nanos >>= 4
	}
}

// randomToHex 生成随机十六进制字符串
func randomToHex(buf []byte) {
	const hexDigits = "0123456789abcdef"
	// 使用时间戳的低位作为随机源
	seed := time.Now().UnixNano()
	for i := 0; i < len(buf); i++ {
		buf[i] = hexDigits[(seed+int64(i))&0xf]
		seed = seed>>4 + int64(i)*31
	}
}

// GetRequestID 从 gin.Context 获取 request_id
// 供 handler 层使用
func GetRequestID(c *gin.Context) string {
	if requestID, exists := c.Get("request_id"); exists {
		if id, ok := requestID.(string); ok {
			return id
		}
	}
	return ""
}
