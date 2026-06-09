package async_task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// TaskAsyncSubmitData is stored in Task.Data for async task submissions.
type TaskAsyncSubmitData struct {
	UpstreamTaskID string          `json:"upstream_task_id"`
	RawSubmitBody  json.RawMessage `json:"raw_submit_body,omitempty"`
}

// StartTaskPolling 后台轮询异步任务状态，直到终态或超时。
// task 必须是已入库的任务对象（用于获取 ID 和初始状态）。
func StartTaskPolling(task *model.Task, info *relaycommon.RelayInfo, config *dto.ChannelAsyncTaskConfig) {
	config.Defaults()

	interval := time.Duration(config.PollIntervalSec) * time.Second
	if interval < 3*time.Second {
		interval = 3 * time.Second
	}
	maxAttempts := config.MaxPollAttempts
	if maxAttempts < 1 {
		maxAttempts = 120
	}

	// 解析提交数据获取 upstream taskId
	submitData, err := parseSubmitDataFromTask(task)
	if err != nil {
		failTask(task, "解析任务数据失败: "+err.Error())
		return
	}
	upstreamTaskID := submitData.UpstreamTaskID

	baseURL := strings.TrimRight(info.ChannelBaseUrl, "/")
	lastPollFailure := ""

	for attempt := 0; attempt < maxAttempts; attempt++ {
		time.Sleep(interval)

		// 查询上游任务状态
		resp, err := doQueryRequest(baseURL, info.ApiKey, upstreamTaskID, config)
		if err != nil {
			lastPollFailure = "request failed: " + err.Error()
			common.SysLog(fmt.Sprintf("async_task poll attempt %d/%d failed: %s", attempt+1, maxAttempts, err.Error()))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastPollFailure = "read response failed: " + err.Error()
			common.SysLog(fmt.Sprintf("async_task poll read body failed: %s", err.Error()))
			continue
		}
		if resp.StatusCode >= http.StatusBadRequest {
			lastPollFailure = buildPollingHTTPFailure(resp.StatusCode, body)
			common.SysLog(fmt.Sprintf("async_task poll attempt %d/%d failed: %s", attempt+1, maxAttempts, lastPollFailure))
			continue
		}

		// 提取状态 → 映射为内部状态
		rawStatus := extractByPath(body, config.StatusPath)
		mapped := mapStatus(rawStatus, config.StatusMap)

		switch mapped {
		case "succeeded", "completed", "success":
			// 提取结果
			results := extractResultList(body, config)
			resultBytes, _ := common.Marshal(results)
			if succeedTask(task, json.RawMessage(resultBytes), string(body)) {
				updateAsyncTaskConsumeLog(task, "succeeded", "async task succeeded", resultURLsFromResults(results, config), "")
			}
			return

		case "failed", "failure", "error":
			errCode, errMsg := extractTaskDiagnostic(body, config)
			if failTask(task, errMsg) {
				updateAsyncTaskConsumeLog(task, "failed", "async task failed", nil, errMsg)
			}
			_ = errCode // 日志记录用
			common.SysLog(fmt.Sprintf("async_task %s failed: [%s] %s", task.TaskID, errCode, errMsg))
			return

		default: // pending, running, queued, unknown
			progress := fmt.Sprintf("%d%%", (attempt+1)*100/maxAttempts)
			newStatus := model.TaskStatus(model.TaskStatusInProgress)
			updateTaskProgress(task, newStatus, progress, body)
		}
	}

	// 超时
	reason := buildPollingTimeoutReason(maxAttempts, lastPollFailure)
	if failTask(task, reason) {
		updateAsyncTaskConsumeLog(task, "failed", "async task failed", nil, reason)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func parseSubmitDataFromTask(task *model.Task) (*TaskAsyncSubmitData, error) {
	var data TaskAsyncSubmitData
	if err := task.GetData(&data); err != nil {
		return nil, err
	}
	if data.UpstreamTaskID == "" {
		return nil, fmt.Errorf("upstream task id is empty")
	}
	return &data, nil
}

func doQueryRequest(baseURL, apiKey, taskID string, config *dto.ChannelAsyncTaskConfig) (*http.Response, error) {
	fullURL := baseURL + "/" + strings.TrimLeft(config.QueryPath, "/")
	fullURL = strings.ReplaceAll(fullURL, "${task_id}", taskID)

	var req *http.Request
	var err error

	switch strings.ToUpper(config.QueryMethod) {
	case "POST":
		bodyMap := make(map[string]string)
		for k, v := range config.QueryBody {
			bodyMap[k] = strings.ReplaceAll(v, "${task_id}", taskID)
		}
		bodyBytes, _ := common.Marshal(bodyMap)
		req, err = http.NewRequest(http.MethodPost, fullURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	default:
		req, err = http.NewRequest(http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, err
		}
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := service.GetHttpClient()
	if client == nil {
		client = http.DefaultClient
	}
	return client.Do(req)
}

func mapStatus(raw string, statusMap map[string]string) string {
	if raw == "" {
		return ""
	}
	if mapped, ok := statusMap[raw]; ok {
		return mapped
	}
	// 大小写不敏感回退
	rawLower := strings.ToLower(raw)
	for k, v := range statusMap {
		if strings.ToLower(k) == rawLower {
			return v
		}
	}
	return rawLower // 返回小写原始值，调用方自行判断
}

func extractResultList(body []byte, config *dto.ChannelAsyncTaskConfig) []map[string]any {
	if config.ResultListPath == "" {
		return nil
	}
	var data any
	if err := common.Unmarshal(body, &data); err != nil {
		return nil
	}
	for _, part := range parsePath(config.ResultListPath) {
		data = navigateField(data, part)
		if data == nil {
			return nil
		}
	}
	arr, ok := data.([]any)
	if !ok {
		return nil
	}
	var results []map[string]any
	for _, item := range arr {
		if m, ok := item.(map[string]any); ok {
			results = append(results, m)
		}
	}
	return results
}

func extractTaskDiagnostic(body []byte, config *dto.ChannelAsyncTaskConfig) (code string, msg string) {
	if config.ErrorCodePath != "" {
		code = extractByPath(body, config.ErrorCodePath)
	}
	if config.ErrorMsgPath != "" {
		msg = extractByPath(body, config.ErrorMsgPath)
	}
	if msg == "" {
		msg = "upstream task failed"
	}
	return
}

// ── DB helpers ───────────────────────────────────────────────────────────────

func buildPollingHTTPFailure(statusCode int, body []byte) string {
	message := compactPollingFailureMessage(string(body))
	if message == "" {
		return fmt.Sprintf("upstream returned HTTP %d", statusCode)
	}
	return fmt.Sprintf("upstream returned HTTP %d: %s", statusCode, message)
}

func buildPollingTimeoutReason(maxAttempts int, lastPollFailure string) string {
	reason := fmt.Sprintf("polling timeout after %d attempts", maxAttempts)
	lastPollFailure = compactPollingFailureMessage(lastPollFailure)
	if lastPollFailure == "" {
		return reason
	}
	return reason + "; last failure: " + lastPollFailure
}

func compactPollingFailureMessage(message string) string {
	message = strings.TrimSpace(common.MaskSensitiveInfo(message))
	if message == "" {
		return ""
	}
	message = strings.Join(strings.Fields(message), " ")
	const maxLen = 500
	runes := []rune(message)
	if len(runes) <= maxLen {
		return message
	}
	return string(runes[:maxLen]) + "..."
}

// updateTaskProgress uses CAS to move the task from a non-terminal state to IN_PROGRESS.
func updateTaskProgress(task *model.Task, newStatus model.TaskStatus, progress string, rawBody []byte) {
	// 重新从 DB 加载最新状态
	current, ok, err := model.GetByOnlyTaskId(task.TaskID)
	if err != nil || !ok {
		return
	}
	// 只有非终态才更新进度
	if current.Status == model.TaskStatusSuccess || current.Status == model.TaskStatusFailure {
		return
	}
	current.Progress = progress
	// 首次从 SUBMITTED 转 IN_PROGRESS 时设置 StartTime
	if current.Status == model.TaskStatusSubmitted && current.StartTime == 0 {
		current.StartTime = time.Now().Unix()
	}
	if newStatus != "" && current.Status != newStatus {
		fromStatus := current.Status
		current.Status = newStatus
		_, _ = current.UpdateWithStatus(fromStatus)
	} else {
		_ = current.Update()
	}
}

// succeedTask marks the task as successful with result data.
func succeedTask(task *model.Task, resultData json.RawMessage, rawResponseBody string) bool {
	current, ok, err := model.GetByOnlyTaskId(task.TaskID)
	if err != nil || !ok {
		return false
	}
	if current.Status == model.TaskStatusSuccess || current.Status == model.TaskStatusFailure {
		return false
	}
	fromStatus := current.Status
	current.Status = model.TaskStatusSuccess
	current.Progress = "100%"
	current.FinishTime = time.Now().Unix()
	if len(resultData) > 0 && string(resultData) != "null" {
		current.Data = resultData
	}
	won, err := current.UpdateWithStatus(fromStatus)
	return err == nil && won
}

// failTask marks the task as failed.
func failTask(task *model.Task, reason string) bool {
	current, ok, err := model.GetByOnlyTaskId(task.TaskID)
	if err != nil || !ok {
		return false
	}
	if current.Status == model.TaskStatusSuccess || current.Status == model.TaskStatusFailure {
		return false
	}
	fromStatus := current.Status
	current.Status = model.TaskStatusFailure
	current.Progress = "100%"
	current.FailReason = reason
	current.FinishTime = time.Now().Unix()
	won, err := current.UpdateWithStatus(fromStatus)
	return err == nil && won
}

// PollSynchronouslyResult 同步轮询的返回结果。
type PollSynchronouslyResult struct {
	ImageURLs []string // 最终提取到的图片 URL 列表
	RawBody   []byte   // 最后一次轮询的原始响应
}

// PollSynchronously 同步轮询上游任务状态，直到终态或超时。
// 阻塞调用方，适用于将异步任务转为同步等待的场景。
func PollSynchronously(baseURL, apiKey, upstreamTaskID string, config *dto.ChannelAsyncTaskConfig) (*PollSynchronouslyResult, error) {
	interval := time.Duration(config.PollIntervalSec) * time.Second
	if interval < 3*time.Second {
		interval = 3 * time.Second
	}
	maxAttempts := config.MaxPollAttempts
	if maxAttempts < 1 {
		maxAttempts = 120
	}

	lastPollFailure := ""
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			time.Sleep(interval)
		}

		resp, err := doQueryRequest(baseURL, apiKey, upstreamTaskID, config)
		if err != nil {
			lastPollFailure = "request failed: " + err.Error()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastPollFailure = "read response failed: " + err.Error()
			continue
		}
		if resp.StatusCode >= http.StatusBadRequest {
			lastPollFailure = buildPollingHTTPFailure(resp.StatusCode, body)
			continue
		}

		rawStatus := extractByPath(body, config.StatusPath)
		mapped := mapStatus(rawStatus, config.StatusMap)

		switch mapped {
		case "succeeded", "completed", "success":
			urls := extractImageURLs(body, config)
			return &PollSynchronouslyResult{ImageURLs: urls, RawBody: body}, nil

		case "failed", "failure", "error":
			errCode, errMsg := extractTaskDiagnostic(body, config)
			return nil, fmt.Errorf("async task failed: [%s] %s", errCode, errMsg)

		default:
			// pending, running, queued — 继续轮询
		}
	}

	return nil, fmt.Errorf("%s", buildPollingTimeoutReason(maxAttempts, lastPollFailure))
}

// extractImageURLs 从最终响应中提取图片 URL 列表。
func extractImageURLs(body []byte, config *dto.ChannelAsyncTaskConfig) []string {
	results := extractResultList(body, config)
	return resultURLsFromResults(results, config)
}

func resultURLsFromResults(results []map[string]any, config *dto.ChannelAsyncTaskConfig) []string {
	if len(results) == 0 {
		return nil
	}
	var urls []string
	for _, item := range results {
		if url, ok := item[config.ResultURLPath].(string); ok && url != "" {
			urls = append(urls, url)
		}
	}
	return urls
}
