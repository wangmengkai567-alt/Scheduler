// Package mysql 提供 MySQL 数据访问层实现
// 设计意图：封装 GORM 操作，实现 TaskStore 接口，便于测试和替换
package mysql

import (
	"context"
	stderrors "errors"
	"time"

	"gorm.io/gorm"

	"gtask-scheduler/internal/model"
	"gtask-scheduler/pkg/errors"
)

// TaskStoreInterface 任务存储接口
// 定义任务数据访问的契约，便于 mock 测试和替换实现
//
// 设计说明：为什么定义接口？
// 1. 可测试性：单元测试可以用 mock 实现，不依赖真实数据库
// 2. 可替换性：换数据库或 ORM 时只需实现接口，不影响业务层
// 3. 关注点分离：业务层不需要知道数据如何存储
type TaskStoreInterface interface {
	// Create 创建任务
	Create(ctx context.Context, task *model.Task) error
	// GetByID 根据 ID 获取任务
	GetByID(ctx context.Context, id int64) (*model.Task, error)
	// List 获取所有任务列表
	List(ctx context.Context) ([]*model.Task, error)
	// ListActive 获取活跃任务（用于调度器启动时加载）
	ListActive(ctx context.Context) ([]*model.Task, error)
	// UpdateStatus 更新任务状态
	UpdateStatus(ctx context.Context, id int64, status model.TaskStatus) error
	// UpdateNextRunAt 更新下次执行时间
	UpdateNextRunAt(ctx context.Context, id int64, t time.Time) error
	// SaveExecLog 保存执行日志（事务内同步更新 tasks.last_run_at）
	SaveExecLog(ctx context.Context, log *model.TaskExecLog) error
	// ListExecLogs 获取任务的执行日志
	ListExecLogs(ctx context.Context, taskID int64, limit int) ([]*model.TaskExecLog, error)
}

// TaskStore TaskStoreInterface 的 GORM 实现
type TaskStore struct {
	db *gorm.DB
}

// NewTaskStore 创建任务存储实例
// 参数：db GORM DB 实例，已完成初始化
func NewTaskStore(db *gorm.DB) TaskStoreInterface {
	return &TaskStore{db: db}
}

// Create 创建任务
// 错误处理：唯一键冲突返回 ErrConflict，其他错误返回 ErrInternal
func (s *TaskStore) Create(ctx context.Context, task *model.Task) error {
	if err := s.db.WithContext(ctx).Create(task).Error; err != nil {
		// 检查是否为唯一键冲突
		if isDuplicateKeyError(err) {
			return errors.ErrConflict
		}
		return errors.NewInternal(err, "failed to create task")
	}
	return nil
}

// GetByID 根据 ID 获取任务
// 错误处理：gorm.ErrRecordNotFound 转换为 ErrNotFound，不透传 GORM 错误
func (s *TaskStore) GetByID(ctx context.Context, id int64) (*model.Task, error) {
	var task model.Task
	if err := s.db.WithContext(ctx).First(&task, id).Error; err != nil {
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			// GORM 错误转换为应用错误，不透传底层错误
			return nil, errors.NewNotFound("task", id)
		}
		return nil, errors.NewInternal(err, "failed to get task")
	}
	return &task, nil
}

// List 获取所有任务列表
// 结果按 created_at DESC 排序，默认最多返回 100 条
func (s *TaskStore) List(ctx context.Context) ([]*model.Task, error) {
	var tasks []*model.Task

	// 默认限制 100 条，避免大量数据查询
	const defaultLimit = 100

	if err := s.db.WithContext(ctx).
		Order("created_at DESC").
		Limit(defaultLimit).
		Find(&tasks).Error; err != nil {
		return nil, errors.NewInternal(err, "failed to list tasks")
	}

	return tasks, nil
}

// ListActive 获取活跃任务
// status = 'pending' 或 status = 'running'，用于调度器启动时加载
func (s *TaskStore) ListActive(ctx context.Context) ([]*model.Task, error) {
	var tasks []*model.Task

	// 查询待执行和运行中的任务
	if err := s.db.WithContext(ctx).
		Where("status IN ?", []model.TaskStatus{model.StatusPending, model.StatusRunning}).
		Order("next_run_at ASC").
		Find(&tasks).Error; err != nil {
		return nil, errors.NewInternal(err, "failed to list active tasks")
	}

	return tasks, nil
}

// UpdateStatus 更新任务状态
func (s *TaskStore) UpdateStatus(ctx context.Context, id int64, status model.TaskStatus) error {
	result := s.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Update("status", status)

	if result.Error != nil {
		return errors.NewInternal(result.Error, "failed to update task status")
	}

	if result.RowsAffected == 0 {
		return errors.NewNotFound("task", id)
	}

	return nil
}

// UpdateNextRunAt 更新下次执行时间
func (s *TaskStore) UpdateNextRunAt(ctx context.Context, id int64, t time.Time) error {
	result := s.db.WithContext(ctx).
		Model(&model.Task{}).
		Where("id = ?", id).
		Update("next_run_at", t)

	if result.Error != nil {
		return errors.NewInternal(result.Error, "failed to update next_run_at")
	}

	if result.RowsAffected == 0 {
		return errors.NewNotFound("task", id)
	}

	return nil
}

// SaveExecLog 保存执行日志
// 在事务内同步更新 tasks.last_run_at 字段，保证数据一致性
//
// 设计说明：为什么使用事务？
// 执行日志和任务状态需要保持一致，如果分开更新可能出现：
// 1. 日志写入成功但任务更新失败，导致数据不一致
// 2. 并发场景下，日志顺序和任务状态可能错乱
// 使用事务可以保证原子性，要么都成功，要么都失败
func (s *TaskStore) SaveExecLog(ctx context.Context, log *model.TaskExecLog) error {
	// 开启事务
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 写入执行日志
		if err := tx.Create(log).Error; err != nil {
			return errors.NewInternal(err, "failed to save exec log")
		}

		// 2. 更新任务的 last_run_at
		result := tx.Model(&model.Task{}).
			Where("id = ?", log.TaskID).
			Update("last_run_at", log.StartedAt)

		if result.Error != nil {
			return errors.NewInternal(result.Error, "failed to update last_run_at")
		}

		// 如果任务不存在，事务会回滚，日志也不会写入
		if result.RowsAffected == 0 {
			return errors.NewNotFound("task", log.TaskID)
		}

		return nil
	})
}

// ListExecLogs 获取任务的执行日志
// 按创建时间倒序，返回最近 limit 条记录
func (s *TaskStore) ListExecLogs(ctx context.Context, taskID int64, limit int) ([]*model.TaskExecLog, error) {
	var logs []*model.TaskExecLog

	// 限制最大查询数量
	if limit <= 0 {
		limit = 50
	}
	if limit > 1000 {
		limit = 1000
	}

	if err := s.db.WithContext(ctx).
		Where("task_id = ?", taskID).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return nil, errors.NewInternal(err, "failed to list exec logs")
	}

	return logs, nil
}

// isDuplicateKeyError 检查是否为唯一键冲突错误
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	// MySQL 唯一键冲突错误码为 1062
	return containsString(errMsg, "Duplicate entry") ||
		containsString(errMsg, "1062") ||
		containsString(errMsg, "UNIQUE constraint")
}

// containsString 检查字符串是否包含子串
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Migrate 执行数据库迁移
// 创建或更新表结构
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.Task{}, &model.TaskExecLog{})
}
