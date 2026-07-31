package common

import (
	"fmt"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

// LogRelayOwnParams 记录 new-api-pro 自身解析出的中转参数：
// 渠道（id/类型/名称/base_url）、模型映射（原始→上游）、密钥（脱敏）、参数覆盖。
// 在 ModelMappedHelper 中以 defer 调用，确保各 return 路径都能读到最终解析状态。
func LogRelayOwnParams(c *gin.Context, info *RelayInfo) {
	if info == nil || info.ChannelMeta == nil {
		return
	}
	cm := info.ChannelMeta
	channelName := c.GetString("channel_name")

	override := "<nil>"
	if cm.ParamOverride != nil {
		if b, err := rootcommon.Marshal(cm.ParamOverride); err == nil {
			override = rootcommon.RedactAndTruncateBody(b, "application/json")
		}
	}

	logger.LogInfo(c, fmt.Sprintf(
		"[RELAY-OWN] channel=#%d type=%d name=%q base_url=%q | model %q->%q (mapped=%t) | api_key=%s | param_override=%s",
		cm.ChannelId, cm.ChannelType, channelName, cm.ChannelBaseUrl,
		info.OriginModelName, cm.UpstreamModelName, cm.IsModelMapped,
		rootcommon.MaskSecret(cm.ApiKey),
		override,
	))
}
