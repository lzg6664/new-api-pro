package common

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
)

// 中转请求/响应日志的脱敏与截断工具。
// 设计目标：保留 URL 与文本字段（prompt/model/size/headers 等），
// 剥离体积庞大的图片 base64 / data:URI 数据，并脱敏密钥。

const relayTraceBodyMax = 16 * 1024 // 单条日志 body 安全上限（仅对极端长文本兜底）

var (
	// base64Re 匹配纯 base64 字符串（含可选 padding）
	base64Re = regexp.MustCompile(`^[A-Za-z0-9+/]+={0,2}$`)
	// dataImageRe 匹配 data:image/...;base64,... 形式的图片 URI
	dataImageRe = regexp.MustCompile(`data:image/[A-Za-z0-9.+-]+;base64,[A-Za-z0-9+/=]+`)
	// b64WordRe 匹配候选 base64 串；是否为超长图片数据由回调按长度判定。
	// 注意：Go RE2 重复计数上限为 1000，不能在此使用 {4096,} 之类的大重复。
	b64WordRe = regexp.MustCompile(`[A-Za-z0-9+/]{2,}={0,2}`)
)

// longBase64Threshold 超过此长度的 base64 串视为内嵌图片/文件并剥离。
const longBase64Threshold = 1000

// imageFieldNames 是常见的图片字段名；命中后若值像 base64 且足够长则视为图片数据。
// URL 不会命中（包含 : . 等非 base64 字符），故图片 URL 得以保留。
var imageFieldNames = map[string]bool{
	"b64_json": true, "bimg": true, "mask": true,
	"image_b64": true, "image_b64_json": true,
	"input_image": true, "reference_image": true, "source_image": true,
	"init_image": true, "control_image": true, "thumbnail": true,
	"image": true, "data": true,
}

// MaskSecret 脱敏密钥：显示前缀 + 标注长度。
// 例："sk-8sAr8Tk..." -> "sk-8sA…(len=51)"；"Bearer xxx" -> "Bearer…(len=N)"。
func MaskSecret(s string) string {
	if s == "" {
		return "<empty>"
	}
	const prefixLen = 6
	if len(s) <= prefixLen {
		return fmt.Sprintf("…(len=%d)", len(s))
	}
	return fmt.Sprintf("%s…(len=%d)", s[:prefixLen], len(s))
}

func isSecretHeaderKey(key string) bool {
	k := strings.ToLower(key)
	for _, sub := range []string{"auth", "token", "secret", "password", "cookie", "api-key"} {
		if strings.Contains(k, sub) {
			return true
		}
	}
	return false
}

// RedactHeaders 返回小写 key -> 值的 map 副本；敏感 header 的值用 MaskSecret 脱敏。
func RedactHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		v := strings.Join(vs, ", ")
		if isSecretHeaderKey(k) {
			v = MaskSecret(v)
		}
		out[strings.ToLower(k)] = v
	}
	return out
}

func looksBase64(s string) bool {
	if len(s) < 16 {
		return false
	}
	return base64Re.MatchString(s)
}

// isImageBase64Value 判断字符串值是否为图片 base64 数据（保留 URL 与短文本）。
func isImageBase64Value(key, val string) bool {
	if strings.HasPrefix(val, "data:image/") {
		return true
	}
	if !looksBase64(val) {
		return false
	}
	if len(val) >= longBase64Threshold {
		return true
	}
	return len(val) > 512 && imageFieldNames[strings.ToLower(key)]
}

// redactWalk 递归遍历 JSON 结构，把图片 base64 字符串替换为占位。
func redactWalk(v any, key string) any {
	switch val := v.(type) {
	case map[string]any:
		for k, vv := range val {
			val[k] = redactWalk(vv, k)
		}
		return val
	case []any:
		for i := range val {
			val[i] = redactWalk(val[i], key)
		}
		return val
	case string:
		if isImageBase64Value(key, val) {
			return fmt.Sprintf("[image base64, %d bytes]", len(val))
		}
		return val
	default:
		return v
	}
}

// scrubRaw 用正则剥离 data:URI 与超长 base64（JSON 解析失败或非 JSON 时使用）。
func scrubRaw(s string) string {
	s = dataImageRe.ReplaceAllStringFunc(s, func(m string) string {
		return fmt.Sprintf("[image base64, %d bytes]", len(m))
	})
	s = b64WordRe.ReplaceAllStringFunc(s, func(m string) string {
		if len(m) >= longBase64Threshold {
			return fmt.Sprintf("[image base64, %d bytes]", len(m))
		}
		return m
	})
	return s
}

func capBody(s string) string {
	if len(s) <= relayTraceBodyMax {
		return s
	}
	return fmt.Sprintf("%s...[truncated, total=%d]", s[:relayTraceBodyMax], len(s))
}

// RedactAndTruncateBody 脱敏请求/响应体：保留 URL 与文本字段，剥离图片 base64。
// 仅用于日志输出，不修改原始 body。
func RedactAndTruncateBody(b []byte, contentType string) string {
	if len(b) == 0 {
		return "<empty>"
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))

	// multipart：含二进制文件，整体跳过
	if strings.HasPrefix(ct, "multipart/") {
		return fmt.Sprintf("<multipart, %d bytes, body omitted>", len(b))
	}

	// JSON：优先结构化遍历（精准保留字段名/URL，仅替换图片 base64）
	if strings.Contains(ct, "json") {
		var parsed any
		if err := Unmarshal(b, &parsed); err == nil {
			redacted := redactWalk(parsed, "")
			if out, err := Marshal(redacted); err == nil {
				return capBody(string(out))
			}
		}
		return capBody(scrubRaw(string(b)))
	}

	// 其它（text/*、表单等）：正则剥离后兜底截断
	return capBody(scrubRaw(string(b)))
}
