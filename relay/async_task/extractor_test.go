package async_task

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func TestReplaceByPath_ReplacesNestedField(t *testing.T) {
	body := []byte(`{"data":{"task_id":"upstream_123","status":"RUNNING"}}`)

	updated := replaceByPath(body, "data.task_id", "task_local_123")

	require.JSONEq(t, `{"data":{"task_id":"task_local_123","status":"RUNNING"}}`, string(updated))
}

func TestReplaceByPath_LeavesBodyUnchangedWhenPathMissing(t *testing.T) {
	body := []byte(`{"data":{"task_id":"upstream_123"}}`)

	updated := replaceByPath(body, "data.id", "task_local_123")

	require.JSONEq(t, string(body), string(updated))
}

func TestExtractByPath_AfterReplaceByPath(t *testing.T) {
	body := []byte(`{"results":[{"url":"https://example.com/a.png"}],"taskId":"upstream_123"}`)

	updated := replaceByPath(body, "taskId", "task_local_123")

	require.Equal(t, "task_local_123", extractByPath(updated, "taskId"))
	require.Equal(t, "https://example.com/a.png", extractByPath(updated, "results[0].url"))

	var out map[string]any
	err := common.Unmarshal(updated, &out)
	require.NoError(t, err)
}

func TestTryDetectAsyncTaskDetectsToapisGenerationTask(t *testing.T) {
	config := &dto.ChannelAsyncTaskConfig{
		Enabled:         true,
		TaskIDPath:      "taskId",
		StatusPath:      "status",
		SuccessStatuses: []string{"PENDING", "QUEUED", "RUNNING"},
	}
	body := []byte(`{"id":"tsk_img_01KPT","object":"generation.task","model":"gpt-image-2","status":"pending","progress":0}`)

	taskID, rawBody := TryDetectAsyncTask(body, config)

	require.Equal(t, "tsk_img_01KPT", taskID)
	require.Equal(t, body, rawBody)
}

func TestExtractImageDataFallsBackToResultData(t *testing.T) {
	config := &dto.ChannelAsyncTaskConfig{
		ResultListPath: "results",
		ResultURLPath:  "url",
	}
	body := []byte(`{"status":"completed","result":{"data":[{"url":"https://example.com/final.png"}]}}`)

	imageData := extractImageData(body, config)

	require.Len(t, imageData, 1)
	require.Equal(t, "https://example.com/final.png", imageData[0].Url)
}
