package middleware

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

// RelayTraceReceive 在 /v1 中转入口记录接收到的请求（脱敏）。
// 应挂在 httpRouter 上、Distribute() 之后（此时 BodyStorage 已被 prime）。
// BodyStorage.Bytes() 为非破坏性读取，不影响后续 UnmarshalBodyReusable 等消费方。
func RelayTraceReceive() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !common.DebugEnabled {
			// 全量接收日志含 body 脱敏解析（可达 MB 级），仅 DEBUG 开启时记录
			c.Next()
			return
		}
		method := c.Request.Method
		path := c.Request.URL.Path
		ct := c.GetHeader("Content-Type")

		var bodyStr string
		if storage, err := common.GetBodyStorage(c); err == nil {
			if b, bErr := storage.Bytes(); bErr == nil {
				bodyStr = common.RedactAndTruncateBody(b, ct)
			} else {
				bodyStr = fmt.Sprintf("<body read error: %v>", bErr)
			}
		} else {
			bodyStr = fmt.Sprintf("<body unavailable: %v>", err)
		}

		logger.LogInfo(c, fmt.Sprintf(
			"[RELAY-RECEIVE] %s %s | ct=%s | auth=%s | body=%s",
			method, path, ct, common.MaskSecret(c.GetHeader("Authorization")), bodyStr,
		))

		c.Next()
	}
}
