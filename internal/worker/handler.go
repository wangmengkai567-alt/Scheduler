// Package worker 提供任务执行器和 Worker Pool 实现
// 设计意图：将任务执行逻辑与调度逻辑解耦，支持自定义任务处理器
package worker

import (
	"context"
)

// JobHandler 任务处理器接口
// 定义任务执行的契约，所有任务处理器必须实现此接口
// 设计意图：通过接口抽象实现可测试性和可扩展性
type JobHandler interface {
	// Handle 执行任务
	// ctx 用于传递取消信号和超时控制
	// taskID 为任务标识
	// payload 为任务参数（JSON 格式）
	// 返回错误时，任务将被标记为失败
	Handle(ctx context.Context, taskID uint, payload string) error
}

// Job 表示一个待执行的任务
// 包含执行任务所需的所有信息
type Job struct {
	TaskID  uint   // 任务 ID
	Name    string // 任务名称
	Payload string // 任务参数
}

// JobResult 任务执行结果
// 用于在 goroutine 间传递执行结果
type JobResult struct {
	TaskID  uint   // 任务 ID
	Success bool   // 是否成功
	Error   string // 错误信息
}

// HandlerFunc 将函数转换为 JobHandler
// 提供快速创建 JobHandler 的便捷方法
type HandlerFunc func(ctx context.Context, taskID uint, payload string) error

// Handle 实现 JobHandler 接口
func (f HandlerFunc) Handle(ctx context.Context, taskID uint, payload string) error {
	return f(ctx, taskID, payload)
}

// NoOpHandler 空操作处理器
// 用于测试或作为默认处理器
var NoOpHandler JobHandler = HandlerFunc(func(ctx context.Context, taskID uint, payload string) error {
	return nil
})
