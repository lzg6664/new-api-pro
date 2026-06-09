package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestImageRequestPreservesGPTImage2EditFields(t *testing.T) {
	var request ImageRequest
	body := []byte(`{
		"model":"gpt-image-2",
		"prompt":"edit image",
		"image_urls":["https://example.com/a.png"],
		"mask_url":"https://example.com/mask.png",
		"resolution":"2k",
		"aspect_ratio":"16:9"
	}`)

	require.NoError(t, common.Unmarshal(body, &request))

	marshaled, err := common.Marshal(request)
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(marshaled))
}
