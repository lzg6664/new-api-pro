package dto

type FileTransferRequest struct {
	URL      string `json:"url"`
	Output   string `json:"output,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Quality  int    `json:"quality,omitempty"`
}

type FileTransferResponse struct {
	Value        string `json:"value,omitempty"`
	URL          string `json:"url,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	OriginalSize int64  `json:"original_size,omitempty"`
	StoredSize   int64  `json:"stored_size,omitempty"`
}

type RequestPreviewRequest struct {
	RequestPath string            `json:"request_path"`
	Method      string            `json:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        string            `json:"body,omitempty"`
}

type RequestPreviewResponse struct {
	MatchedRoute       string            `json:"matched_route,omitempty"`
	FinalMethod        string            `json:"final_method"`
	FinalURL           string            `json:"final_url"`
	Headers            map[string]string `json:"headers,omitempty"`
	Body               string            `json:"body,omitempty"`
	ParamOverrideAudit []string          `json:"param_override_audit,omitempty"`
}
