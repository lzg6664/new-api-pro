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

// HandleAsyncTaskSubmit stores the upstream task and either:
// 1) returns the submit response immediately while polling in background; or
// 2) blocks and returns the final image result when sync_mode=true.
func HandleAsyncTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo,
	upstreamTaskID string, rawBody []byte, config *dto.ChannelAsyncTaskConfig) error {

	if info == nil || config == nil {
		return fmt.Errorf("HandleAsyncTaskSubmit: info and config must not be nil")
	}

	config.Defaults()

	publicTaskID := model.GenerateTaskID()
	info.PublicTaskID = publicTaskID

	taskData := TaskAsyncSubmitData{
		UpstreamTaskID: upstreamTaskID,
		RawSubmitBody:  json.RawMessage(rawBody),
	}

	action := "generate"
	if info.TaskRelayInfo != nil && info.Action != "" {
		action = info.Action
	}

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

	submitBody := replaceByPath(rawBody, config.TaskIDPath, publicTaskID)

	if !config.SyncMode {
		pollInfo := *info
		pollConfig := *config
		go StartTaskPolling(task, &pollInfo, &pollConfig)

		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(http.StatusOK)
		_, err := c.Writer.Write(submitBody)
		return err
	}

	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	result, err := PollSynchronously(baseURL, info.ApiKey, upstreamTaskID, config)
	if err != nil {
		failTask(task, err.Error())
		return err
	}

	imageResp := dto.ImageResponse{
		Data:    make([]dto.ImageData, 0, len(result.ImageURLs)),
		Created: time.Now().Unix(),
	}
	for _, url := range result.ImageURLs {
		imageResp.Data = append(imageResp.Data, dto.ImageData{Url: url})
	}

	resultBytes, _ := common.Marshal(imageResp)
	succeedTask(task, json.RawMessage(resultBytes), string(result.RawBody))

	if newAPIError := relaycommon.WriteImageResponse(c, info, http.StatusOK, &imageResp); newAPIError != nil {
		return newAPIError
	}
	return nil
}
