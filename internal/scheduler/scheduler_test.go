// Package scheduler 提供定时任务调度功能
package scheduler

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gtask-scheduler/internal/model"
	"gtask-scheduler/internal/worker"
)

// MockTaskStore 是 TaskStoreInterface 的 mock 实现
type MockTaskStore struct {
	mu       sync.Mutex
	tasks    map[int64]*model.Task
	execLogs []*model.TaskExecLog
	err      error
}

// NewMockTaskStore 创建 mock 存储
func NewMockTaskStore() *MockTaskStore {
	return &MockTaskStore{
		tasks: make(map[int64]*model.Task),
	}
}

func (m *MockTaskStore) Create(ctx context.Context, task *model.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[int64(task.ID)] = task
	return nil
}

func (m *MockTaskStore) GetByID(ctx context.Context, id int64) (*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	if task, ok := m.tasks[id]; ok {
		return task, nil
	}
	return nil, nil
}

func (m *MockTaskStore) List(ctx context.Context) ([]*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*model.Task
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result, nil
}

func (m *MockTaskStore) ListActive(ctx context.Context) ([]*model.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return nil, m.err
	}
	var result []*model.Task
	for _, t := range m.tasks {
		if t.Status == model.StatusPending || t.Status == model.StatusRunning {
			result = append(result, t)
		}
	}
	return result, nil
}

func (m *MockTaskStore) UpdateStatus(ctx context.Context, id int64, status model.TaskStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.Status = status
	}
	return nil
}

func (m *MockTaskStore) UpdateNextRunAt(ctx context.Context, id int64, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if task, ok := m.tasks[id]; ok {
		task.NextRunAt = &t
	}
	return nil
}

func (m *MockTaskStore) SaveExecLog(ctx context.Context, log *model.TaskExecLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.execLogs = append(m.execLogs, log)
	return nil
}

func (m *MockTaskStore) ListExecLogs(ctx context.Context, taskID int64, limit int) ([]*model.TaskExecLog, error) {
	return nil, nil
}

// =============================================================================
// 测试用例
// =============================================================================

// TestScheduler_New 测试创建调度器
// 场景说明：验证调度器能正确创建
func TestScheduler_New(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	require.NotNil(t, sched)
	assert.False(t, sched.IsRunning())
}

// TestScheduler_StartStop 测试启动和停止
// 场景说明：验证调度器能正确启动和停止
func TestScheduler_StartStop(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)

	// 启动
	pool.Start()
	err := sched.Start()
	require.NoError(t, err)
	assert.True(t, sched.IsRunning())

	// 停止
	sched.Stop()
	assert.False(t, sched.IsRunning())
	pool.Stop()
}

// TestScheduler_AddTask 测试添加任务
// 场景说明：验证任务能被正确添加到调度器
func TestScheduler_AddTask(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 添加任务
	task := &model.Task{
		ID:       1,
		Name:     "test-task",
		CronExpr: "*/5 * * * * *",
		Status:   model.StatusPending,
	}
	err = sched.AddTask(task)
	require.NoError(t, err)

	assert.Equal(t, 1, sched.TaskCount())
}

// TestScheduler_RemoveTask 测试移除任务
// 场景说明：验证任务能从调度器中正确移除
func TestScheduler_RemoveTask(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 添加任务
	task := &model.Task{
		ID:       1,
		Name:     "test-task",
		CronExpr: "*/5 * * * * *",
		Status:   model.StatusPending,
	}
	err = sched.AddTask(task)
	require.NoError(t, err)

	// 移除任务
	err = sched.RemoveTask(task.ID)
	require.NoError(t, err)
	assert.Equal(t, 0, sched.TaskCount())
}

// TestScheduler_ListActive 测试加载活跃任务
// 场景说明：验证调度器启动时能正确加载 pending 状态的任务
func TestScheduler_ListActive(t *testing.T) {
	tests := []struct {
		name        string
		tasks       []*model.Task
		wantCount   int
		description string
	}{
		{
			name: "load pending tasks",
			tasks: []*model.Task{
				{ID: 1, Name: "task1", CronExpr: "*/5 * * * * *", Status: model.StatusPending},
				{ID: 2, Name: "task2", CronExpr: "*/10 * * * * *", Status: model.StatusPending},
			},
			wantCount:   2,
			description: "应该加载所有 pending 状态的任务",
		},
		{
			name: "skip completed tasks",
			tasks: []*model.Task{
				{ID: 1, Name: "task1", CronExpr: "*/5 * * * * *", Status: model.StatusCompleted},
				{ID: 2, Name: "task2", CronExpr: "*/10 * * * * *", Status: model.StatusPending},
			},
			wantCount:   1,
			description: "应该跳过已完成的任务",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStore := NewMockTaskStore()
			for _, task := range tt.tasks {
				mockStore.tasks[int64(task.ID)] = task
			}

			pool := worker.NewPool(2, 10, worker.NoOpHandler)
			sched := NewScheduler(mockStore, pool)

			pool.Start()
			defer pool.Stop()

			err := sched.Start()
			require.NoError(t, err)
			defer sched.Stop()

			assert.Equal(t, tt.wantCount, sched.TaskCount())
		})
	}
}

// TestScheduler_DoubleStart 测试重复启动
// 场景说明：验证重复启动是安全的（幂等性）
func TestScheduler_DoubleStart(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	// 第一次启动
	err := sched.Start()
	require.NoError(t, err)

	// 第二次启动应该安全
	err = sched.Start()
	require.NoError(t, err)

	sched.Stop()
}

// TestScheduler_TaskCount 测试任务计数
// 场景说明：验证任务计数正确
func TestScheduler_TaskCount(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 初始为 0
	assert.Equal(t, 0, sched.TaskCount())

	// 添加任务
	task := &model.Task{
		ID:       1,
		Name:     "test-task",
		CronExpr: "*/5 * * * * *",
		Status:   model.StatusPending,
	}
	err = sched.AddTask(task)
	require.NoError(t, err)

	assert.Equal(t, 1, sched.TaskCount())
}

// TestScheduler_DuplicateAdd 测试重复添加任务
// 场景说明：验证重复添加相同 ID 的任务是幂等的
func TestScheduler_DuplicateAdd(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 添加任务
	task := &model.Task{
		ID:       1,
		Name:     "test-task",
		CronExpr: "*/5 * * * * *",
		Status:   model.StatusPending,
	}

	// 第一次添加
	err = sched.AddTask(task)
	require.NoError(t, err)
	assert.Equal(t, 1, sched.TaskCount())

	// 第二次添加相同 ID 的任务，应该是幂等的
	err = sched.AddTask(task)
	require.NoError(t, err)
	assert.Equal(t, 1, sched.TaskCount())
}

// TestScheduler_InvalidCronExpr 测试无效的 cron 表达式
// 场景说明：验证无效 cron 表达式返回错误
func TestScheduler_InvalidCronExpr(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 添加无效 cron 表达式的任务
	task := &model.Task{
		ID:       1,
		Name:     "invalid-cron-task",
		CronExpr: "invalid-cron",
		Status:   model.StatusPending,
	}
	err = sched.AddTask(task)
	assert.Error(t, err)
	assert.Equal(t, 0, sched.TaskCount())
}

// TestScheduler_RemoveNonExistentTask 测试移除不存在的任务
// 场景说明：验证移除不存在的任务是安全的（幂等）
func TestScheduler_RemoveNonExistentTask(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 移除不存在的任务，应该安全返回
	err = sched.RemoveTask(999)
	require.NoError(t, err)
}

// TestScheduler_DoubleStop 测试重复停止
// 场景说明：验证重复停止是安全的（幂等）
func TestScheduler_DoubleStop(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	err := sched.Start()
	require.NoError(t, err)

	// 第一次停止
	sched.Stop()
	assert.False(t, sched.IsRunning())

	// 第二次停止应该安全
	sched.Stop()
	assert.False(t, sched.IsRunning())
}

// TestScheduler_StartError 测试启动失败
// 场景说明：验证数据库错误时启动失败
func TestScheduler_StartError(t *testing.T) {
	mockStore := NewMockTaskStore()
	mockStore.SetError(assert.AnError)

	pool := worker.NewPool(2, 10, worker.NoOpHandler)
	sched := NewScheduler(mockStore, pool)

	pool.Start()
	defer pool.Stop()

	// 启动应该失败
	err := sched.Start()
	assert.Error(t, err)
	assert.False(t, sched.IsRunning())
}

// SetError 设置 mock 存储返回错误
func (m *MockTaskStore) SetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

// =============================================================================

// MockHandler 是用于测试的 JobHandler
// 记录调用次数和返回结果
type MockHandler struct {
	mu       sync.Mutex
	callCount int
	shouldErr bool
}

// NewMockHandler 创建 mock handler
func NewMockHandler() *MockHandler {
	return &MockHandler{}
}

func (h *MockHandler) Handle(ctx context.Context, taskID uint, payload string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callCount++
	if h.shouldErr {
		return assert.AnError
	}
	return nil
}

// SetShouldErr 设置是否返回错误
func (h *MockHandler) SetShouldErr(shouldErr bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shouldErr = shouldErr
}

// CallCount 返回调用次数
func (h *MockHandler) CallCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.callCount
}

// =============================================================================

// TestScheduler_WithTimezone 测试带时区的调度器
// 场景说明：验证时区选项能正确应用
func TestScheduler_WithTimezone(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	sched := NewScheduler(mockStore, pool, WithTimezone(loc))
	require.NotNil(t, sched)
}

// TestScheduler_RemoveTaskBeforeStart 测试启动前移除任务
// 场景说明：验证在调度器启动前移除任务是安全的
func TestScheduler_RemoveTaskBeforeStart(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)

	// 启动前移除任务，应该安全
	err := sched.RemoveTask(1)
	require.NoError(t, err)
}

// TestScheduler_StopBeforeStart 测试未启动时停止
// 场景说明：验证未启动时停止是安全的
func TestScheduler_StopBeforeStart(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)

	// 未启动时停止，应该安全
	sched.Stop()
	assert.False(t, sched.IsRunning())
}

// TestScheduler_AddTaskBeforeStart 测试启动前添加任务
// 场景说明：验证启动前添加任务，启动后能正确调度
func TestScheduler_AddTaskBeforeStart(t *testing.T) {
	mockStore := NewMockTaskStore()
	pool := worker.NewPool(2, 10, worker.NoOpHandler)

	sched := NewScheduler(mockStore, pool)
	pool.Start()
	defer pool.Stop()

	// 启动前添加任务
	task := &model.Task{
		ID:       1,
		Name:     "test-task",
		CronExpr: "*/5 * * * * *",
		Status:   model.StatusPending,
	}

	err := sched.AddTask(task)
	require.NoError(t, err)

	// 启动调度器
	err = sched.Start()
	require.NoError(t, err)
	defer sched.Stop()

	// 任务应该还在
	assert.Equal(t, 1, sched.TaskCount())
}
