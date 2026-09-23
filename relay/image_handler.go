package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// asyncImageSubmitTimeout：任务式渠道 clientAsync 提交（POST ...?async=1）的上游硬上限。
// 任务式上游提交本应秒级返回 taskId；60s 已极宽裕（正常观测 <1s）。
// 背景见 2026-09-23 toapis.cn 30 分钟连接黑洞：无上限时 read 挂到 OS 级 TCP 放弃（15~18min），
// 调用方（Java 生图管线 submit-timeout=120s）在窗口内永远等不到回执，任务重试 ×3 全灭。
//
// 不变式：调用方的提交超时（Java yml submit-timeout，现 120s）必须显著大于本值——
// 上限触发后 504 要在调用方断开之前送达；否则回执/错误写进死连接，任务行已建、
// SettleBilling 已结算，成为孤儿。调整任一侧时必须同步评估另一侧。
const asyncImageSubmitTimeout = 60 * time.Second

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	// store original request body JSON for REST route matching
	if rawBody, err := common.Marshal(request); err == nil {
		info.RequestBodyJson = string(rawBody)
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	var requestBody io.Reader

	if model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			if common.DebugEnabled {
				logger.LogDebug(c, fmt.Sprintf("image request body: %s", string(jsonData)))
			}
			requestBody = bytes.NewBuffer(jsonData)
		}
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	// async=1：客户端要求「提交即返回内部 task_id」。渠道已启用 async_task 时，
	// 任务型上游响应会在 DoResponse 内由 HandleAsyncTaskSubmit 的 clientAsync 分支处理；
	// 其余（同步上游）在此分叉，转后台协程执行并立即写回执。
	clientAsync := c.Query("async") == "1"
	channelTaskActive := info.ChannelOtherSettings.AsyncTask != nil && info.ChannelOtherSettings.AsyncTask.IsActive()
	if clientAsync && !channelTaskActive {
		return handleAsyncImageRelay(c, info, adaptor, requestBody, statusCodeMappingStr, request)
	}
	if clientAsync && channelTaskActive {
		// 任务式上游的提交应秒回 taskId。给上游调用加硬上限：黑洞时快速失败并映射 504
		// （触发换渠道重试 + 调用方 Java 的可重试分类），而非 OS 级 TCP 放弃（15min+）。
		// 必须在 wrapper 分支之后设置，绝不泄入 handleAsyncImageRelay 的后台协程——
		// 同步渠道的后台上游调用合法地需要数分钟完成生成。
		info.UpstreamSubmitTimeout = asyncImageSubmitTimeout
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		statusCode := http.StatusInternalServerError
		var opts []types.NewAPIErrorOptions
		if info.UpstreamSubmitTimeout > 0 && errors.Is(err, context.DeadlineExceeded) {
			// clientAsync 提交超上限：504 语义准确，且落在调用方的可重试白名单里。
			// 同时 SkipRetry——绝不请求内换渠道重试：重试链可能超过调用方（Java
			// submit-timeout=120s）的窗口，届时任务行已建、SettleBilling 已结算、
			// 回执写入死连接 → 孤儿任务+重复计费。重试节奏完全交给调用方
			// （每次全新请求，预扣/退还核算干净）。渠道健康统计不受影响（processChannelError 在前）。
			statusCode = http.StatusGatewayTimeout
			opts = append(opts, types.ErrOptionWithSkipRetry())
		}
		return types.NewErrorWithStatusCode(err, types.ErrorCodeDoRequestFailed, statusCode, opts...)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	// Async task handled — skip image-specific billing
	if info.AsyncTaskHandled {
		return nil
	}

	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}

	// n is handled via OtherRatio so it is applied exactly once in quota
	// calculation (both price-based and ratio-based paths).
	// Adaptors may have already set a more accurate count from the
	// upstream response; only set the default when they haven't.
	if info.PriceData.UsePrice { // only price model use N ratio
		if _, hasN := info.PriceData.OtherRatios["n"]; !hasN {
			info.PriceData.AddOtherRatio("n", float64(imageN))
		}
	}

	if usage.(*dto.Usage).TotalTokens == 0 {
		usage.(*dto.Usage).TotalTokens = 1
	}
	if usage.(*dto.Usage).PromptTokens == 0 {
		usage.(*dto.Usage).PromptTokens = 1
	}

	quality := "standard"
	if request.Quality == "hd" {
		quality = "hd"
	}

	var logContent []string

	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), logContent)
	return nil
}
