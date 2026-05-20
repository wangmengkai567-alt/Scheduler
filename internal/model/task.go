// Package model 定义数据模型和业务实体
// 设计意图：将数据库模型与业务逻辑解耦，便于测试和维护
package model

import (
	"time"

	"gorm.io/gorm"
)

// TaskStatus 任务状态常量
// 使用有意义的常量替代 magic number
type TaskStatus int

const (
	StatusPending    TaskStatus = 0 // 待执行
	StatusRunning    TaskStatus = 1 // 执行中
	StatusCompleted  TaskStatus = 2 // 已完成
	StatusFailed     TaskStatus = 3 // 已失败
	StatusCancelled  TaskStatus = 4 // 已取消
)

// String 返回状态的字符串表示，便于日志输出
func (s TaskStatus) String() string {
	switch s {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusCompleted:
		return "completed"
	case StatusFailed:
		return "failed"
	case StatusCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// Task 任务数据模型
// 对应数据库表 tasks，存储任务的基本信息和执行状态
type Task struct {
	ID          uint           `gorm:"primaryKey" json:"id"`
	Name        string         `gorm:"size:255;not null;index" json:"name"`           // 任务名称
	CronExpr    string         `gorm:"size:100;not null" json:"cron_expr"`             // Cron 表达式
	Payload     string         `gorm:"type:text" json:"payload"`                      // 任务参数（JSON 格式）
	Status      TaskStatus     `gorm:"type:tinyint;default:0;index" json:"status"`     // 任务状态
	LastRunAt   *time.Time     `json:"last_run_at"`                                   // 上次执行时间
	NextRunAt   *time.Time     `gorm:"index" json:"next_run_at"`                      // 下次执行时间
	LastError   string         `gorm:"type:text" json:"last_error"`                   // 最后错误信息
	RunCount    int            `gorm:"default:0" json:"run_count"`                    // 执行次数
	MaxRetries  int            `gorm:"default:3" json:"max_retries"`                  // 最大重试次数
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定表名
// 显式指定表名避免命名冲突，并符合数据库命名规范
func (Task) TableName() string {
	return "tasks"
}

// IsRunnable 检查任务是否可执行
// 只有待执行状态的任务才能被调度器选中执行
func (t *Task) IsRunnable() bool {
	return t.Status == StatusPending || t.Status == StatusFailed
}

// CanRetry 检查任务是否可以重试
// 失败且重试次数未达上限的任务可以重试
func (t *Task) CanRetry() bool {
	return t.Status == StatusFailed && t.RunCount < t.MaxRetries
}

// SetRunning 将任务状态设置为执行中
// 在任务开始执行时调用
func (t *Task) SetRunning() {
	t.Status = StatusRunning
	now := time.Now()
	t.LastRunAt = &now
}

// SetCompleted 将任务状态设置为完成
// 在任务成功执行后调用
func (t *Task) SetCompleted() {
	t.Status = StatusCompleted
	t.LastError = ""
	t.RunCount++
}

// SetFailed 将任务状态设置为失败
// 在任务执行失败时调用，记录错误信息
func (t *Task) SetFailed(errMsg string) {
	t.Status = StatusFailed
	t.LastError = errMsg
	t.RunCount++
}

// TaskCreateRequest 创建任务请求
// 用于接收 HTTP 请求参数，与数据库模型分离
type TaskCreateRequest struct {
	Name       string `json:"name" binding:"required,min=1,max=255"`
	CronExpr   string `json:"cron_expr" binding:"required"`
	Payload    string `json:"payload"`
	MaxRetries int    `json:"max_retries"`
}

// TaskUpdateRequest 更新任务请求
// 所有字段可选，只更新提供的字段
type TaskUpdateRequest struct {
	Name       *string `json:"name" binding:"omitempty,min=1,max=255"`
	CronExpr   *string `json:"cron_expr" binding:"omitempty"`
	Payload    *string `json:"payload"`
	MaxRetries *int    `json:"max_retries" binding:"omitempty,min=0"`
	Status     *int    `json:"status" binding:"omitempty,min=0,max=4"`
}

// TaskQueryRequest 查询任务请求
// 支持分页和状态过滤
type TaskQueryRequest struct {
	Page     int    `form:"page" binding:"omitempty,min=1"`
	PageSize int    `form:"page_size" binding:"omitempty,min=1,max=100"`
	Status   *int   `form:"status" binding:"omitempty,min=0,max=4"`
	Name     string `form:"name" binding:"omitempty"`
}

// TaskResponse 任务响应
// 用于返回给前端的任务信息
type TaskResponse struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	CronExpr  string    `json:"cron_expr"`
	Payload   string    `json:"payload,omitempty"`
	Status    int       `json:"status"`
	StatusStr string    `json:"status_str"`
	LastRunAt time.Time `json:"last_run_at,omitempty"`
	NextRunAt time.Time `json:"next_run_at,omitempty"`
	LastError string    `json:"last_error,omitempty"`
	RunCount  int       `json:"run_count"`
	MaxRetries int      `json:"max_retries"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToResponse 将 Task 转换为 TaskResponse
// 用于 API 层返回数据时统一格式
func (t *Task) ToResponse() *TaskResponse {
	resp := &TaskResponse{
		ID:         t.ID,
		Name:       t.Name,
		CronExpr:   t.CronExpr,
		Payload:    t.Payload,
		Status:     int(t.Status),
		StatusStr:  t.Status.String(),
		RunCount:   t.RunCount,
		MaxRetries: t.MaxRetries,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
	if t.LastRunAt != nil {
		resp.LastRunAt = *t.LastRunAt
	}
	if t.NextRunAt != nil {
		resp.NextRunAt = *t.NextRunAt
	}
	resp.LastError = t.LastError
	return resp
}

// TaskExecLog 任务执行日志
// 记录每次任务执行的详细信息，用于问题排查和历史追溯
type TaskExecLog struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	TaskID     int64     `gorm:"not null;index" json:"task_id"`      // 关联任务 ID
	Status     string    `gorm:"size:20;not null" json:"status"`     // 执行状态：success, failed
	Error      string    `gorm:"type:text" json:"error"`             // 错误信息
	Duration   int64     `gorm:"default:0" json:"duration"`          // 执行耗时（毫秒）
	StartedAt  time.Time `gorm:"not null" json:"started_at"`         // 开始时间
	FinishedAt time.Time `json:"finished_at"`                        // 结束时间
	CreatedAt  time.Time `json:"created_at"`
}

// TableName 指定表名
func (TaskExecLog) TableName() string {
	return "task_exec_logs"
}

// ExecLogStatus 执行日志状态常量
const (
	ExecStatusSuccess = "success"
	ExecStatusFailed  = "failed"
)
