package gemini

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

// isImageGenerationModel checks whether the model should be treated as an image
// generation model for this channel. It returns true if the model name starts
// with "imagen" or matches any of the configured ImageGenerationModelPrefixes.
func isImageGenerationModel(info *relaycommon.RelayInfo) bool {
	if strings.HasPrefix(info.UpstreamModelName, "imagen") {
		return true
	}
	for _, prefix := range info.ChannelSetting.ImageGenerationModelPrefixes {
		if prefix != "" && strings.HasPrefix(info.UpstreamModelName, prefix) {
			return true
		}
	}
	return false
}

type Adaptor struct {
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	if len(request.Contents) > 0 {
		for i, content := range request.Contents {
			if i == 0 {
				if request.Contents[0].Role == "" {
					request.Contents[0].Role = "user"
				}
			}
			for _, part := range content.Parts {
				if part.FileData != nil {
					if part.FileData.MimeType == "" && strings.Contains(part.FileData.FileUri, "www.youtube.com") {
						part.FileData.MimeType = "video/webm"
					}
				}
			}
		}
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	adaptor := openai.Adaptor{}
	oaiReq, err := adaptor.ConvertClaudeRequest(c, info, req)
	if err != nil {
		return nil, err
	}
	return a.ConvertOpenAIRequest(c, info, oaiReq.(*dto.GeneralOpenAIRequest))
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	//TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	// For Imagen models: use the old predict/instances format
	if strings.HasPrefix(info.UpstreamModelName, "imagen") {
		// convert size to aspect ratio but allow user to specify aspect ratio
		aspectRatio := "1:1" // default aspect ratio
		size := strings.TrimSpace(request.Size)
		if size != "" {
			if strings.Contains(size, ":") {
				aspectRatio = size
			} else {
				switch size {
				case "256x256", "512x512", "1024x1024":
					aspectRatio = "1:1"
				case "1536x1024":
					aspectRatio = "3:2"
				case "1024x1536":
					aspectRatio = "2:3"
				case "1024x1792":
					aspectRatio = "9:16"
				case "1792x1024":
					aspectRatio = "16:9"
				}
			}
		}

		// build gemini imagen request
		geminiRequest := dto.GeminiImageRequest{
			Instances: []dto.GeminiImageInstance{
				{
					Prompt: request.Prompt,
				},
			},
			Parameters: dto.GeminiImageParameters{
				SampleCount:      int(lo.FromPtrOr(request.N, uint(1))),
				AspectRatio:      aspectRatio,
				PersonGeneration: "allow_adult", // default allow adult
			},
		}

		// Set imageSize when quality parameter is specified
		if request.Quality != "" {
			imageSize := "1K" // default
			switch request.Quality {
			case "hd", "high":
				imageSize = "2K"
			case "2K":
				imageSize = "2K"
			case "standard", "medium", "low", "auto", "1K":
				imageSize = "1K"
			default:
				imageSize = "1K"
			}
			geminiRequest.Parameters.ImageSize = imageSize
		}

		return geminiRequest, nil
	}

	// For non-Imagen image generation models (e.g. gemini-*-image-* prefix),
	// the upstream expects Gemini chat format with responseModalities: ["IMAGE"].
	parts := []dto.GeminiPart{
		{Text: request.Prompt},
	}

	// If request.Image is provided (URL for image-to-image), download it and
	// add as inlineData to the Gemini chat request.
	if request.Image != nil {
		var imageURLs []string
		// Try to parse as array first, then as single string
		if err := common.Unmarshal(request.Image, &imageURLs); err != nil || len(imageURLs) == 0 {
			// Single URL string
			var singleURL string
			if err := common.Unmarshal(request.Image, &singleURL); err == nil && singleURL != "" {
				imageURLs = []string{singleURL}
			}
		}

		for _, imgURL := range imageURLs {
			if imgURL == "" {
				continue
			}
			mimeType, data, err := service.GetImageFromUrl(imgURL)
			if err != nil {
				return nil, fmt.Errorf("failed to download image from %s: %w", imgURL, err)
			}
			parts = append(parts, dto.GeminiPart{
				InlineData: &dto.GeminiInlineData{
					MimeType: mimeType,
					Data:     data,
				},
			})
		}
	}

	geminiRequest := dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		GenerationConfig: dto.GeminiChatGenerationConfig{
			ResponseModalities: []string{"IMAGE"},
		},
	}

	return geminiRequest, nil
}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {

}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {

	if model_setting.GetGeminiSettings().ThinkingAdapterEnabled &&
		!model_setting.ShouldPreserveThinkingSuffix(info.OriginModelName) {
		// 新增逻辑：处理 -thinking-<budget> 格式
		if strings.Contains(info.UpstreamModelName, "-thinking-") {
			parts := strings.Split(info.UpstreamModelName, "-thinking-")
			info.UpstreamModelName = parts[0]
		} else if strings.HasSuffix(info.UpstreamModelName, "-thinking") { // 旧的适配
			info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-thinking")
		} else if strings.HasSuffix(info.UpstreamModelName, "-nothinking") {
			info.UpstreamModelName = strings.TrimSuffix(info.UpstreamModelName, "-nothinking")
		} else if baseModel, level, ok := reasoning.TrimEffortSuffix(info.UpstreamModelName); ok && level != "" {
			info.UpstreamModelName = baseModel
		}
	}

	if isImageGenerationModel(info) {
		version := model_setting.GetGeminiVersionSetting(info.UpstreamModelName)
		if strings.HasPrefix(info.UpstreamModelName, "imagen") {
			return fmt.Sprintf("%s/%s/models/%s:predict", info.ChannelBaseUrl, version, info.UpstreamModelName), nil
		}
		// Non-Imagen image generation models use the chat generateContent endpoint
		return fmt.Sprintf("%s/%s/models/%s:generateContent", info.ChannelBaseUrl, version, info.UpstreamModelName), nil
	}

	version := model_setting.GetGeminiVersionSetting(info.UpstreamModelName)

	if strings.HasPrefix(info.UpstreamModelName, "text-embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "gemini-embedding") {
		action := "embedContent"
		if info.IsGeminiBatchEmbedding {
			action = "batchEmbedContents"
		}
		return fmt.Sprintf("%s/%s/models/%s:%s", info.ChannelBaseUrl, version, info.UpstreamModelName, action), nil
	}

	action := "generateContent"
	if info.IsStream {
		action = "streamGenerateContent?alt=sse"
		if info.RelayMode == constant.RelayModeGemini {
			info.DisablePing = true
		}
	}
	return fmt.Sprintf("%s/%s/models/%s:%s", info.ChannelBaseUrl, version, info.UpstreamModelName, action), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("x-goog-api-key", info.ApiKey)
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}

	geminiRequest, err := CovertOpenAI2Gemini(c, *request, info)
	if err != nil {
		return nil, err
	}

	return geminiRequest, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, nil
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	if request.Input == nil {
		return nil, errors.New("input is required")
	}

	inputs := request.ParseInput()
	if len(inputs) == 0 {
		return nil, errors.New("input is empty")
	}
	// We always build a batch-style payload with `requests`, so ensure we call the
	// batch endpoint upstream to avoid payload/endpoint mismatches.
	info.IsGeminiBatchEmbedding = true
	// process all inputs
	geminiRequests := make([]map[string]interface{}, 0, len(inputs))
	for _, input := range inputs {
		geminiRequest := map[string]interface{}{
			"model": fmt.Sprintf("models/%s", info.UpstreamModelName),
			"content": dto.GeminiChatContent{
				Parts: []dto.GeminiPart{
					{
						Text: input,
					},
				},
			},
		}

		// set specific parameters for different models
		// https://ai.google.dev/api/embeddings?hl=zh-cn#method:-models.embedcontent
		switch info.UpstreamModelName {
		case "text-embedding-004", "gemini-embedding-exp-03-07", "gemini-embedding-001":
			// Only newer models introduced after 2024 support OutputDimensionality
			dimensions := lo.FromPtrOr(request.Dimensions, 0)
			if dimensions > 0 {
				geminiRequest["outputDimensionality"] = dimensions
			}
		}
		geminiRequests = append(geminiRequests, geminiRequest)
	}

	return map[string]interface{}{
		"requests": geminiRequests,
	}, nil
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	// TODO implement me
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if info.RelayMode == constant.RelayModeGemini {
		if strings.Contains(info.RequestURLPath, ":embedContent") ||
			strings.Contains(info.RequestURLPath, ":batchEmbedContents") {
			return NativeGeminiEmbeddingHandler(c, resp, info)
		}
		if info.IsStream {
			return GeminiTextGenerationStreamHandler(c, info, resp)
		} else {
			return GeminiTextGenerationHandler(c, info, resp)
		}
	}

	// Image generation responses always need the image handler, regardless
	// of whether the model has been configured with image_generation_model_prefixes.
	// This is needed because the upstream returns Gemini chat format responses
	// with inlineData images, which must be converted to OpenAI format.
	if info.RelayMode == constant.RelayModeImagesGenerations || isImageGenerationModel(info) {
		if strings.HasPrefix(info.UpstreamModelName, "imagen") {
			return GeminiImageHandler(c, info, resp)
		}
		return GeminiChatImageHandler(c, info, resp)
	}

	// check if the model is an embedding model
	if strings.HasPrefix(info.UpstreamModelName, "text-embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "embedding") ||
		strings.HasPrefix(info.UpstreamModelName, "gemini-embedding") {
		return GeminiEmbeddingHandler(c, info, resp)
	}

	if info.IsStream {
		return GeminiChatStreamHandler(c, info, resp)
	} else {
		return GeminiChatHandler(c, info, resp)
	}

}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
