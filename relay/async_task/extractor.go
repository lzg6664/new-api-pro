package async_task

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// extractByPath 从 JSON 中按点分路径提取值，支持数组索引。
// 路径格式: "field" "parent.child" "results[0].url" "data.items[2].name"
func extractByPath(body []byte, path string) string {
	if path == "" {
		return ""
	}
	var data any
	if err := common.Unmarshal(body, &data); err != nil {
		return ""
	}
	parts := parsePath(path)
	current := data
	for _, part := range parts {
		current = navigateField(current, part)
		if current == nil {
			return ""
		}
	}
	return fmt.Sprint(current)
}

// parsePath splits a dot-path into segments, handling array index syntax.
// e.g. "results[0].url" → ["results", "[0]", "url"]
func parsePath(path string) []string {
	raw := strings.Split(path, ".")
	var parts []string
	for _, r := range raw {
		// Handle bracket notation: "field[0]" → "field", "[0]"
		bracketIdx := strings.Index(r, "[")
		if bracketIdx > 0 {
			parts = append(parts, r[:bracketIdx])
			parts = append(parts, r[bracketIdx:])
		} else {
			parts = append(parts, r)
		}
	}
	return parts
}

// navigateField navigates one level into a JSON value by field name or array index.
func navigateField(current any, part string) any {
	if current == nil {
		return nil
	}

	// Array index: [0], [3], etc.
	if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
		idxStr := part[1 : len(part)-1]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil
		}
		arr, ok := current.([]any)
		if !ok || idx < 0 || idx >= len(arr) {
			return nil
		}
		return arr[idx]
	}

	// Object field
	m, ok := current.(map[string]any)
	if !ok {
		return nil
	}
	return m[part]
}

// replaceByPath replaces a JSON value at a dot/bracket path and returns the updated body.
// When the path cannot be resolved, the original body is returned unchanged.
func replaceByPath(body []byte, path string, value any) []byte {
	if path == "" || len(body) == 0 {
		return body
	}

	var data any
	if err := common.Unmarshal(body, &data); err != nil {
		return body
	}

	parts := parsePath(path)
	if len(parts) == 0 {
		return body
	}

	if !setFieldValue(data, parts, value) {
		return body
	}

	updated, err := common.Marshal(data)
	if err != nil {
		return body
	}
	return updated
}

func setFieldValue(current any, parts []string, value any) bool {
	if len(parts) == 0 {
		return false
	}

	part := parts[0]
	isLast := len(parts) == 1

	if strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]") {
		idxStr := part[1 : len(part)-1]
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return false
		}
		arr, ok := current.([]any)
		if !ok || idx < 0 || idx >= len(arr) {
			return false
		}
		if isLast {
			arr[idx] = value
			return true
		}
		return setFieldValue(arr[idx], parts[1:], value)
	}

	obj, ok := current.(map[string]any)
	if !ok {
		return false
	}
	if isLast {
		if _, exists := obj[part]; !exists {
			return false
		}
		obj[part] = value
		return true
	}
	next, exists := obj[part]
	if !exists {
		return false
	}
	return setFieldValue(next, parts[1:], value)
}
