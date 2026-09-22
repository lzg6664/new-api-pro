package dto

// ImageTaskSubmitReceipt 是图片异步提交模式的固定回执。
// 客户端在 POST /v1/images/generations?async=1 时立即收到本结构，
// 之后通过 GET /v1/images/tasks/{task_id} 轮询终态。
type ImageTaskSubmitReceipt struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

// ImageTaskQueryResponse 是 GET /v1/images/tasks/{task_id} 的响应。
// Status 为 model.Task 状态原样小写（submitted/in_progress/success/failure 等），
// Data 仅在 Status == success 时填充。
type ImageTaskQueryResponse struct {
	TaskID     string     `json:"task_id"`
	Status     string     `json:"status"`
	FailReason string     `json:"fail_reason,omitempty"`
	Progress   string     `json:"progress,omitempty"`
	Data       []ImageData `json:"data,omitempty"`
}
