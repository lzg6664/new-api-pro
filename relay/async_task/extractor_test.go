package async_task

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
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
