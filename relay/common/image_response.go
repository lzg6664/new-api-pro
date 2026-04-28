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
