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

func buildCOSObjectKey(setting *system_setting.COSSetting, contentType string, data []byte) string {
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	ext := extensionByMimeType(contentType)
	now := time.Now()
	prefix := strings.Trim(strings.TrimSpace(setting.PathPrefix), "/")
	if prefix == "" {
		prefix = "images"
	}
	return path.Join(
		prefix,
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
		hash+ext,
	)
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
	return buildCOSPublicURL(setting, objectKey), nil
}
