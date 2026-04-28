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
	AzureResponsesVersion                 string          `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType   `json:"vertex_key_type,omitempty"`
	OpenRouterEnterprise                  *bool           `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool            `json:"claude_beta_query,omitempty"`
	AllowServiceTier                      bool            `json:"allow_service_tier,omitempty"`
	AllowInferenceGeo                     bool            `json:"allow_inference_geo,omitempty"`
	AllowSpeed                            bool            `json:"allow_speed,omitempty"`
	AllowSafetyIdentifier                 bool            `json:"allow_safety_identifier,omitempty"`
	DisableStore                          bool            `json:"disable_store,omitempty"`
	AllowIncludeObfuscation               bool            `json:"allow_include_obfuscation,omitempty"`
	AwsKeyType                            AwsKeyType      `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool            `json:"upstream_model_update_check_enabled,omitempty"`
	UpstreamModelUpdateAutoSyncEnabled    bool            `json:"upstream_model_update_auto_sync_enabled,omitempty"`
	UpstreamModelUpdateLastCheckTime      int64           `json:"upstream_model_update_last_check_time,omitempty"`
	UpstreamModelUpdateLastDetectedModels []string        `json:"upstream_model_update_last_detected_models,omitempty"`
	UpstreamModelUpdateLastRemovedModels  []string        `json:"upstream_model_update_last_removed_models,omitempty"`
	UpstreamModelUpdateIgnoredModels      []string        `json:"upstream_model_update_ignored_models,omitempty"`
	RestRoutes                            []RestRouteRule `json:"rest_routes,omitempty"`
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
