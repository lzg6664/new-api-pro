package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// GetImageTaskById 查询图片异步任务（platform=async）状态。
// 挂在 relayV1Router 组内（继承 TokenAuth）；用户 id 与任务行 user_id 做归属隔离，
// 非本人、不存在或非 async 平台一律 404，避免任务号探测。
func GetImageTaskById(c *gin.Context) {
	taskId := c.Param("task_id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "get task failed", "type": "server_error"}})
		return
	}
	if !exist || originTask.Platform != constant.TaskPlatformAsyncTask {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "task not found", "type": "invalid_request_error"}})
		return
	}

	resp := dto.ImageTaskQueryResponse{
		TaskID:     originTask.TaskID,
		Status:     strings.ToLower(string(originTask.Status)),
		FailReason: originTask.FailReason,
		Progress:   originTask.Progress,
	}
	if originTask.Status == model.TaskStatusSuccess {
		resp.Data = normalizeImageTaskData(originTask.Data)
	}

	respBytes, err := common.Marshal(resp)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "marshal response failed", "type": "server_error"}})
		return
	}
	c.Data(http.StatusOK, "application/json", respBytes)
}

// normalizeImageTaskData 归一化任务行 Data 列的两种历史形状：
//   - 数组：async_task 后台轮询路径存 extractResultList 的原始结果项（键名沿用上游，
//     按 url/image_url/imageUrl、b64_json/base64/b64/image_base64 兜底提取，与
//     relay/async_task/poller.go imageDataFromResults 的兜底键集一致）；
//   - 对象：sync/wrapper 路径存的 dto.ImageResponse（data[].url/b64_json）；
//     轮询中的 TaskAsyncSubmitData（upstream_task_id 标记）解析后 data 为空，返回 nil。
func normalizeImageTaskData(raw json.RawMessage) []dto.ImageData {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return nil
	}
	switch trimmed[0] {
	case '[':
		var results []map[string]any
		if err := common.Unmarshal(trimmed, &results); err != nil {
			return nil
		}
		items := make([]dto.ImageData, 0, len(results))
		for _, item := range results {
			image := dto.ImageData{
				B64Json: firstStringFromResult(item, []string{"b64_json", "base64", "b64", "image_base64"}),
				Url:     firstStringFromResult(item, []string{"url", "image_url", "imageUrl"}),
			}
			if image.Url != "" || image.B64Json != "" {
				items = append(items, image)
			}
		}
		return items
	case '{':
		var imageResp dto.ImageResponse
		if err := common.Unmarshal(trimmed, &imageResp); err != nil {
			return nil
		}
		return imageResp.Data
	default:
		return nil
	}
}

func firstStringFromResult(data map[string]any, keys []string) string {
	for _, key := range keys {
		if value, ok := data[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
