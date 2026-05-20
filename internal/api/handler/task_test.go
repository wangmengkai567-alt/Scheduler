// Package handler 提供 HTTP 请求处理器
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gtask-scheduler/internal/model"
	customerrors "gtask-scheduler/pkg/errors"
)

// MockTaskStore 是 TaskStoreInterface 的 mock 实现
type MockTaskStore struct {
	tasks    map[int64]*model.Task
	nextID   int64
	err      error
	notFound bool
}

// NewMockTaskStore 创建 mock 存储
func NewMockTaskStore() *MockTaskStore {
	return &MockTaskStore{
		tasks:  make(map[int64]*model.Task),
		nextID: 1,
	}
}

func (m *MockTaskStore) Create(ctx context.Context, task *model.Task) error {
	if m.err != nil {
		return m.err
	}
	task.ID = uint(m.nextID)
	m.tasks[m.nextID] = task
	m.nextID++
	return nil
}

func (m *MockTaskStore) GetByID(ctx context.Context, id int64) (*model.Task, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.notFound {
		return nil, customerrors.NewNotFound("task", id)
	}
	if task, ok := m.tasks[id]; ok {
		return task, nil
	}
	return nil, customerrors.NewNotFound("task", id)
}

func (m *MockTaskStore) List(ctx context.Context) ([]*model.Task, error) {
	if m.err != nil {
		return nil, m.err
	}
	var result []*model.Task
	for _, t := range m.tasks {
		result = append(result, t)
	}
	return result, nil
}

func (m *MockTaskStore) ListActive(ctx context.Context) ([]*model.Task, error) {
	return nil, nil
}

func (m *MockTaskStore) UpdateStatus(ctx context.Context, id int64, status model.TaskStatus) error {
	return nil
}

func (m *MockTaskStore) UpdateNextRunAt(ctx context.Context, id int64, t time.Time) error {
	return nil
}

func (m *MockTaskStore) SaveExecLog(ctx context.Context, log *model.TaskExecLog) error {
	return nil
}

func (m *MockTaskStore) ListExecLogs(ctx context.Context, taskID int64, limit int) ([]*model.TaskExecLog, error) {
	return nil, nil
}

// =============================================================================
// 测试用例
// =============================================================================

func init() {
	gin.SetMode(gin.TestMode)
}

// TestCreateTask_Success 测试创建任务成功
// 场景说明：验证有效请求返回 200 和正确的 JSON 结构
func TestCreateTask_Success(t *testing.T) {
	tests := []struct {
		name        string
		requestBody map[string]interface{}
		wantStatus  int
		wantName    string
		description string
	}{
		{
			name: "create task with all fields",
			requestBody: map[string]interface{}{
				"name":       "test-task",
				"cron_expr":  "*/5 * * * *",
				"payload":    `{"key": "value"}`,
				"max_retries": 3,
			},
			wantStatus:  200,
			wantName:    "test-task",
			description: "完整参数创建任务应该成功",
		},
		{
			name: "create task with minimal fields",
			requestBody: map[string]interface{}{
				"name":      "minimal-task",
				"cron_expr": "*/10 * * * *",
			},
			wantStatus:  200,
			wantName:    "minimal-task",
			description: "最小参数创建任务应该成功",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 准备 mock
			mockStore := NewMockTaskStore()
			handler := NewTaskHandler(mockStore)

			// 创建路由
			router := gin.New()
			api := router.Group("/api/v1")
			handler.RegisterRoutes(api)

			// 准备请求
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// 执行请求
			router.ServeHTTP(w, req)

			// 验证响应
			assert.Equal(t, tt.wantStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, float64(0), response["code"])
			assert.Equal(t, "success", response["message"])
			assert.NotNil(t, response["data"])

			data := response["data"].(map[string]interface{})
			assert.Equal(t, tt.wantName, data["name"])
		})
	}
}

// TestCreateTask_InvalidParam 测试创建任务参数错误
// 场景说明：验证无效参数返回 400 和错误码 40000
func TestCreateTask_InvalidParam(t *testing.T) {
	tests := []struct {
		name        string
		requestBody map[string]interface{}
		wantStatus  int
		wantCode    int
		description string
	}{
		{
			name: "missing name",
			requestBody: map[string]interface{}{
				"cron_expr": "*/5 * * * *",
			},
			wantStatus:  400,
			wantCode:    40000,
			description: "缺少 name 应该返回 400",
		},
		{
			name: "missing cron_expr",
			requestBody: map[string]interface{}{
				"name": "test-task",
			},
			wantStatus:  400,
			wantCode:    40000,
			description: "缺少 cron_expr 应该返回 400",
		},
		{
			name:        "empty body",
			requestBody: map[string]interface{}{},
			wantStatus:  400,
			wantCode:    40000,
			description: "空请求体应该返回 400",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 准备 mock
			mockStore := NewMockTaskStore()
			handler := NewTaskHandler(mockStore)

			// 创建路由
			router := gin.New()
			api := router.Group("/api/v1")
			handler.RegisterRoutes(api)

			// 准备请求
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// 执行请求
			router.ServeHTTP(w, req)

			// 验证响应
			assert.Equal(t, tt.wantStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Equal(t, float64(tt.wantCode), response["code"])
		})
	}
}

// TestGetTask_Success 测试获取任务成功
// 场景说明：验证获取已存在的任务返回 200
func TestGetTask_Success(t *testing.T) {
	// 准备 mock
	mockStore := NewMockTaskStore()
	task := &model.Task{
		Name:       "test-task",
		CronExpr:   "*/5 * * * *",
		Status:     model.StatusPending,
		MaxRetries: 3,
	}
	mockStore.Create(context.Background(), task)

	handler := NewTaskHandler(mockStore)

	// 创建路由
	router := gin.New()
	api := router.Group("/api/v1")
	handler.RegisterRoutes(api)

	// 准备请求
	req := httptest.NewRequest("GET", "/api/v1/tasks/1", nil)
	w := httptest.NewRecorder()

	// 执行请求
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(0), response["code"])
	data := response["data"].(map[string]interface{})
	assert.Equal(t, "test-task", data["name"])
}

// TestGetTask_NotFound 测试获取不存在的任务
// 场景说明：验证获取不存在的任务返回 404 和错误码 40400
func TestGetTask_NotFound(t *testing.T) {
	// 准备 mock
	mockStore := NewMockTaskStore()
	mockStore.notFound = true

	handler := NewTaskHandler(mockStore)

	// 创建路由
	router := gin.New()
	api := router.Group("/api/v1")
	handler.RegisterRoutes(api)

	// 准备请求
	req := httptest.NewRequest("GET", "/api/v1/tasks/999", nil)
	w := httptest.NewRecorder()

	// 执行请求
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, 404, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(40400), response["code"])
	assert.Contains(t, response["message"], "not found")
}

// TestGetTask_InvalidID 测试无效的任务 ID
// 场景说明：验证无效 ID 返回 400
func TestGetTask_InvalidID(t *testing.T) {
	// 准备 mock
	mockStore := NewMockTaskStore()
	handler := NewTaskHandler(mockStore)

	// 创建路由
	router := gin.New()
	api := router.Group("/api/v1")
	handler.RegisterRoutes(api)

	tests := []struct {
		name    string
		url     string
		wantStatus int
	}{
		{
			name:       "invalid id format",
			url:        "/api/v1/tasks/abc",
			wantStatus: 400,
		},
		{
			name:       "zero id",
			url:        "/api/v1/tasks/0",
			wantStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

// TestListTasks_Success 测试获取任务列表成功
// 场景说明：验证获取任务列表返回正确格式
func TestListTasks_Success(t *testing.T) {
	// 准备 mock
	mockStore := NewMockTaskStore()
	mockStore.Create(context.Background(), &model.Task{Name: "task1", CronExpr: "*/5 * * * *"})
	mockStore.Create(context.Background(), &model.Task{Name: "task2", CronExpr: "*/10 * * * *"})

	handler := NewTaskHandler(mockStore)

	// 创建路由
	router := gin.New()
	api := router.Group("/api/v1")
	handler.RegisterRoutes(api)

	// 准备请求
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()

	// 执行请求
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, float64(0), response["code"])
	data := response["data"].([]interface{})
	assert.Len(t, data, 2)
}

// TestListTasks_Empty 测试空任务列表
// 场景说明：验证空列表返回空数组
func TestListTasks_Empty(t *testing.T) {
	// 准备 mock
	mockStore := NewMockTaskStore()
	handler := NewTaskHandler(mockStore)

	// 创建路由
	router := gin.New()
	api := router.Group("/api/v1")
	handler.RegisterRoutes(api)

	// 准备请求
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()

	// 执行请求
	router.ServeHTTP(w, req)

	// 验证响应
	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	data := response["data"].([]interface{})
	assert.Len(t, data, 0)
}

// TestHealthCheck 测试健康检查
// 场景说明：验证健康检查端点返回正确状态
func TestHealthCheck(t *testing.T) {
	router := gin.New()
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	assert.Equal(t, "ok", response["status"])
}
