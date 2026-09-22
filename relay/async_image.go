package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/async_task"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// handleAsyncImageRelay 同步上游渠道的 async=1 包装：先建内部任务行并立即返回固定回执，
// 上游转发与响应处理转入后台协程执行，结果经 RelayInfo.ImageCapture 捕获开关落回任务行，
// 客户端随后通过 GET /v1/images/tasks/{task_id} 轮询终态。
//
// 计费语义与同步长连接一致：controller 已预扣，后台协程成功时正常结算（PostTextConsumeQuota）、
// 失败时退款（Billing.Refund，幂等）；退款/结算都以任务行 CAS 终态化为前置，防止与
// task sweep（超时判死+退款）叠加。
//
// 返回 nil 表示回执已写出：调用方（ImageHelper→Relay）视为成功，不再渠道重试、defer 退款不触发。
// 返回错误表示回执未写出：走正常错误路径（退款+可选渠道重试），任务行若已建则先终态化。
func handleAsyncImageRelay(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor,
	requestBody io.Reader, statusCodeMappingStr string, imageReq *dto.ImageRequest) *types.NewAPIError {

	// 物化请求体：pass-through 模式下 requestBody 是请求体存储的 Reader，
	// BodyStorageCleanup 中间件会在本次响应结束后关闭存储，后台协程再读会得到 ErrStorageClosed。
	rawBody, err := io.ReadAll(requestBody)
	if err != nil {
		return types.NewError(fmt.Errorf("read request body for async image task failed: %w", err), types.ErrorCodeReadRequestBodyFailed)
	}
	requestBody = bytes.NewReader(rawBody)

	now := time.Now().Unix()
	task := &model.Task{
		TaskID:     model.GenerateTaskID(),
		Platform:   constant.TaskPlatformAsyncTask,
		UserId:     info.UserId,
		Group:      info.TokenGroup,
		ChannelId:  info.ChannelId,
		Quota:      info.PriceData.Quota,
		Action:     "generate",
		Status:     model.TaskStatusSubmitted,
		SubmitTime: now,
		StartTime:  now,
		Progress:   "0%",
		Properties: model.Properties{
			OriginModelName:   info.OriginModelName,
			UpstreamModelName: info.UpstreamModelName,
		},
	}
	if err := task.Insert(); err != nil {
		return types.NewError(fmt.Errorf("insert async image task failed: %w", err), types.ErrorCodeUpdateDataError)
	}

	receipt, err := common.Marshal(dto.ImageTaskSubmitReceipt{
		TaskID: task.TaskID,
		Status: "submitted",
	})
	if err != nil {
		async_task.MarkTaskFailed(task, "marshal receipt failed: "+err.Error())
		return types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(http.StatusOK)
	if _, werr := c.Writer.Write(receipt); werr != nil {
		// 回执不可达（客户端已断开）：立即终态化任务行，防止 task sweep 超时后再次退款
		// 与本函数返回错误触发的 controller defer 退款叠加；Billing.Refund 幂等兜底。
		async_task.MarkTaskFailed(task, "receipt write failed: "+werr.Error())
		return types.NewOpenAIError(werr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	common.SysLog(fmt.Sprintf("[async-image] task=%s channel=%d model=%s submitted, execution moved to background",
		task.TaskID, info.ChannelId, info.OriginModelName))

	// c.Copy() 官方安全副本：Keys 为浅拷贝、Writer 底层为 nil（绝不可写，写响应一律走捕获开关）。
	// 上游 http 调用不携带 ctx（doRequest 用无 ctx 的 http.Request），客户端断开不影响后台执行。
	cCopy := c.Copy()
	asyncInfo := *info
	asyncInfo.Request = nil // 内存卫生：不长期持有 MB 级 b64 参考图载荷
	asyncInfo.ImageCapture = &dto.ImageResponse{}

	go func() {
		billing := asyncInfo.Billing

		defer func() {
			if r := recover(); r != nil {
				common.SysLog(fmt.Sprintf("[async-image] task=%s panic: %v", task.TaskID, r))
				if async_task.MarkTaskFailed(task, fmt.Sprintf("internal error: %v", r)) && billing != nil {
					billing.Refund(cCopy)
				}
			}
		}()

		// fail 终态化任务行；仅在 CAS 赢得所有权时退款（行已被 sweep 判终时其退款已发生）
		fail := func(reason string) {
			reason = truncateReason(reason)
			if async_task.MarkTaskFailed(task, reason) {
				common.SysLog(fmt.Sprintf("[async-image] task=%s failed: %s", task.TaskID, reason))
				if billing != nil {
					billing.Refund(cCopy)
				}
			}
		}

		resp, err := adaptor.DoRequest(cCopy, &asyncInfo, requestBody)
		if err != nil {
			fail("upstream request failed: " + err.Error())
			return
		}
		httpResp, ok := resp.(*http.Response)
		if !ok || httpResp == nil {
			fail("upstream returned invalid response")
			return
		}
		if httpResp.StatusCode != http.StatusOK {
			// replicate 渠道 Prefer: wait 时返回 201 Created，按成功处理（与 ImageHelper 一致）
			if httpResp.StatusCode == http.StatusCreated && asyncInfo.ApiType == constant.APITypeReplicate {
				httpResp.StatusCode = http.StatusOK
			} else {
				apiErr := service.RelayErrorHandler(context.Background(), httpResp, false)
				service.ResetStatusCode(apiErr, statusCodeMappingStr)
				fail(apiErr.Error())
				return
			}
		}

		// DoResponse 内部经 WriteImageResponse 命中 ImageCapture 捕获开关：只做 COS 自动转存并暂存结果
		usage, apiErr := adaptor.DoResponse(cCopy, httpResp, &asyncInfo)
		if apiErr != nil {
			service.ResetStatusCode(apiErr, statusCodeMappingStr)
			fail(apiErr.Error())
			return
		}

		if len(asyncInfo.ImageCapture.Data) == 0 {
			fail("upstream succeeded but no image data captured")
			return
		}

		// 以下镜像 ImageHelper 成功路径的计费参数准备（n-ratio / usage 兜底 / 日志内容）
		imageN := uint(1)
		if imageReq.N != nil {
			imageN = *imageReq.N
		}
		if asyncInfo.PriceData.UsePrice {
			if _, hasN := asyncInfo.PriceData.OtherRatios["n"]; !hasN {
				asyncInfo.PriceData.AddOtherRatio("n", float64(imageN))
			}
		}
		typedUsage, usageOk := usage.(*dto.Usage)
		if !usageOk || typedUsage == nil {
			typedUsage = &dto.Usage{}
		}
		if typedUsage.TotalTokens == 0 {
			typedUsage.TotalTokens = 1
		}
		if typedUsage.PromptTokens == 0 {
			typedUsage.PromptTokens = 1
		}
		quality := "standard"
		if imageReq.Quality == "hd" {
			quality = "hd"
		}
		var logContent []string
		if len(imageReq.Size) > 0 {
			logContent = append(logContent, fmt.Sprintf("大小 %s", imageReq.Size))
		}
		if len(quality) > 0 {
			logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
		}
		if imageN > 0 {
			logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
		}

		resultBytes, merr := common.Marshal(asyncInfo.ImageCapture)
		if merr != nil {
			fail("marshal captured image response failed: " + merr.Error())
			return
		}
		// 先 CAS 终态化再结算：任务行若已被 sweep 判终（已退款），不再计费
		if async_task.MarkTaskSucceeded(task, json.RawMessage(resultBytes)) {
			common.SysLog(fmt.Sprintf("[async-image] task=%s succeeded, settling quota", task.TaskID))
			service.PostTextConsumeQuota(cCopy, &asyncInfo, typedUsage, logContent)
		}
	}()

	return nil
}

// truncateReason 控制任务行 fail_reason 长度，避免异常上游把超大 body 写入 DB。
func truncateReason(reason string) string {
	const maxLen = 500
	runes := []rune(reason)
	if len(runes) <= maxLen {
		return reason
	}
	return string(runes[:maxLen]) + "..."
}
