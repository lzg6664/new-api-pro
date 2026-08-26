package system_setting

import "github.com/QuantumNous/new-api/setting/config"

const (
	ImageAutoStoreModeOff       = "off"
	ImageAutoStoreModeB64Only   = "b64_only"
	ImageAutoStoreModeURLAndB64 = "url_and_b64"
)

type COSSetting struct {
	Enabled       bool   `json:"enabled"`
	SecretID      string `json:"secret_id"`
	SecretKey     string `json:"secret_key"`
	Region        string `json:"region"`
	Bucket        string `json:"bucket"`
	PublicBaseURL string `json:"public_base_url"`
	// CDNAuthKey 为 PublicBaseURL 启用 TypeD+SHA256 签名（与 snapstory 后端 CosStorageService.signCdnUrl 算法一致），
	// 空则返回未签名公网 URL
	CDNAuthKey             string `json:"cdn_auth_key"`
	PathPrefix             string `json:"path_prefix"`
	ReadTimeoutSeconds     int    `json:"read_timeout_seconds"`
	MaxUploadMB            int    `json:"max_upload_mb"`
	ImageAutoStoreMode     string `json:"image_auto_store_mode"`
	ImageAutoStoreStrict   bool   `json:"image_auto_store_strict"`
	PresignedExpireSeconds int    `json:"presigned_expire_seconds"`
}

var defaultCOSSetting = COSSetting{
	Enabled:                false,
	Region:                 "ap-beijing",
	Bucket:                 "ldz-1304506381",
	PathPrefix:             "static",
	ReadTimeoutSeconds:     120,
	MaxUploadMB:            20,
	ImageAutoStoreMode:     ImageAutoStoreModeB64Only,
	ImageAutoStoreStrict:   false,
	PresignedExpireSeconds: 3153600000, // 100年 ≈ 永不过期
}

func init() {
	config.GlobalConfig.Register("cos_setting", &defaultCOSSetting)
}

func GetCOSSetting() *COSSetting {
	return &defaultCOSSetting
}
