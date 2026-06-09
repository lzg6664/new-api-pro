package async_task

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/dto"
)

// TryDetectAsyncTask 检测响应体是否是一个异步任务提交响应。
// 如果检测成功，返回上游 taskId 和原始响应体（用于存储）。
// 如果不是任务响应，返回空字符串。
func TryDetectAsyncTask(body []byte, config *dto.ChannelAsyncTaskConfig) (taskID string, rawBody []byte) {
	if !config.IsActive() {
		return "", nil
	}

	// 用路径提取 taskId
	taskID = extractByPath(body, config.TaskIDPath)
	if taskID == "" && looksLikeAsyncTaskResponse(body, config) {
		taskID = firstPathValue(body, []string{"id", "task_id", "taskId", "data.id", "data.task_id", "data.taskId"})
	}
	if taskID == "" {
		return "", nil
	}

	// 可选：验证提交是否成功（状态值在成功列表中）
	if len(config.SuccessStatuses) > 0 && config.StatusPath != "" {
		status := extractByPath(body, config.StatusPath)
		if status != "" && !containsStringFold(config.SuccessStatuses, status) {
			// 状态不在成功列表中 → 可能是个错误响应，不是有效的任务提交
			return "", nil
		}
	}

	return taskID, body
}

func looksLikeAsyncTaskResponse(body []byte, config *dto.ChannelAsyncTaskConfig) bool {
	if extractByPath(body, "object") == "generation.task" {
		return true
	}
	status := extractByPath(body, config.StatusPath)
	if status == "" {
		status = extractByPath(body, "status")
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "in_progress", "running", "pending", "processing", "submitted", "completed", "success", "succeeded", "failed", "failure", "error":
		return true
	default:
		return false
	}
}

func firstPathValue(body []byte, paths []string) string {
	for _, path := range paths {
		if value := strings.TrimSpace(extractByPath(body, path)); value != "" {
			return value
		}
	}
	return ""
}

func containsStringFold(values []string, target string) bool {
	if slices.Contains(values, target) {
		return true
	}
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

// ExtractTaskError 从响应中提取错误信息（如果有）
func ExtractTaskError(body []byte, config *dto.ChannelAsyncTaskConfig) (code string, msg string) {
	if config.ErrorCodePath != "" {
		code = extractByPath(body, config.ErrorCodePath)
	}
	if config.ErrorMsgPath != "" {
		msg = extractByPath(body, config.ErrorMsgPath)
	}
	return
}
