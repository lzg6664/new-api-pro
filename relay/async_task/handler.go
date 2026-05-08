package async_task

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// HandleAsyncTaskSubmit 处理异步任务提交。
// 在 OpenaiHandler 等 response handler 中检测到任务响应后调用。
// 返回 error 表示存储失败（此时客户端尚未收到响应）。
func HandleAsyncTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo,
	upstreamTaskID string, rawBody []byte, config *dto.ChannelAsyncTaskConfig) error {

	if info == nil || config == nil {
		return fmt.Errorf("HandleAsyncTaskSubmit: info and config must not be nil")
	}

	config.Defaults()

	// 1. 生成公开 task ID
	publicTaskID := model.GenerateTaskID()

	// 2. 生成 Data（存储原始响应用于后续轮询）
	taskData := TaskAsyncSubmitData{
		UpstreamTaskID: upstreamTaskID,
		RawSubmitBody:  json.RawMessage(rawBody),
	}

	// 3. 取 action（TaskRelayInfo 在 OpenAI 通道上下文中为 nil，使用默认值）
	action := "generate"
	if info.TaskRelayInfo != nil && info.Action != "" {
		action = info.Action
	}

	// 4. 构建 task 并入库
	task := &model.Task{
		TaskID:     publicTaskID,
		Platform:   constant.TaskPlatform(strconv.Itoa(info.ChannelType)),
		UserId:     info.UserId,
		Group:      info.TokenGroup,
		ChannelId:  info.ChannelId,
		Quota:      info.PriceData.Quota,
		Action:     action,
		Status:     model.TaskStatusSubmitted,
		SubmitTime: time.Now().Unix(),
		StartTime:  time.Now().Unix(),
		Progress:   "0%",
		Properties: model.Properties{
			OriginModelName:   info.OriginModelName,
			UpstreamModelName: info.UpstreamModelName,
		},
	}
	task.SetData(&taskData)
	if err := task.Insert(); err != nil {
		return err
	}

	// 5. 结算计费：任务提交成功即 settle 预扣额
	if info.Billing != nil {
		quota := info.PriceData.Quota
		if quota <= 0 {
			quota = task.Quota
		}
		if quota > 0 {
			if err := service.SettleBilling(c, info, quota); err != nil {
				logger.LogError(c, "async task settle billing failed: "+err.Error())
			}
		}
	}

	// 6. 同步轮询上游，等待最终结果
	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	result, err := PollSynchronously(baseURL, info.ApiKey, upstreamTaskID, config)
	if err != nil {
		// 轮询失败，更新任务状态并返回错误
		failTask(task, err.Error())
		return err
	}

	// 7. 构建标准 OpenAI ImageResponse 返回给客户端
	imageResp := dto.ImageResponse{
		Data:    make([]dto.ImageData, 0, len(result.ImageURLs)),
		Created: time.Now().Unix(),
	}
	for _, url := range result.ImageURLs {
		imageResp.Data = append(imageResp.Data, dto.ImageData{Url: url})
	}

	// 更新任务状态为成功
	resultBytes, _ := common.Marshal(imageResp)
	succeedTask(task, json.RawMessage(resultBytes), string(result.RawBody))

	if newAPIError := relaycommon.WriteImageResponse(c, info, http.StatusOK, &imageResp); newAPIError != nil {
		return newAPIError
	}
	return nil
}
