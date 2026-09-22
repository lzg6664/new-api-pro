package controller

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeImageTaskData(t *testing.T) {
	t.Run("array shape from background poller", func(t *testing.T) {
		raw := json.RawMessage(`[{"url":"https://example.com/a.png","type":"image"},{"b64_json":"ZmFrZQ=="}]`)
		items := normalizeImageTaskData(raw)
		require.Len(t, items, 2)
		require.Equal(t, "https://example.com/a.png", items[0].Url)
		require.Equal(t, "ZmFrZQ==", items[1].B64Json)
	})

	t.Run("array shape with alias keys", func(t *testing.T) {
		raw := json.RawMessage(`[{"image_url":"https://example.com/b.png"},{"base64":"ZmFrZQ=="}]`)
		items := normalizeImageTaskData(raw)
		require.Len(t, items, 2)
		require.Equal(t, "https://example.com/b.png", items[0].Url)
		require.Equal(t, "ZmFrZQ==", items[1].B64Json)
	})

	t.Run("object shape from sync/wrapper path", func(t *testing.T) {
		raw := json.RawMessage(`{"created":123,"data":[{"url":"https://cdn.example.com/c.png","b64_json":""}]}`)
		items := normalizeImageTaskData(raw)
		require.Len(t, items, 1)
		require.Equal(t, "https://cdn.example.com/c.png", items[0].Url)
	})

	t.Run("submit marker object yields nil", func(t *testing.T) {
		raw := json.RawMessage(`{"upstream_task_id":"upstream_123"}`)
		require.Nil(t, normalizeImageTaskData(raw))
	})

	t.Run("null and empty yield nil", func(t *testing.T) {
		require.Nil(t, normalizeImageTaskData(json.RawMessage(`null`)))
		require.Nil(t, normalizeImageTaskData(nil))
		require.Nil(t, normalizeImageTaskData(json.RawMessage(`  `)))
	})
}
