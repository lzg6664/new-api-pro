package relay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"
	relaygemini "github.com/QuantumNous/new-api/relay/channel/gemini"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type RequestPreviewResult struct {
	MatchedRoute       string
	FinalMethod        string
	FinalURL           string
	Headers            map[string]string
	Body               []byte
	ParamOverrideAudit []string
}

func BuildRequestPreview(c *gin.Context, info *relaycommon.RelayInfo) (*RequestPreviewResult, error) {
	if c == nil || info == nil {
		return nil, errors.New("preview context is required")
	}

	info.InitChannelMeta(c)
	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, fmt.Errorf("invalid api type: %d", info.ApiType)
	}
	adaptor.Init(info)

	bodyBytes, err := buildPreviewRequestBody(c, info, adaptor)
	if err != nil {
		return nil, err
	}

	request, err := relaychannel.BuildAPIRequest(adaptor, c, info, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}

	return &RequestPreviewResult{
		MatchedRoute:       info.MatchedRestRouteName,
		FinalMethod:        request.Method,
		FinalURL:           request.URL.String(),
		Headers:            info.ResolvedRequestHeaders,
		Body:               bodyBytes,
		ParamOverrideAudit: info.ParamOverrideAudit,
	}, nil
}

func buildPreviewRequestBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return buildOpenAIPreviewBody(c, info, adaptor)
	case types.RelayFormatClaude:
		return buildClaudePreviewBody(c, info, adaptor)
	case types.RelayFormatGemini:
		return buildGeminiPreviewBody(c, info, adaptor)
	case types.RelayFormatEmbedding:
		return buildEmbeddingPreviewBody(c, info, adaptor)
	case types.RelayFormatOpenAIImage:
		return buildImagePreviewBody(c, info, adaptor)
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return buildResponsesPreviewBody(c, info, adaptor)
	case types.RelayFormatRerank:
		return buildRerankPreviewBody(c, info, adaptor)
	case types.RelayFormatOpenAIAudio:
		return nil, errors.New("audio request preview is not supported yet")
	default:
		return nil, fmt.Errorf("unsupported relay format for preview: %s", info.RelayFormat)
	}
}

func buildPassThroughPreviewBody(c *gin.Context) ([]byte, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	previewBody := make([]byte, len(body))
	copy(previewBody, body)
	return previewBody, nil
}

func isPassThroughPreviewEnabled(info *relaycommon.RelayInfo) bool {
	return model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled
}

func buildOpenAIPreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	openAIReq, ok := info.Request.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(openAIReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	if info.RelayMode == relayconstant.RelayModeChatCompletions &&
		service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		applySystemPromptIfNeeded(c, info, request)
		return buildResponsesPreviewBodyFromChat(c, info, adaptor, request)
	}

	convertedRequest, err := adaptor.ConvertOpenAIRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	if info.ChannelSetting.SystemPrompt != "" {
		if convertedOpenAIRequest, ok := convertedRequest.(*dto.GeneralOpenAIRequest); ok {
			applySystemPromptIfNeeded(c, info, convertedOpenAIRequest)
		}
	}

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildResponsesPreviewBodyFromChat(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor, request *dto.GeneralOpenAIRequest) ([]byte, error) {
	chatJSON, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	chatJSON, err = relaycommon.RemoveDisabledFields(chatJSON, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		chatJSON, err = relaycommon.ApplyParamOverrideWithRelayInfo(chatJSON, info)
		if err != nil {
			return nil, err
		}
	}

	var overriddenChatReq dto.GeneralOpenAIRequest
	if err = common.Unmarshal(chatJSON, &overriddenChatReq); err != nil {
		return nil, err
	}

	responsesReq, err := service.ChatCompletionsRequestToResponsesRequest(&overriddenChatReq)
	if err != nil {
		return nil, err
	}
	info.AppendRequestConversion(types.RelayFormatOpenAIResponses)
	info.RelayMode = relayconstant.RelayModeResponses
	info.RequestURLPath = "/v1/responses"

	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *responsesReq)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
	return common.Marshal(convertedRequest)
}

func buildResponsesPreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		responsesReq = &dto.OpenAIResponsesRequest{
			Model:              req.Model,
			Input:              req.Input,
			Instructions:       req.Instructions,
			PreviousResponseID: req.PreviousResponseID,
		}
	default:
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildEmbeddingPreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	embeddingReq, ok := info.Request.(*dto.EmbeddingRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(embeddingReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	convertedRequest, err := adaptor.ConvertEmbeddingRequest(c, info, *request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildImagePreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return nil, err
	}

	// store original request body JSON for REST route matching (preview)
	if rawBody, err := common.Marshal(request); err == nil {
		info.RequestBodyJson = string(rawBody)
	}

	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	if _, ok = convertedRequest.(*bytes.Buffer); ok {
		return nil, errors.New("multipart/form image request preview is not supported yet")
	}

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildRerankPreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	rerankReq, ok := info.Request.(*dto.RerankRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(rerankReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	convertedRequest, err := adaptor.ConvertRerankRequest(c, info.RelayMode, *request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildClaudePreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	claudeReq, ok := info.Request.(*dto.ClaudeRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(claudeReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	if request.MaxTokens == nil || *request.MaxTokens == 0 {
		defaultMaxTokens := uint(model_setting.GetClaudeSettings().GetDefaultMaxTokens(request.Model))
		request.MaxTokens = &defaultMaxTokens
	}

	if baseModel, effortLevel, ok := reasoning.TrimEffortSuffix(request.Model); ok && effortLevel != "" &&
		(strings.HasPrefix(request.Model, "claude-opus-4-6") || strings.HasPrefix(request.Model, "claude-opus-4-7")) {
		request.Model = baseModel
		request.Thinking = &dto.Thinking{
			Type: "adaptive",
		}
		request.OutputConfig = json.RawMessage(fmt.Sprintf(`{"effort":"%s"}`, effortLevel))
		if strings.HasPrefix(request.Model, "claude-opus-4-7") {
			request.Thinking.Display = "summarized"
			request.Temperature = nil
			request.TopP = nil
			request.TopK = nil
		} else {
			request.Temperature = common.GetPointer[float64](1.0)
		}
		info.UpstreamModelName = request.Model
	} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled &&
		strings.HasSuffix(request.Model, "-thinking") {
		if request.Thinking == nil {
			baseModel := strings.TrimSuffix(request.Model, "-thinking")
			if strings.HasPrefix(baseModel, "claude-opus-4-7") {
				request.Thinking = &dto.Thinking{Type: "adaptive", Display: "summarized"}
				request.OutputConfig = json.RawMessage(`{"effort":"high"}`)
				request.Temperature = nil
				request.TopP = nil
				request.TopK = nil
			} else {
				if request.MaxTokens == nil || *request.MaxTokens < 1280 {
					request.MaxTokens = common.GetPointer[uint](1280)
				}
				request.Thinking = &dto.Thinking{
					Type:         "enabled",
					BudgetTokens: common.GetPointer[int](int(float64(*request.MaxTokens) * model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage)),
				}
				request.Temperature = common.GetPointer[float64](1.0)
			}
		}
		if !model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
			request.Model = strings.TrimSuffix(request.Model, "-thinking")
		}
		info.UpstreamModelName = request.Model
	}

	if info.ChannelSetting.SystemPrompt != "" {
		if request.System == nil {
			request.SetStringSystem(info.ChannelSetting.SystemPrompt)
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			if request.IsStringSystem() {
				existing := strings.TrimSpace(request.GetStringSystem())
				if existing == "" {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt)
				} else {
					request.SetStringSystem(info.ChannelSetting.SystemPrompt + "\n" + existing)
				}
			} else {
				systemContents := request.ParseSystem()
				newSystem := dto.ClaudeMediaMessage{Type: dto.ContentTypeText}
				newSystem.SetText(info.ChannelSetting.SystemPrompt)
				if len(systemContents) == 0 {
					request.System = []dto.ClaudeMediaMessage{newSystem}
				} else {
					request.System = append([]dto.ClaudeMediaMessage{newSystem}, systemContents...)
				}
			}
		}
	}

	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	if service.ShouldChatCompletionsUseResponsesGlobal(info.ChannelId, info.ChannelType, info.OriginModelName) {
		openAIRequest, err := service.ClaudeToOpenAIRequest(*request, info)
		if err != nil {
			return nil, err
		}
		return buildResponsesPreviewBodyFromChat(c, info, adaptor, openAIRequest)
	}

	convertedRequest, err := adaptor.ConvertClaudeRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}

func buildGeminiPreviewBody(c *gin.Context, info *relaycommon.RelayInfo, adaptor relaychannel.Adaptor) ([]byte, error) {
	geminiReq, ok := info.Request.(*dto.GeminiChatRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type for preview: %T", info.Request)
	}

	request, err := common.DeepCopy(geminiReq)
	if err != nil {
		return nil, err
	}
	if err = helper.ModelMappedHelper(c, info, request); err != nil {
		return nil, err
	}

	if model_setting.GetGeminiSettings().ThinkingAdapterEnabled {
		if isNoThinkingRequest(request) && !strings.Contains(info.OriginModelName, "-nothinking") {
			noThinkingModelName := info.OriginModelName + "-nothinking"
			if helper.HasModelBillingConfig(noThinkingModelName) {
				info.OriginModelName = noThinkingModelName
				info.UpstreamModelName = noThinkingModelName
			}
		}
		if request.GenerationConfig.ThinkingConfig == nil {
			relaygemini.ThinkingAdaptor(request, info)
		}
	}

	if info.ChannelSetting.SystemPrompt != "" {
		if request.SystemInstructions == nil {
			request.SystemInstructions = &dto.GeminiChatContent{
				Parts: []dto.GeminiPart{{Text: info.ChannelSetting.SystemPrompt}},
			}
		} else if len(request.SystemInstructions.Parts) == 0 {
			request.SystemInstructions.Parts = []dto.GeminiPart{{Text: info.ChannelSetting.SystemPrompt}}
		} else if info.ChannelSetting.SystemPromptOverride {
			common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
			merged := false
			for i := range request.SystemInstructions.Parts {
				if request.SystemInstructions.Parts[i].Text == "" {
					continue
				}
				request.SystemInstructions.Parts[i].Text = info.ChannelSetting.SystemPrompt + "\n" + request.SystemInstructions.Parts[i].Text
				merged = true
				break
			}
			if !merged {
				request.SystemInstructions.Parts = append([]dto.GeminiPart{{Text: info.ChannelSetting.SystemPrompt}}, request.SystemInstructions.Parts...)
			}
		}
	}

	if request.SystemInstructions != nil {
		hasContent := false
		for _, part := range request.SystemInstructions.Parts {
			if part.Text != "" {
				hasContent = true
				break
			}
		}
		if !hasContent {
			request.SystemInstructions = nil
		}
	}

	if isPassThroughPreviewEnabled(info) {
		return buildPassThroughPreviewBody(c)
	}

	convertedRequest, err := adaptor.ConvertGeminiRequest(c, info, request)
	if err != nil {
		return nil, err
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return nil, err
	}
	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return nil, err
		}
	}
	return jsonData, nil
}
