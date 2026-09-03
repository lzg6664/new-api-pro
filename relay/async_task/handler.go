package async_task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
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

var asyncTaskConsumeLogIDs sync.Map

// HandleAsyncTaskSubmit stores the upstream task and either:
// 1) returns the submit response immediately while polling in background; or
// 2) blocks and returns the final image result when sync_mode=true.
func HandleAsyncTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo,
	upstreamTaskID string, rawBody []byte, config *dto.ChannelAsyncTaskConfig) error {

	if c == nil || info == nil || config == nil {
		return fmt.Errorf("HandleAsyncTaskSubmit: context, info and config must not be nil")
	}
	if strings.TrimSpace(upstreamTaskID) == "" {
		return fmt.Errorf("HandleAsyncTaskSubmit: upstream task id must not be empty")
	}

	config.Defaults()

	publicTaskID := model.GenerateTaskID()
	if info.TaskRelayInfo == nil {
		info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	}
	info.PublicTaskID = publicTaskID

	// 只存 UpstreamTaskID：RawSubmitBody 含 b64 参考图（MB 级/行）且全链路无读取方，
	// 存进 tasks 表会让该表随历史任务无界膨胀。字段保留以兼容旧行反序列化。
	taskData := TaskAsyncSubmitData{
		UpstreamTaskID: upstreamTaskID,
	}

	action := "generate"
	if info.TaskRelayInfo != nil && info.Action != "" {
		action = info.Action
	}

	task := &model.Task{
		TaskID:     publicTaskID,
		Platform:   constant.TaskPlatformAsyncTask,
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
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: upstreamTaskID,
		},
	}
	task.SetData(&taskData)
	if err := task.Insert(); err != nil {
		return err
	}

	quota := asyncTaskQuota(info, task)
	recordAsyncTaskSubmitLog(c, info, task, upstreamTaskID, config, quota)

	if info.Billing != nil {
		if quota > 0 {
			if err := service.SettleBilling(c, info, quota); err != nil {
				logger.LogError(c, "async task settle billing failed: "+err.Error())
			}
		}
	}

	submitBody := replaceByPath(rawBody, config.TaskIDPath, publicTaskID)

	common.SysLog(fmt.Sprintf("[async-task] task=%s upstream=%s channel=%d syncMode=%v outputType=%s pollInterval=%ds",
		publicTaskID, upstreamTaskID, info.ChannelId, config.SyncMode, config.OutputType, config.PollIntervalSec))

	if !config.SyncMode {
		pollInfo := *info
		pollInfo.Request = nil // 后台协程不需要请求载荷，避免长期持有 MB 级 b64 参考图
		pollConfig := *config
		go StartTaskPolling(task, &pollInfo, &pollConfig)

		c.Writer.Header().Set("Content-Type", "application/json")
		c.Writer.WriteHeader(http.StatusOK)
		_, err := c.Writer.Write(submitBody)
		return err
	}

	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	result, err := PollSynchronously(c.Request.Context(), baseURL, info.ApiKey, upstreamTaskID, config, info.ChannelId)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// 客户端已断开（调用方超时/取消），响应不可达：脱尾后台把任务推进到真实终态，
			// 不写响应也不直接判 FAILURE——慢任务可能仍在正常生成，只是没人等它了。
			// 收尾协程受同一轮询预算约束，天然有界。
			common.SysLog(fmt.Sprintf("[async-task] task=%s client disconnected (%v), finish in background", publicTaskID, err))
			finishDetachedTask(task, info, config, upstreamTaskID, baseURL)
			return nil
		}
		if failTask(task, err.Error()) {
			updateAsyncTaskConsumeLog(task, "failed", "async task failed", nil, err.Error())
		}
		return err
	}

	imageResp := dto.ImageResponse{
		Data:    make([]dto.ImageData, 0, len(result.ImageData)),
		Created: time.Now().Unix(),
	}
	imageResp.Data = append(imageResp.Data, result.ImageData...)

	resultBytes, _ := common.Marshal(imageResp)
	if succeedTask(task, json.RawMessage(resultBytes), string(result.RawBody)) {
		updateAsyncTaskConsumeLog(task, "succeeded", "async task succeeded", result.ImageURLs, "")
	}

	if newAPIError := relaycommon.WriteImageResponse(c, info, http.StatusOK, &imageResp); newAPIError != nil {
		return newAPIError
	}
	return nil
}

// finishDetachedTask 客户端已断开时的脱尾收尾：后台续跑轮询到终态并落库/更新消费日志，不写响应。
// 复制 info 并丢弃 Request 载荷，防止后台协程长期持有 MB 级 b64 参考图。
func finishDetachedTask(task *model.Task, info *relaycommon.RelayInfo, config *dto.ChannelAsyncTaskConfig, upstreamTaskID, baseURL string) {
	pollInfo := *info
	pollInfo.Request = nil
	pollConfig := *config
	go func() {
		result, err := PollSynchronously(context.Background(), baseURL, pollInfo.ApiKey, upstreamTaskID, &pollConfig, pollInfo.ChannelId)
		if err != nil {
			if failTask(task, "client disconnected; "+err.Error()) {
				updateAsyncTaskConsumeLog(task, "failed", "async task failed (client disconnected)", nil, err.Error())
			}
			return
		}
		imageResp := dto.ImageResponse{
			Data:    make([]dto.ImageData, 0, len(result.ImageData)),
			Created: time.Now().Unix(),
		}
		imageResp.Data = append(imageResp.Data, result.ImageData...)
		resultBytes, _ := common.Marshal(imageResp)
		if succeedTask(task, json.RawMessage(resultBytes), string(result.RawBody)) {
			updateAsyncTaskConsumeLog(task, "succeeded", "async task succeeded", result.ImageURLs, "")
		}
	}()
}

func asyncTaskQuota(info *relaycommon.RelayInfo, task *model.Task) int {
	if info == nil {
		if task == nil {
			return 0
		}
		return task.Quota
	}
	quota := info.PriceData.Quota
	if quota <= 0 && task != nil {
		quota = task.Quota
	}
	return quota
}

func recordAsyncTaskSubmitLog(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, upstreamTaskID string, config *dto.ChannelAsyncTaskConfig, quota int) {
	if c == nil || info == nil || task == nil {
		return
	}
	if quota > 0 {
		model.UpdateUserUsedQuotaAndRequestCount(info.UserId, quota)
		model.UpdateChannelUsedQuota(info.ChannelId, quota)
	}

	log, err := model.CreateConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId:        info.ChannelId,
		PromptTokens:     asyncTaskPromptTokens(info),
		CompletionTokens: 0,
		ModelName:        asyncTaskModelName(info),
		TokenName:        c.GetString("token_name"),
		Quota:            quota,
		Content:          asyncTaskSubmitLogContent(info, config),
		TokenId:          info.TokenId,
		UseTimeSeconds:   asyncTaskUseTimeSeconds(info.StartTime),
		IsStream:         info.IsStream,
		Group:            asyncTaskGroup(info),
		Other:            asyncTaskSubmitLogOther(c, info, task, upstreamTaskID, config),
	})
	if err != nil {
		logger.LogError(c, "async task create consume log failed: "+err.Error())
		return
	}
	if log != nil && log.Id > 0 {
		asyncTaskConsumeLogIDs.Store(task.TaskID, log.Id)
	}
}

func asyncTaskPromptTokens(info *relaycommon.RelayInfo) int {
	if info == nil {
		return 1
	}
	if tokens := info.GetEstimatePromptTokens(); tokens > 0 {
		return tokens
	}
	return 1
}

func asyncTaskModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.OriginModelName != "" {
		return info.OriginModelName
	}
	return info.UpstreamModelName
}

func asyncTaskGroup(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.UsingGroup != "" {
		return info.UsingGroup
	}
	return info.TokenGroup
}

func asyncTaskUseTimeSeconds(start time.Time) int {
	if start.IsZero() {
		return 0
	}
	seconds := time.Now().Unix() - start.Unix()
	if seconds < 0 {
		return 0
	}
	return int(seconds)
}

func asyncTaskSubmitLogContent(info *relaycommon.RelayInfo, config *dto.ChannelAsyncTaskConfig) string {
	parts := []string{"async task submitted"}
	if config != nil && config.OutputType != "" {
		parts = append(parts, "output "+config.OutputType)
	}
	if config != nil && config.SyncMode {
		parts = append(parts, "sync_mode=true")
	}
	if info != nil && info.Action != "" {
		parts = append(parts, "action "+info.Action)
	}
	if info != nil {
		if req, ok := info.Request.(*dto.ImageRequest); ok && req != nil {
			if req.Size != "" {
				parts = append(parts, "size "+req.Size)
			}
			if req.Quality != "" {
				parts = append(parts, "quality "+req.Quality)
			}
			if req.N != nil {
				parts = append(parts, fmt.Sprintf("count %d", *req.N))
			}
		}
	}
	return strings.Join(parts, ", ")
}

func asyncTaskSubmitLogOther(c *gin.Context, info *relaycommon.RelayInfo, task *model.Task, upstreamTaskID string, config *dto.ChannelAsyncTaskConfig) map[string]interface{} {
	other := map[string]interface{}{
		"async_task":           true,
		"async_task_status":    "submitted",
		"task_id":              task.TaskID,
		"upstream_task_id":     upstreamTaskID,
		"model_price":          info.PriceData.ModelPrice,
		"model_ratio":          info.PriceData.ModelRatio,
		"group_ratio":          info.PriceData.GroupRatioInfo.GroupRatio,
		"user_group_ratio":     info.PriceData.GroupRatioInfo.GroupSpecialRatio,
		"billing_source":       info.BillingSource,
		"subscription_id":      info.SubscriptionId,
		"subscription_plan_id": info.SubscriptionPlanId,
	}
	if config != nil {
		other["sync_mode"] = config.SyncMode
		other["output_type"] = config.OutputType
		other["query_path"] = config.QueryPath
	}
	if c != nil && c.Request != nil && c.Request.URL != nil {
		other["request_path"] = c.Request.URL.Path
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	if len(info.PriceData.OtherRatios) > 0 {
		other["other_ratios"] = info.PriceData.OtherRatios
	}
	if c != nil {
		if useChannel := c.GetStringSlice("use_channel"); len(useChannel) > 0 {
			other["admin_info"] = map[string]interface{}{
				"use_channel": useChannel,
			}
		}
	}
	return other
}

func updateAsyncTaskConsumeLog(task *model.Task, status string, content string, resultURLs []string, errorMessage string) {
	if task == nil || task.TaskID == "" {
		return
	}
	logIDValue, ok := asyncTaskConsumeLogIDs.Load(task.TaskID)
	if !ok {
		return
	}
	logID, ok := logIDValue.(int)
	if !ok || logID <= 0 {
		return
	}
	// 终态只更新一次，处理完即从 map 删除，防止 asyncTaskConsumeLogIDs 随任务数无限增长
	defer asyncTaskConsumeLogIDs.Delete(task.TaskID)

	other := map[string]interface{}{
		"async_task_status": status,
		"task_finished_at":  time.Now().Unix(),
	}
	if len(resultURLs) > 0 {
		other["result_urls"] = resultURLs
	}
	other["result_count"] = len(resultURLs)
	if errorMessage != "" {
		other["error_message"] = errorMessage
		other["reason"] = errorMessage
	}
	if content == "" {
		content = "async task " + status
	}
	useTimeSeconds := 0
	if task.SubmitTime > 0 {
		elapsed := time.Now().Unix() - task.SubmitTime
		if elapsed > 0 {
			useTimeSeconds = int(elapsed)
		}
	}
	if err := model.UpdateConsumeLog(logID, model.UpdateConsumeLogParams{
		Content:        content,
		UseTimeSeconds: useTimeSeconds,
		Other:          other,
	}); err != nil {
		common.SysLog("async task update consume log failed: " + err.Error())
	}
}
