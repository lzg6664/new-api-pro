package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestShouldRetryDefaultsToRetryingNon2xxUpstreamStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &gin.Context{}

	require.True(t, shouldRetry(c, upstreamStatusError(http.StatusBadRequest), 1))
	require.True(t, shouldRetry(c, upstreamStatusError(http.StatusRequestTimeout), 1))
	require.False(t, shouldRetry(c, upstreamStatusError(http.StatusOK), 1))
	require.False(t, shouldRetry(c, upstreamStatusError(http.StatusGatewayTimeout), 1))
	require.False(t, shouldRetry(c, upstreamStatusError(524), 1))
}

func TestShouldRetryTaskRelayDefaultsToRetryingNonLocalNon2xxStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c := &gin.Context{}

	require.True(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusBadRequest, false), 1))
	require.True(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusRequestTimeout, false), 1))
	require.False(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusBadRequest, true), 1))
	require.False(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusOK, false), 1))
	require.False(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusGatewayTimeout, false), 1))
	require.False(t, shouldRetryTaskRelay(c, 1, taskStatusError(524, false), 1))

	c.Set("specific_channel_id", "1")
	require.False(t, shouldRetryTaskRelay(c, 1, taskStatusError(http.StatusBadRequest, false), 1))
}

func upstreamStatusError(statusCode int) *types.NewAPIError {
	return types.NewOpenAIError(errors.New("upstream error"), types.ErrorCodeBadResponseStatusCode, statusCode)
}

func taskStatusError(statusCode int, local bool) *dto.TaskError {
	return &dto.TaskError{
		Code:       "upstream_error",
		Message:    "upstream error",
		StatusCode: statusCode,
		LocalError: local,
		Error:      errors.New("upstream error"),
	}
}
