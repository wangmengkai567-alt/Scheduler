// Package mysql 提供 MySQL 数据访问层实现
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"gtask-scheduler/internal/model"
	customerrors "gtask-scheduler/pkg/errors"
)

// setupMockDB 创建 mock 数据库连接
func setupMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err, "failed to create sqlmock")

	dialector := mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	})

	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err, "failed to open gorm connection")

	return db, mock
}

// TestGetByID_Success 测试 GetByID 正常场景
func TestGetByID_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 准备测试数据
	now := time.Now()
	expectedTask := &model.Task{
		ID:         1,
		Name:       "test-task",
		CronExpr:   "*/5 * * * *",
		Status:     model.StatusPending,
		MaxRetries: 3,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// 设置 mock 期望
	rows := sqlmock.NewRows([]string{
		"id", "name", "cron_expr", "payload", "status",
		"last_run_at", "next_run_at", "last_error", "run_count",
		"max_retries", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		expectedTask.ID,
		expectedTask.Name,
		expectedTask.CronExpr,
		expectedTask.Payload,
		expectedTask.Status,
		nil, // last_run_at
		nil, // next_run_at
		"",  // last_error
		0,   // run_count
		expectedTask.MaxRetries,
		expectedTask.CreatedAt,
		expectedTask.UpdatedAt,
		nil, // deleted_at
	)

	// GORM 生成的 SQL 使用 ? 作为 LIMIT 参数
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`id` = ? AND `tasks`.`deleted_at` IS NULL ORDER BY `tasks`.`id` LIMIT ?")).
		WithArgs(int64(1), 1).
		WillReturnRows(rows)

	// 执行测试
	task, err := store.GetByID(context.Background(), 1)

	// 验证结果
	assert.NoError(t, err)
	assert.NotNil(t, task)
	assert.Equal(t, expectedTask.ID, task.ID)
	assert.Equal(t, expectedTask.Name, task.Name)
	assert.Equal(t, expectedTask.CronExpr, task.CronExpr)
	assert.Equal(t, expectedTask.Status, task.Status)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByID_NotFound 测试 GetByID 任务不存在场景
func TestGetByID_NotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 返回空结果
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`id` = ? AND `tasks`.`deleted_at` IS NULL ORDER BY `tasks`.`id` LIMIT ?")).
		WithArgs(int64(999), 1).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "name", "cron_expr", "payload", "status",
			"last_run_at", "next_run_at", "last_error", "run_count",
			"max_retries", "created_at", "updated_at", "deleted_at",
		}))

	// 执行测试
	task, err := store.GetByID(context.Background(), 999)

	// 验证结果 - 应该返回 ErrNotFound
	assert.Error(t, err)
	assert.Nil(t, task)

	// 验证错误类型
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeNotFound, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGetByID_DBError 测试 GetByID 数据库错误场景
func TestGetByID_DBError(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 返回数据库错误
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`id` = ? AND `tasks`.`deleted_at` IS NULL ORDER BY `tasks`.`id` LIMIT ?")).
		WithArgs(int64(1), 1).
		WillReturnError(sql.ErrConnDone)

	// 执行测试
	task, err := store.GetByID(context.Background(), 1)

	// 验证结果 - 应该返回内部错误
	assert.Error(t, err)
	assert.Nil(t, task)

	// 验证错误类型
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeInternal, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_Success 测试 Create 正常场景
func TestCreate_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	task := &model.Task{
		Name:       "new-task",
		CronExpr:   "*/10 * * * *",
		Status:     model.StatusPending,
		MaxRetries: 3,
	}

	// 设置 mock 期望
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `tasks`")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// 执行测试
	err := store.Create(context.Background(), task)

	// 验证结果
	assert.NoError(t, err)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestList_Success 测试 List 正常场景
func TestList_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	now := time.Now()

	// 设置 mock 期望
	rows := sqlmock.NewRows([]string{
		"id", "name", "cron_expr", "payload", "status",
		"last_run_at", "next_run_at", "last_error", "run_count",
		"max_retries", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		1, "task1", "*/5 * * * *", "", model.StatusPending,
		nil, nil, "", 0, 3, now, now, nil,
	).AddRow(
		2, "task2", "*/10 * * * *", "", model.StatusPending,
		nil, nil, "", 0, 3, now, now, nil,
	)

	// GORM 使用参数化 LIMIT
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`deleted_at` IS NULL ORDER BY created_at DESC LIMIT ?")).
		WithArgs(100).
		WillReturnRows(rows)

	// 执行测试
	tasks, err := store.List(context.Background())

	// 验证结果
	assert.NoError(t, err)
	assert.Len(t, tasks, 2)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListActive_Success 测试 ListActive 正常场景
func TestListActive_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	now := time.Time{}
	nextRun := now.Add(5 * time.Minute)

	// 设置 mock 期望
	rows := sqlmock.NewRows([]string{
		"id", "name", "cron_expr", "payload", "status",
		"last_run_at", "next_run_at", "last_error", "run_count",
		"max_retries", "created_at", "updated_at", "deleted_at",
	}).AddRow(
		1, "active-task", "*/5 * * * *", "", model.StatusPending,
		nil, nextRun, "", 0, 3, now, now, nil,
	)

	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE status IN (?,?) AND `tasks`.`deleted_at` IS NULL ORDER BY next_run_at ASC")).
		WithArgs(model.StatusPending, model.StatusRunning).
		WillReturnRows(rows)

	// 执行测试
	tasks, err := store.ListActive(context.Background())

	// 验证结果
	assert.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "active-task", tasks[0].Name)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateStatus_Success 测试 UpdateStatus 正常场景
func TestUpdateStatus_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - GORM 会自动更新 updated_at
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `status`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(model.StatusRunning, sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 执行测试
	err := store.UpdateStatus(context.Background(), 1, model.StatusRunning)

	// 验证结果
	assert.NoError(t, err)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateStatus_NotFound 测试 UpdateStatus 任务不存在
func TestUpdateStatus_NotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 没有行受影响
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `status`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(model.StatusRunning, sqlmock.AnyArg(), int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	// 执行测试
	err := store.UpdateStatus(context.Background(), 999, model.StatusRunning)

	// 验证结果 - 应该返回 ErrNotFound
	assert.Error(t, err)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeNotFound, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSaveExecLog_Success 测试 SaveExecLog 正常场景
func TestSaveExecLog_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	now := time.Now()
	execLog := &model.TaskExecLog{
		TaskID:     1,
		Status:     model.ExecStatusSuccess,
		StartedAt:  now,
		FinishedAt: now.Add(1 * time.Second),
		Duration:   1000,
	}

	// 设置 mock 期望 - 事务
	mock.ExpectBegin()
	// 插入执行日志
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `task_exec_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	// 更新任务的 last_run_at - GORM 会自动更新 updated_at
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `last_run_at`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(now, sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 执行测试
	err := store.SaveExecLog(context.Background(), execLog)

	// 验证结果
	assert.NoError(t, err)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSaveExecLog_TaskNotFound 测试 SaveExecLog 任务不存在
func TestSaveExecLog_TaskNotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	now := time.Now()
	execLog := &model.TaskExecLog{
		TaskID:     999,
		Status:     model.ExecStatusSuccess,
		StartedAt:  now,
		FinishedAt: now,
	}

	// 设置 mock 期望 - 事务会回滚
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `task_exec_logs`")).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `last_run_at`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(now, sqlmock.AnyArg(), int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 没有行受影响
	mock.ExpectRollback()

	// 执行测试
	err := store.SaveExecLog(context.Background(), execLog)

	// 验证结果 - 应该返回 ErrNotFound
	assert.Error(t, err)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeNotFound, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListExecLogs_Success 测试 ListExecLogs 正常场景
func TestListExecLogs_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	now := time.Now()

	// 设置 mock 期望
	rows := sqlmock.NewRows([]string{
		"id", "task_id", "status", "error", "duration",
		"started_at", "finished_at", "created_at",
	}).AddRow(
		1, 1, model.ExecStatusSuccess, "", 100, now, now.Add(time.Second), now,
	).AddRow(
		2, 1, model.ExecStatusFailed, "timeout", 5000, now.Add(-time.Minute), now, now,
	)

	// GORM 使用参数化 LIMIT
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `task_exec_logs` WHERE task_id = ? ORDER BY created_at DESC LIMIT ?")).
		WithArgs(int64(1), 10).
		WillReturnRows(rows)

	// 执行测试
	logs, err := store.ListExecLogs(context.Background(), 1, 10)

	// 验证结果
	assert.NoError(t, err)
	assert.Len(t, logs, 2)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateNextRunAt_Success 测试 UpdateNextRunAt 正常场景
func TestUpdateNextRunAt_Success(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	nextRun := time.Now().Add(5 * time.Minute)

	// 设置 mock 期望
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `next_run_at`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(nextRun, sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	// 执行测试
	err := store.UpdateNextRunAt(context.Background(), 1, nextRun)

	// 验证结果
	assert.NoError(t, err)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestCreate_DBError 测试 Create 数据库错误场景
func TestCreate_DBError(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	task := &model.Task{
		Name:     "fail-task",
		CronExpr: "*/10 * * * *",
	}

	// 设置 mock 期望 - 返回错误
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO `tasks`")).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	// 执行测试
	err := store.Create(context.Background(), task)

	// 验证结果
	assert.Error(t, err)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeInternal, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestList_DBError 测试 List 数据库错误场景
func TestList_DBError(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 返回错误
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE `tasks`.`deleted_at` IS NULL ORDER BY created_at DESC LIMIT ?")).
		WithArgs(100).
		WillReturnError(sql.ErrConnDone)

	// 执行测试
	tasks, err := store.List(context.Background())

	// 验证结果
	assert.Error(t, err)
	assert.Nil(t, tasks)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeInternal, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListActive_DBError 测试 ListActive 数据库错误场景
func TestListActive_DBError(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 返回错误
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `tasks` WHERE status IN (?,?) AND `tasks`.`deleted_at` IS NULL ORDER BY next_run_at ASC")).
		WithArgs(model.StatusPending, model.StatusRunning).
		WillReturnError(sql.ErrConnDone)

	// 执行测试
	tasks, err := store.ListActive(context.Background())

	// 验证结果
	assert.Error(t, err)
	assert.Nil(t, tasks)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeInternal, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestUpdateNextRunAt_NotFound 测试 UpdateNextRunAt 任务不存在
func TestUpdateNextRunAt_NotFound(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	nextRun := time.Now().Add(5 * time.Minute)

	// 设置 mock 期望 - 没有行受影响
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta("UPDATE `tasks` SET `next_run_at`=?,`updated_at`=? WHERE id = ? AND `tasks`.`deleted_at` IS NULL")).
		WithArgs(nextRun, sqlmock.AnyArg(), int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	// 执行测试
	err := store.UpdateNextRunAt(context.Background(), 999, nextRun)

	// 验证结果 - 应该返回 ErrNotFound
	assert.Error(t, err)
	var appErr *customerrors.AppError
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, customerrors.CodeNotFound, appErr.Code)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestListExecLogs_Empty 测试 ListExecLogs 空结果
func TestListExecLogs_Empty(t *testing.T) {
	db, mock := setupMockDB(t)
	store := NewTaskStore(db).(*TaskStore)

	// 设置 mock 期望 - 返回空结果
	mock.ExpectQuery(regexp.QuoteMeta("SELECT * FROM `task_exec_logs` WHERE task_id = ? ORDER BY created_at DESC LIMIT ?")).
		WithArgs(int64(1), 10).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "task_id", "status", "error", "duration",
			"started_at", "finished_at", "created_at",
		}))

	// 执行测试
	logs, err := store.ListExecLogs(context.Background(), 1, 10)

	// 验证结果
	assert.NoError(t, err)
	assert.Len(t, logs, 0)

	// 确保 mock 期望都被满足
	assert.NoError(t, mock.ExpectationsWereMet())
}
