// Package handler 提供 HTTP 请求处理器
// 设计意图：处理 HTTP 请求，调用业务逻辑，返回统一格式的响应
package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gtask-scheduler/internal/model"
	mysqlstore "gtask-scheduler/internal/store/mysql"
	"gtask-scheduler/pkg/errors"
	"gtask-scheduler/pkg/response"
)

// TaskHandler 任务处理器
// 封装任务相关的 HTTP 处理逻辑
type TaskHandler struct {
	store mysqlstore.TaskStoreInterface
}

// NewTaskHandler 创建任务处理器
func NewTaskHandler(store mysqlstore.TaskStoreInterface) *TaskHandler {
	return &TaskHandler{store: store}
}

// Create 创建任务
// POST /api/v1/tasks
func (h *TaskHandler) Create(c *gin.Context) {
	var req model.TaskCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body: "+err.Error())
		return
	}

	// 构建任务模型
	task := &model.Task{
		Name:       req.Name,
		CronExpr:   req.CronExpr,
		Payload:    req.Payload,
		Status:     model.StatusPending,
		MaxRetries: req.MaxRetries,
	}

	// 设置默认重试次数
	if task.MaxRetries <= 0 {
		task.MaxRetries = 3
	}

	// 保存到数据库
	if err := h.store.Create(c.Request.Context(), task); err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, task.ToResponse())
}

// Get 获取单个任务
// GET /api/v1/tasks/:id
func (h *TaskHandler) Get(c *gin.Context) {
	id, err := parseTaskID(c)
	if err != nil {
		response.BadRequest(c, "invalid task id")
		return
	}

	task, err := h.store.GetByID(c.Request.Context(), int64(id))
	if err != nil {
		response.Error(c, err)
		return
	}

	response.Success(c, task.ToResponse())
}

// List 列表查询任务
// GET /api/v1/tasks
func (h *TaskHandler) List(c *gin.Context) {
	tasks, err := h.store.List(c.Request.Context())
	if err != nil {
		response.Error(c, err)
		return
	}

	// 转换为响应格式
	list := make([]*model.TaskResponse, 0, len(tasks))
	for _, t := range tasks {
		list = append(list, t.ToResponse())
	}

	response.Success(c, list)
}

// Delete 删除任务
// DELETE /api/v1/tasks/:id
func (h *TaskHandler) Delete(c *gin.Context) {
	id, err := parseTaskID(c)
	if err != nil {
		response.BadRequest(c, "invalid task id")
		return
	}

	// 注意：当前接口没有 Delete 方法，这里简化处理
	// 实际项目中应该添加 Delete 方法到接口
	response.SuccessWithMessage(c, "delete not implemented yet", gin.H{"id": id})
}

// RegisterRoutes 注册路由
// 将任务相关的路由注册到 gin.RouterGroup
func (h *TaskHandler) RegisterRoutes(rg *gin.RouterGroup) {
	tasks := rg.Group("/tasks")
	{
		tasks.POST("", h.Create)
		tasks.GET("", h.List)
		tasks.GET("/:id", h.Get)
		tasks.DELETE("/:id", h.Delete)
	}
}

// parseTaskID 从 URL 参数解析任务 ID
func parseTaskID(c *gin.Context) (uint, error) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		return 0, errors.ErrBadRequest
	}
	if id == 0 {
		return 0, errors.NewBadRequest("task id cannot be zero")
	}
	return uint(id), nil
}
