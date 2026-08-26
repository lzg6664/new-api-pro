package imagepipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	appcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/tencentyun/cos-go-sdk-v5"
)

func validateCOSSetting(setting *system_setting.COSSetting) error {
	if setting == nil || !setting.Enabled {
		return fmt.Errorf("cos is not enabled")
	}
	if strings.TrimSpace(setting.SecretID) == "" ||
		strings.TrimSpace(setting.SecretKey) == "" ||
		strings.TrimSpace(setting.Region) == "" ||
		strings.TrimSpace(setting.Bucket) == "" {
		return fmt.Errorf("cos configuration is incomplete")
	}
	return nil
}

func newCOSClient(setting *system_setting.COSSetting) (*cos.Client, error) {
	if err := validateCOSSetting(setting); err != nil {
		return nil, err
	}

	bucketURL, err := url.Parse(fmt.Sprintf("https://%s.cos.%s.myqcloud.com", strings.TrimSpace(setting.Bucket), strings.TrimSpace(setting.Region)))
	if err != nil {
		return nil, err
	}

	timeoutSeconds := setting.ReadTimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 120
	}

	baseURL := &cos.BaseURL{BucketURL: bucketURL}
	client := cos.NewClient(baseURL, &http.Client{
		Timeout: time.Duration(timeoutSeconds) * time.Second,
		Transport: &cos.AuthorizationTransport{
			SecretID:  strings.TrimSpace(setting.SecretID),
			SecretKey: strings.TrimSpace(setting.SecretKey),
		},
	})
	return client, nil
}

// buildCOSObjectKey 生成内容寻址对象键：{path_prefix}/{sha256}{ext}（不含日期层级）。
func buildCOSObjectKey(setting *system_setting.COSSetting, contentType string, data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := extensionByMimeType(contentType)
	prefix := strings.Trim(strings.TrimSpace(setting.PathPrefix), "/")
	if prefix == "" {
		prefix = "images"
	}
	return path.Join(prefix, hash+ext)
}

func extensionByMimeType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	case "image/bmp":
		return ".bmp"
	case "image/svg+xml":
		return ".svg"
	case "image/heic":
		return ".heic"
	case "image/heif":
		return ".heif"
	default:
		return ".bin"
	}
}

func buildCOSPublicURL(setting *system_setting.COSSetting, objectKey string) string {
	baseURL := strings.TrimSpace(setting.PublicBaseURL)
	if baseURL == "" {
		baseURL = fmt.Sprintf("https://%s.cos.%s.myqcloud.com", strings.TrimSpace(setting.Bucket), strings.TrimSpace(setting.Region))
	}
	return appcommon.BuildURL(strings.TrimRight(baseURL, "/")+"/", strings.TrimLeft(objectKey, "/"))
}

// cdnSignWindowSeconds 对齐 snapstory 后端 CosStorageService.SIGN_WINDOW_SECONDS（约 20 年）。
// 时间戳按窗口取整 → 同一对象的签名 URL 恒定，浏览器可持续命中磁盘缓存，
// 且与 Java 侧 generateDisplayUrl 重签结果逐字节一致。
const cdnSignWindowSeconds int64 = 630720000

// buildCDNSignedURL 生成完整 CDN URL（PublicBaseURL 须已配置）：
// sign = hex(sha256(cdnAuthKey + "/" + objectKey + ts))，ts = now 按窗口取整，
// 复刻 Java CosStorageService.signCdnUrl 的 TypeD+SHA256 签名。
// cdn_auth_key 为空时返回未签名公网 URL。
func buildCDNSignedURL(setting *system_setting.COSSetting, objectKey string, nowUnix int64) string {
	base := buildCOSPublicURL(setting, objectKey)
	if strings.TrimSpace(setting.CDNAuthKey) == "" {
		return base
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	timestamp := strconv.FormatInt(nowUnix-nowUnix%cdnSignWindowSeconds, 10)
	sum := sha256.Sum256([]byte(setting.CDNAuthKey + parsed.Path + timestamp))
	return base + "?sign=" + hex.EncodeToString(sum[:]) + "&t=" + timestamp
}

func UploadBytesToCOS(ctx context.Context, data []byte, contentType string, objectKey string) (string, error) {
	setting := system_setting.GetCOSSetting()
	if err := validateCOSSetting(setting); err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("upload data is empty")
	}
	maxUploadMB := setting.MaxUploadMB
	if maxUploadMB <= 0 {
		maxUploadMB = 20
	}
	if len(data) > maxUploadMB*1024*1024 {
		return "", fmt.Errorf("upload data exceeds max size: %dMB", maxUploadMB)
	}

	client, err := newCOSClient(setting)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(objectKey) == "" {
		objectKey = buildCOSObjectKey(setting, contentType, data)
	}

	_, err = client.Object.Put(
		ctx,
		objectKey,
		bytes.NewReader(data),
		&cos.ObjectPutOptions{
			ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
				ContentType: contentType,
			},
		},
	)
	if err != nil {
		return "", err
	}

	// 配置了 public_base_url（如 CDN 域名）时，返回完整 CDN URL（cdn_auth_key 非空则带 TypeD 签名），
	// 调用方可直接使用；未配置时回退桶域名预签名 URL。
	if strings.TrimSpace(setting.PublicBaseURL) != "" {
		return buildCDNSignedURL(setting, objectKey, time.Now().Unix()), nil
	}

	// Generate a presigned URL for download (bucket may have private read access).
	// The URL expires after the configured duration.
	presignedURL, err := client.Object.GetPresignedURL(ctx, http.MethodGet, objectKey,
		setting.SecretID, setting.SecretKey, time.Duration(setting.PresignedExpireSeconds)*time.Second, nil)
	if err != nil {
		// Fallback to plain URL if presigning fails (e.g. bucket is public-read).
		return buildCOSPublicURL(setting, objectKey), nil
	}
	return presignedURL.String(), nil
}

func PreviewBytesToCOS(data []byte, contentType string, objectKey string) (string, error) {
	setting := system_setting.GetCOSSetting()
	if err := validateCOSSetting(setting); err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("upload data is empty")
	}
	if strings.TrimSpace(objectKey) == "" {
		objectKey = buildCOSObjectKey(setting, contentType, data)
	}
	return buildCDNSignedURL(setting, objectKey, time.Now().Unix()), nil
}
