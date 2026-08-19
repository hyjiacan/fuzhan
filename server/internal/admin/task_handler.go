package admin

import (
	"net/http"
	"strconv"

	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/utils"

	"github.com/gin-gonic/gin"
)

// TaskHandler 任务管理处理器
type TaskHandler struct {
	taskService *services.TaskService
}

// NewTaskHandler 创建任务管理处理器
func NewTaskHandler(taskService *services.TaskService) *TaskHandler {
	return &TaskHandler{taskService: taskService}
}

// ListTasks 获取任务列表（活跃 + 最近历史）
func (h *TaskHandler) ListTasks(c *gin.Context) {
	active, err := h.taskService.GetActiveTasks()
	if err != nil {
		utils.HandleInternalServerError(c, "获取活跃任务失败")
		return
	}

	recent, err := h.taskService.GetRecentTasks(20)
	if err != nil {
		utils.HandleInternalServerError(c, "获取最近任务失败")
		return
	}

	activeResp := make([]models.TaskRecordResponse, len(active))
	for i, t := range active {
		activeResp[i] = t.ToResponse()
	}

	recentResp := make([]models.TaskRecordResponse, len(recent))
	for i, t := range recent {
		recentResp[i] = t.ToResponse()
	}

	utils.HandleSuccess(c, http.StatusOK, "", models.TaskListResponse{
		Active: activeResp,
		Recent: recentResp,
	})
}

// GetTask 获取单个任务详情
func (h *TaskHandler) GetTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	task, err := h.taskService.GetTaskByID(uint(id))
	if err != nil {
		utils.HandleNotFound(c, "任务不存在")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "", task.ToResponse())
}

// GetTaskHistory 获取任务历史（分页）
func (h *TaskHandler) GetTaskHistory(c *gin.Context) {
	taskType := c.Query("type")
	pageStr := c.DefaultQuery("page", "1")
	pageSizeStr := c.DefaultQuery("pageSize", "20")

	page, _ := strconv.Atoi(pageStr)
	pageSize, _ := strconv.Atoi(pageSizeStr)

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	tasks, total, err := h.taskService.GetTaskHistory(taskType, page, pageSize)
	if err != nil {
		utils.HandleInternalServerError(c, "获取任务历史失败")
		return
	}

	resp := make([]models.TaskRecordResponse, len(tasks))
	for i, t := range tasks {
		resp[i] = t.ToResponse()
	}

	utils.HandleSuccess(c, http.StatusOK, "", gin.H{
		"items": resp,
		"total": total,
		"page":  page,
	})
}

// CancelTask 取消任务
func (h *TaskHandler) CancelTask(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		utils.HandleBadRequest(c, "无效的任务ID", nil)
		return
	}

	if err := h.taskService.CancelTask(uint(id)); err != nil {
		utils.HandleInternalServerError(c, "取消任务失败")
		return
	}

	utils.HandleSuccess(c, http.StatusOK, "任务已取消", nil)
}