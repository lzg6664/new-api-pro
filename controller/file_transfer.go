package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/imagepipeline"

	"github.com/gin-gonic/gin"
)

func TransferFile(c *gin.Context) {
	req := dto.FileTransferRequest{}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		common.ApiErrorMsg(c, "url is required")
		return
	}

	output := strings.ToLower(strings.TrimSpace(req.Output))
	if output == "" {
		output = "url"
	}

	result, err := imagepipeline.TransformImageValue(c.Request.Context(), req.URL, imagepipeline.ImagePipelineOptions{
		Input:    "url",
		Output:   output,
		Storage:  "cos",
		MimeType: strings.TrimSpace(req.MimeType),
		Quality:  req.Quality,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}

	resp := dto.FileTransferResponse{
		Value:        result.Value,
		MimeType:     result.MimeType,
		OriginalSize: result.OriginalSize,
		StoredSize:   result.StoredSize,
	}
	if result.OutputFormat == "url" {
		resp.URL = result.Value
	}

	common.ApiSuccess(c, resp)
}
