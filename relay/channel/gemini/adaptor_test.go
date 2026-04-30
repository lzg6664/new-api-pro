package gemini

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func TestConvertImageRequestForGeminiChatImageModelMapsOpenAIFields(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "gemini-3-pro-image-preview",
		},
	}
	request := dto.ImageRequest{
		Model:   "gemini-3-pro-image-preview",
		Prompt:  "给我一个香蕉哦图片",
		Size:    "1024x1792",
		Quality: "hd",
		N:       uintPtr(2),
	}

	got, err := adaptor.ConvertImageRequest(gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New()), info, request)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	geminiRequest, ok := got.(dto.GeminiChatRequest)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want dto.GeminiChatRequest", got)
	}

	if len(geminiRequest.GenerationConfig.ResponseModalities) != 1 || geminiRequest.GenerationConfig.ResponseModalities[0] != "IMAGE" {
		t.Fatalf("responseModalities = %#v, want [\"IMAGE\"]", geminiRequest.GenerationConfig.ResponseModalities)
	}
	if geminiRequest.GenerationConfig.CandidateCount == nil || *geminiRequest.GenerationConfig.CandidateCount != 2 {
		t.Fatalf("candidateCount = %#v, want 2", geminiRequest.GenerationConfig.CandidateCount)
	}
	if len(geminiRequest.GenerationConfig.ImageConfig) == 0 {
		t.Fatalf("imageConfig is empty")
	}

	var imageConfig map[string]any
	if err := common.Unmarshal(geminiRequest.GenerationConfig.ImageConfig, &imageConfig); err != nil {
		t.Fatalf("failed to unmarshal imageConfig: %v", err)
	}

	if imageConfig["aspectRatio"] != "9:16" {
		t.Fatalf("aspectRatio = %#v, want %q", imageConfig["aspectRatio"], "9:16")
	}
	if imageConfig["imageSize"] != "2K" {
		t.Fatalf("imageSize = %#v, want %q", imageConfig["imageSize"], "2K")
	}
}

func TestConvertImageRequestForImagenModelMapsOpenAIFields(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "imagen-4.0-generate-001",
		},
	}
	request := dto.ImageRequest{
		Model:   "imagen-4.0-generate-001",
		Prompt:  "a red fox in snowfall",
		Size:    "1536x1024",
		Quality: "high",
		N:       uintPtr(3),
	}

	got, err := adaptor.ConvertImageRequest(gin.CreateTestContextOnly(httptest.NewRecorder(), gin.New()), info, request)
	if err != nil {
		t.Fatalf("ConvertImageRequest returned error: %v", err)
	}

	geminiRequest, ok := got.(dto.GeminiImageRequest)
	if !ok {
		t.Fatalf("ConvertImageRequest returned %T, want dto.GeminiImageRequest", got)
	}

	if geminiRequest.Parameters.SampleCount != 3 {
		t.Fatalf("sampleCount = %d, want 3", geminiRequest.Parameters.SampleCount)
	}
	if geminiRequest.Parameters.AspectRatio != "3:2" {
		t.Fatalf("aspectRatio = %q, want %q", geminiRequest.Parameters.AspectRatio, "3:2")
	}
	if geminiRequest.Parameters.ImageSize != "2K" {
		t.Fatalf("imageSize = %q, want %q", geminiRequest.Parameters.ImageSize, "2K")
	}
}

func uintPtr(v uint) *uint {
	return &v
}
