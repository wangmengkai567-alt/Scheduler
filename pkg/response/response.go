// Package response 提供统一的 HTTP 响应格式
// 设计意图：确保所有 API 返回一致的 JSON 结构，便于前端统一处理
package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"gtask-scheduler/pkg/errors"
)

// Response 统一的 HTTP 响应结构
// 所有 API 都应使用此结构返回数据，保持一致性
type Response struct {
	Code    int         `json:"code"`    // 业务状态码，0 表示成功
	Message string      `json:"message"` // 错误或成功消息
	Data    interface{} `json:"data"`    // 响应数据，成功时返回
}

// Success 返回成功响应
// 参数：data 响应数据，可以是任意类型或 nil
func Success(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    errors.CodeOK,
		Message: "success",
		Data:    data,
	})
}

// SuccessWithMessage 返回带自定义消息的成功响应
// 用于需要返回特定成功消息的场景
func SuccessWithMessage(c *gin.Context, message string, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Code:    errors.CodeOK,
		Message: message,
		Data:    data,
	})
}

// Error 返回错误响应
// 自动从 AppError 提取 HTTP 状态码和错误信息
func Error(c *gin.Context, err error) {
	var appErr *errors.AppError
	if e, ok := err.(*errors.AppError); ok {
		appErr = e
	} else {
		// 非 AppError 类型，统一视为内部错误
		appErr = errors.NewInternal(err, "unexpected error occurred")
	}

	c.JSON(appErr.HTTP, Response{
		Code:    appErr.Code,
		Message: appErr.Message,
		Data:    nil,
	})
}

// BadRequest 返回参数错误响应
// 快捷方法，用于参数校验失败场景
func BadRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, Response{
		Code:    errors.CodeBadRequest,
		Message: message,
		Data:    nil,
	})
}

// NotFound 返回资源不存在响应
// 快捷方法，用于资源查找失败场景
func NotFound(c *gin.Context, resource string) {
	c.JSON(http.StatusNotFound, Response{
		Code:    errors.CodeNotFound,
		Message: resource + " not found",
		Data:    nil,
	})
}

// PageData 分页数据结构
// 用于列表查询接口，包含分页元信息
type PageData struct {
	List     interface{} `json:"list"`      // 数据列表
	Total    int64       `json:"total"`     // 总记录数
	Page     int         `json:"page"`      // 当前页码
	PageSize int         `json:"page_size"` // 每页数量
}

// SuccessWithPage 返回分页成功响应
// 统一分页数据格式，便于前端处理
func SuccessWithPage(c *gin.Context, list interface{}, total int64, page, pageSize int) {
	Success(c, PageData{
		List:     list,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}
