package dto

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
	// ImageGenerationModelPrefixes defines model name prefixes that should be
	// treated as image generation models (converted to Gemini/Imagen format).
	// This is needed for proxy channels where the upstream uses Gemini format
	// but the model names don't start with "imagen-".
	// Example: ["gemini-3.1-flash-image", "gemini-3-pro-image"]
	ImageGenerationModelPrefixes []string `json:"image_generation_model_prefixes,omitempty"`
}

type RestRouteRule struct {
	Name     string          `json:"name,omitempty"`
	Enabled  bool            `json:"enabled,omitempty"`
	Priority int             `json:"priority,omitempty"`
	Match    RestRouteMatch  `json:"match,omitempty"`
	Target   RestRouteTarget `json:"target,omitempty"`
}

type RestRouteMatch struct {
	RequestPath             string `json:"request_path,omitempty"`
	RequestPathPrefix       string `json:"request_path_prefix,omitempty"`
	Method                  string `json:"method,omitempty"`
	RelayMode               string `json:"relay_mode,omitempty"`
	ModelPrefix             string `json:"model_prefix,omitempty"`
	RequestBodyFieldExists  string `json:"request_body_field_exists,omitempty"`
	RequestBodyFieldMissing string `json:"request_body_field_missing,omitempty"`
}

type RestRouteTarget struct {
	Method string            `json:"method,omitempty"`
	Path   string            `json:"path,omitempty"`
	Query  map[string]string `json:"query,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk"
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string                  `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType           `json:"vertex_key_type,omitempty"`
	OpenRouterEnterprise                  *bool                   `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool                    `json:"claude_beta_query,omitempty"`
	AllowServiceTier                      bool                    `json:"allow_service_tier,omitempty"`
	AllowInferenceGeo                     bool                    `json:"allow_inference_geo,omitempty"`
	AllowSpeed                            bool                    `json:"allow_speed,omitempty"`
	AllowSafetyIdentifier                 bool                    `json:"allow_safety_identifier,omitempty"`
	DisableStore                          bool                    `json:"disable_store,omitempty"`
	AllowIncludeObfuscation               bool                    `json:"allow_include_obfuscation,omitempty"`
	AwsKeyType                            AwsKeyType              `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool                    `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool                    `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64                   `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string                `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string                `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string                `json:"upstream_model_update_ignored_models,omitempty"`
	RestRoutes                            []RestRouteRule         `json:"rest_routes,omitempty"`
	AsyncTask                             *ChannelAsyncTaskConfig `json:"async_task,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}

// ── Universal Async Task Configuration ──────────────────────────────────────

// ChannelAsyncTaskConfig 通用异步任务配置
// 启用后，上游返回的任务型响应会被自动识别并进入异步轮询模式。
// 存储在 ChannelOtherSettings.AsyncTask 中。
type ChannelAsyncTaskConfig struct {
	Enabled  bool `json:"enabled"`   // 总开关
	SyncMode bool `json:"sync_mode"` // 同步模式：轮询完成后直接返回结果给客户端

	// ── 提交响应解析 ──
	TaskIDPath      string   `json:"task_id_path"`     // 响应中 taskId 的路径, 如 "taskId" 或 "data.task_id"
	StatusPath      string   `json:"status_path"`      // 响应中 status 的路径
	ErrorCodePath   string   `json:"error_code_path"`  // 错误码路径（可选）
	ErrorMsgPath    string   `json:"error_msg_path"`   // 错误消息路径（可选）
	SuccessStatuses []string `json:"success_statuses"` // 提交成功的状态值, 如 ["RUNNING","QUEUED"]

	// ── 轮询查询 ──
	QueryMethod string            `json:"query_method"` // "GET" | "POST"
	QueryPath   string            `json:"query_path"`   // 查询端点, 如 "/openapi/v1/query"
	QueryBody   map[string]string `json:"query_body"`   // POST 查询的请求体模板, {"taskId":"${task_id}"}

	// ── 查询结果解析 ──
	StatusMap      map[string]string `json:"status_map"`       // 上游状态 → 内部状态, {"RUNNING":"running","SUCCESS":"succeeded"}
	ResultListPath string            `json:"result_list_path"` // 结果列表路径, 如 "results"
	ResultURLPath  string            `json:"result_url_path"`  // 结果 URL 路径(相对结果项), 如 "url"
	ResultTypePath string            `json:"result_type_path"` // 结果类型路径, 如 "outputType"

	// ── 输出类型 ──
	OutputType string `json:"output_type"` // "image" | "video" | "audio" | "text"

	// ── 轮询策略 ──
	PollIntervalSec int `json:"poll_interval_sec"` // 轮询间隔(秒), 默认 5
	MaxPollAttempts int `json:"max_poll_attempts"` // 最大轮询次数, 默认 120
}

// Defaults fills zero-value fields with sensible defaults.
func (c *ChannelAsyncTaskConfig) Defaults() {
	if c.TaskIDPath == "" {
		c.TaskIDPath = "taskId"
	}
	if c.StatusPath == "" {
		c.StatusPath = "status"
	}
	if len(c.SuccessStatuses) == 0 {
		c.SuccessStatuses = []string{"RUNNING", "QUEUED", "PENDING", "PROCESSING", "SUBMITTED", "IN_PROGRESS", "COMPLETED", "SUCCESS", "queued", "in_progress", "completed"}
	}
	if c.QueryMethod == "" {
		c.QueryMethod = "GET"
	}
	if c.QueryBody == nil {
		c.QueryBody = map[string]string{"taskId": "${task_id}"}
	}
	if c.StatusMap == nil {
		c.StatusMap = map[string]string{
			"QUEUED":      "pending",
			"RUNNING":     "running",
			"PENDING":     "pending",
			"PROCESSING":  "running",
			"SUBMITTED":   "running",
			"IN_PROGRESS": "running",
			"SUCCESS":     "succeeded",
			"SUCCEEDED":   "succeeded",
			"COMPLETED":   "succeeded",
			"FAILED":      "failed",
			"FAILURE":     "failed",
			"ERROR":       "failed",
			"queued":      "pending",
			"in_progress": "running",
			"completed":   "succeeded",
			"failed":      "failed",
		}
	}
	if c.ResultListPath == "" {
		c.ResultListPath = "results"
	}
	if c.ResultURLPath == "" {
		c.ResultURLPath = "url"
	}
	if c.ResultTypePath == "" {
		c.ResultTypePath = "outputType"
	}
	if c.OutputType == "" {
		c.OutputType = "image"
	}
	if c.PollIntervalSec <= 0 {
		c.PollIntervalSec = 5
	}
	if c.MaxPollAttempts <= 0 {
		c.MaxPollAttempts = 120
	}
}

// IsActive returns true if the config is enabled and minimally configured.
func (c *ChannelAsyncTaskConfig) IsActive() bool {
	return c != nil && c.Enabled && c.TaskIDPath != ""
}
