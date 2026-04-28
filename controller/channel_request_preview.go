package controller

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ChannelRequestPreview(c *gin.Context) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}

	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	req := dto.RequestPreviewRequest{}
	if err = common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	relayFormat, err := inferPreviewRelayFormat(req.RequestPath)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	previewCtx, err := buildRequestPreviewContext(c, req)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	request, err := relayhelper.GetAndValidateRequest(previewCtx, relayFormat)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	modelName := resolvePreviewModelName(previewCtx, relayFormat, request)
	channelErr := middleware.SetupContextForSelectedChannel(previewCtx, channel, modelName)
	if channelErr != nil {
		common.ApiError(c, channelErr)
		return
	}

	info, err := relaycommon.GenRelayInfo(previewCtx, relayFormat, request, nil)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	info.IsRequestPreview = true

	preview, err := relay.BuildRequestPreview(previewCtx, info)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	common.ApiSuccess(c, dto.RequestPreviewResponse{
		MatchedRoute:       preview.MatchedRoute,
		FinalMethod:        preview.FinalMethod,
		FinalURL:           preview.FinalURL,
		Headers:            redactPreviewHeaders(preview.Headers),
		Body:               redactPreviewBody(preview.Body),
		ParamOverrideAudit: preview.ParamOverrideAudit,
	})
}

func inferPreviewRelayFormat(requestPath string) (types.RelayFormat, error) {
	parsedPath := parsePreviewRequestPath(requestPath)
	switch {
	case strings.HasPrefix(parsedPath, "/v1/messages"):
		return types.RelayFormatClaude, nil
	case strings.HasPrefix(parsedPath, "/v1/responses/compact"):
		return types.RelayFormatOpenAIResponsesCompaction, nil
	case strings.HasPrefix(parsedPath, "/v1/responses"):
		return types.RelayFormatOpenAIResponses, nil
	case strings.HasPrefix(parsedPath, "/v1/embeddings"):
		return types.RelayFormatEmbedding, nil
	case strings.HasPrefix(parsedPath, "/v1/images/"):
		return types.RelayFormatOpenAIImage, nil
	case strings.HasPrefix(parsedPath, "/v1/rerank") || parsedPath == "/rerank":
		return types.RelayFormatRerank, nil
	case strings.HasPrefix(parsedPath, "/v1/audio/"):
		return types.RelayFormatOpenAIAudio, nil
	case strings.HasPrefix(parsedPath, "/v1beta/models/"), strings.HasPrefix(parsedPath, "/v1/models/"):
		return types.RelayFormatGemini, nil
	case strings.HasPrefix(parsedPath, "/v1/chat/completions"),
		strings.HasPrefix(parsedPath, "/v1/completions"),
		strings.HasPrefix(parsedPath, "/v1/moderations"):
		return types.RelayFormatOpenAI, nil
	default:
		return "", fmt.Errorf("unsupported request path for preview: %s", parsedPath)
	}
}

func parsePreviewRequestPath(requestPath string) string {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" {
		return "/"
	}
	parsedURL, err := url.ParseRequestURI(trimmed)
	if err != nil {
		return trimmed
	}
	if parsedURL.Path == "" {
		return "/"
	}
	return parsedURL.Path
}

func extractModelNameFromGeminiPath(requestPath string) string {
	trimmed := strings.TrimSpace(requestPath)
	if trimmed == "" {
		return ""
	}
	modelMarker := "/models/"
	index := strings.Index(trimmed, modelMarker)
	if index < 0 {
		return ""
	}
	rest := trimmed[index+len(modelMarker):]
	if rest == "" {
		return ""
	}
	if colonIndex := strings.Index(rest, ":"); colonIndex >= 0 {
		rest = rest[:colonIndex]
	}
	if slashIndex := strings.Index(rest, "/"); slashIndex >= 0 {
		rest = rest[:slashIndex]
	}
	return strings.TrimSpace(strings.TrimPrefix(rest, "models/"))
}

func buildRequestPreviewContext(current *gin.Context, req dto.RequestPreviewRequest) (*gin.Context, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodPost
	}

	path := strings.TrimSpace(req.RequestPath)
	if path == "" {
		path = "/v1/chat/completions"
	}
	parsedURL, err := url.ParseRequestURI(path)
	if err != nil {
		return nil, err
	}

	contentType := "application/json"
	headers := make(http.Header)
	for key, value := range req.Headers {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		headers.Set(trimmedKey, value)
		if strings.EqualFold(trimmedKey, "Content-Type") && strings.TrimSpace(value) != "" {
			contentType = value
		}
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "application/json") {
		return nil, fmt.Errorf("only application/json preview is supported")
	}
	headers.Set("Content-Type", contentType)

	bodyBytes := []byte(req.Body)
	recorder := httptest.NewRecorder()
	previewCtx, _ := gin.CreateTestContext(recorder)
	previewCtx.Request = &http.Request{
		Method:        method,
		URL:           parsedURL,
		Header:        headers,
		Body:          http.NoBody,
		ContentLength: int64(len(bodyBytes)),
	}
	if len(bodyBytes) > 0 {
		previewCtx.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	copyPreviewContextKey(current, previewCtx, constant.ContextKeyUserId)
	copyPreviewContextKey(current, previewCtx, constant.ContextKeyUserQuota)
	copyPreviewContextKey(current, previewCtx, constant.ContextKeyUserEmail)
	copyPreviewContextKey(current, previewCtx, constant.ContextKeyUserSetting)

	usingGroup := common.GetContextKeyString(current, constant.ContextKeyUsingGroup)
	if usingGroup == "" {
		usingGroup = common.GetContextKeyString(current, constant.ContextKeyUserGroup)
	}
	if usingGroup == "" {
		usingGroup = "default"
	}
	common.SetContextKey(previewCtx, constant.ContextKeyUsingGroup, usingGroup)
	common.SetContextKey(previewCtx, constant.ContextKeyUserGroup, usingGroup)
	common.SetContextKey(previewCtx, constant.ContextKeyRequestStartTime, time.Now())

	return previewCtx, nil
}

func copyPreviewContextKey(src *gin.Context, dst *gin.Context, key constant.ContextKey) {
	if src == nil || dst == nil {
		return
	}
	value, ok := common.GetContextKey(src, key)
	if !ok {
		return
	}
	common.SetContextKey(dst, key, value)
}

func resolvePreviewModelName(c *gin.Context, relayFormat types.RelayFormat, request dto.Request) string {
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		return strings.TrimSpace(req.Model)
	case *dto.ClaudeRequest:
		return strings.TrimSpace(req.Model)
	case *dto.OpenAIResponsesRequest:
		return strings.TrimSpace(req.Model)
	case *dto.OpenAIResponsesCompactionRequest:
		return strings.TrimSpace(req.Model)
	case *dto.ImageRequest:
		return strings.TrimSpace(req.Model)
	case *dto.EmbeddingRequest:
		return strings.TrimSpace(req.Model)
	case *dto.RerankRequest:
		return strings.TrimSpace(req.Model)
	case *dto.AudioRequest:
		return strings.TrimSpace(req.Model)
	default:
		if relayFormat == types.RelayFormatGemini && c != nil && c.Request != nil && c.Request.URL != nil {
			return extractModelNameFromGeminiPath(c.Request.URL.Path)
		}
		return ""
	}
}

func isSensitivePreviewHeader(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch lower {
	case "authorization", "api-key", "x-api-key", "x-goog-api-key", "cookie", "set-cookie", "proxy-authorization":
		return true
	default:
		return strings.Contains(lower, "token") || strings.Contains(lower, "secret")
	}
}

func isSensitivePreviewField(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	switch lower {
	case "authorization", "api_key", "api-key", "x_api_key", "x-api-key", "secret", "secret_key", "secret-id", "secret_id", "cookie":
		return true
	default:
		return strings.Contains(lower, "token") || strings.Contains(lower, "secret")
	}
}

func maskPreviewValue(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parts := strings.SplitN(raw, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return "Bearer ***"
	}
	if len(raw) <= 8 {
		return "***"
	}
	return raw[:4] + "***" + raw[len(raw)-2:]
}

func redactPreviewHeaders(headers map[string]string) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	redacted := make(map[string]string, len(headers))
	for key, value := range headers {
		if isSensitivePreviewHeader(key) {
			redacted[key] = maskPreviewValue(value)
			continue
		}
		redacted[key] = value
	}
	return redacted
}

func redactPreviewBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload any
	if err := common.Unmarshal(body, &payload); err != nil {
		return truncatePreviewString(string(body))
	}
	redactedBytes, err := common.Marshal(redactPreviewValue(payload, ""))
	if err != nil {
		return truncatePreviewString(string(body))
	}
	return string(redactedBytes)
}

func redactPreviewValue(value any, parentKey string) any {
	if isSensitivePreviewField(parentKey) {
		switch typed := value.(type) {
		case string:
			return maskPreviewValue(typed)
		default:
			return "[redacted]"
		}
	}

	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = redactPreviewValue(child, key)
		}
		return result
	case []any:
		result := make([]any, 0, len(typed))
		for _, child := range typed {
			result = append(result, redactPreviewValue(child, parentKey))
		}
		return result
	case string:
		return truncatePreviewString(typed)
	default:
		return value
	}
}

func truncatePreviewString(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "data:image/") && len(trimmed) > 256 {
		return trimmed[:96] + "...<truncated>"
	}
	if looksLikeBase64(trimmed) && len(trimmed) > 512 {
		return trimmed[:96] + "...<truncated>"
	}
	if len(trimmed) > 8192 {
		return trimmed[:512] + "...<truncated>"
	}
	return raw
}

func looksLikeBase64(raw string) bool {
	if len(raw) < 128 {
		return false
	}
	validCount := 0
	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		switch {
		case ch >= 'a' && ch <= 'z':
			validCount++
		case ch >= 'A' && ch <= 'Z':
			validCount++
		case ch >= '0' && ch <= '9':
			validCount++
		case ch == '+', ch == '/', ch == '=', ch == '-', ch == '_':
			validCount++
		}
	}
	return validCount*100/len(raw) >= 95
}
