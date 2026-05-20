// Package errors 提供统一的错误码和错误类型定义
// 设计意图：将业务错误与 HTTP 状态码解耦，便于统一处理和国际化
package errors

import (
	"errors"
	"fmt"
)

// AppError 统一的应用错误类型
// 包含错误码、HTTP状态码和错误消息，用于在分层架构中传递错误上下文
type AppError struct {
	Code    int    // 业务错误码
	Message string // 错误消息
	HTTP    int    // 对应的 HTTP 状态码
	Err     error  // 原始错误，用于错误链追踪
}

// Error 实现 error 接口，返回完整的错误信息
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%d] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

// Unwrap 支持 errors.Is 和 errors.As 进行错误链解包
func (e *AppError) Unwrap() error {
	return e.Err
}

// 错误码常量定义
// 遵循 HTTP 状态码风格，便于理解和扩展
const (
	CodeOK               = 0     // 成功
	CodeBadRequest       = 40000 // 请求参数错误
	CodeUnauthorized     = 40100 // 未授权
	CodeForbidden        = 40300 // 禁止访问
	CodeNotFound         = 40400 // 资源不存在
	CodeConflict         = 40900 // 资源冲突
	CodeInternal         = 50000 // 内部错误
	CodeServiceUnavailable = 50300 // 服务不可用
)

// 预定义错误实例
// 对于高频使用的错误，使用预定义实例减少内存分配
var (
	// ErrNotFound 资源不存在错误，用于替代 gorm.ErrRecordNotFound
	ErrNotFound = &AppError{
		Code:    CodeNotFound,
		Message: "resource not found",
		HTTP:    404,
	}

	// ErrBadRequest 请求参数错误
	ErrBadRequest = &AppError{
		Code:    CodeBadRequest,
		Message: "invalid request parameters",
		HTTP:    400,
	}

	// ErrInternal 内部错误，用于包装未知错误
	ErrInternal = &AppError{
		Code:    CodeInternal,
		Message: "internal server error",
		HTTP:    500,
	}

	// ErrConflict 资源冲突错误
	ErrConflict = &AppError{
		Code:    CodeConflict,
		Message: "resource conflict",
		HTTP:    409,
	}

	// ErrServiceUnavailable 服务不可用错误
	ErrServiceUnavailable = &AppError{
		Code:    CodeServiceUnavailable,
		Message: "service temporarily unavailable",
		HTTP:    503,
	}
)

// NewBadRequest 创建参数错误，包含具体错误信息
// 参数：msg 错误消息，args 格式化参数
func NewBadRequest(msg string, args ...interface{}) *AppError {
	return &AppError{
		Code:    CodeBadRequest,
		Message: fmt.Sprintf(msg, args...),
		HTTP:    400,
	}
}

// NewNotFound 创建资源不存在错误
// 参数：resource 资源名称，id 资源标识
func NewNotFound(resource string, id interface{}) *AppError {
	return &AppError{
		Code:    CodeNotFound,
		Message: fmt.Sprintf("%s with id '%v' not found", resource, id),
		HTTP:    404,
	}
}

// NewInternal 创建内部错误，包装原始错误
// 用于将第三方错误转换为应用错误，同时保留错误链
func NewInternal(err error, msg string, args ...interface{}) *AppError {
	return &AppError{
		Code:    CodeInternal,
		Message: fmt.Sprintf(msg, args...),
		HTTP:    500,
		Err:     err,
	}
}

// Wrap 将普通错误包装为 AppError
// 如果 err 已经是 AppError，直接返回；否则包装为内部错误
// 设计意图：避免重复包装，保持错误链清晰
func Wrap(err error, msg string, args ...interface{}) *AppError {
	if err == nil {
		return nil
	}
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return NewInternal(err, msg, args...)
}

// IsNotFound 判断是否为资源不存在错误
// 用于在业务层判断是否需要特殊处理
func IsNotFound(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == CodeNotFound
	}
	return false
}
