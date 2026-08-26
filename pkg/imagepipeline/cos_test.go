package imagepipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
)

// 期望向量与 snapstory 后端 CosStorageService.signCdnUrl（TypeD+SHA256，窗口取整时间戳）同式计算：
// sign = hex(sha256(cdnAuthKey + "/" + objectKey + ts))，ts = now - now % 630720000。
func TestBuildCDNSignedURL(t *testing.T) {
	setting := &system_setting.COSSetting{
		Bucket:        "ldz-1304506381",
		Region:        "ap-beijing",
		PublicBaseURL: "https://cdn.xiangzhifenjing.com",
		CDNAuthKey:    "9sfl3cPoDz0OYp34",
	}

	// now=1700000000 → ts=1261440000（1700000000 - 1700000000%630720000）
	got := buildCDNSignedURL(setting, "static/test.png", 1700000000)
	want := "https://cdn.xiangzhifenjing.com/static/test.png" +
		"?sign=fcbb092bd7225ef7d50df91ebf8f169e0c1032dbfb9bd0742b9d2b6a0d1ef765&t=1261440000"
	if got != want {
		t.Fatalf("buildCDNSignedURL mismatch:\n got=%s\nwant=%s", got, want)
	}

	// 时间戳按窗口取整：同窗口内任意时刻生成相同 URL（浏览器缓存友好、与 Java 重签一致）
	if again := buildCDNSignedURL(setting, "static/test.png", 1261440000); again != want {
		t.Fatalf("same window should produce identical URL:\n got=%s\nwant=%s", again, want)
	}

	// cdn_auth_key 为空 → 未签名公网 URL
	setting.CDNAuthKey = ""
	if got := buildCDNSignedURL(setting, "static/test.png", 1700000000); got != "https://cdn.xiangzhifenjing.com/static/test.png" {
		t.Fatalf("empty cdn_auth_key should return unsigned public URL, got=%s", got)
	}
}

// 独立复核签名公式本身（不依赖固定向量，防止手算错误掩盖实现错误）
func TestCDNSignFormula(t *testing.T) {
	now := int64(1700000000)
	ts := now - now%cdnSignWindowSeconds
	if ts != 1261440000 {
		t.Fatalf("window floor mismatch: %d", ts)
	}
	sum := sha256.Sum256([]byte("9sfl3cPoDz0OYp34" + "/static/test.png" + "1261440000"))
	if hex.EncodeToString(sum[:]) != "fcbb092bd7225ef7d50df91ebf8f169e0c1032dbfb9bd0742b9d2b6a0d1ef765" {
		t.Fatalf("formula mismatch: %s", hex.EncodeToString(sum[:]))
	}
}
