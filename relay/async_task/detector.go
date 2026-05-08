package async_task

import (
	"slices"

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
	if taskID == "" {
		return "", nil
	}

	// 可选：验证提交是否成功（状态值在成功列表中）
	if len(config.SuccessStatuses) > 0 && config.StatusPath != "" {
		status := extractByPath(body, config.StatusPath)
		if status != "" && !slices.Contains(config.SuccessStatuses, status) {
			// 状态不在成功列表中 → 可能是个错误响应，不是有效的任务提交
			return "", nil
		}
	}

	return taskID, body
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
