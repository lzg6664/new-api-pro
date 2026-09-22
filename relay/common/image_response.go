package common

import (
	"context"
	"net/http"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/imagepipeline"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func WriteImageResponse(c *gin.Context, info *RelayInfo, statusCode int, response *dto.ImageResponse) *types.NewAPIError {
	// 捕获模式（async=1 提交的后台协程）：只做自动转存并暂存结果，不写客户端。
	// 此时 gin context 可能是 c.Copy() 副本（Writer 底层为 nil，绝不可写），
	// 且原始客户端连接已返回回执，故转存必须用独立 context。
	if info != nil && info.ImageCapture != nil {
		processed, err := imagepipeline.ProcessImageResponseAutoStore(context.Background(), response)
		if err != nil {
			return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
		}
		if processed != nil {
			*info.ImageCapture = *processed
		}
		return nil
	}
	requestContext := context.Background()
	if c != nil && c.Request != nil {
		requestContext = c.Request.Context()
	}
	processed, err := imagepipeline.ProcessImageResponseAutoStore(requestContext, response)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	jsonResponse, err := appcommon.Marshal(processed)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(statusCode)
	if _, err = c.Writer.Write(jsonResponse); err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	return nil
}
