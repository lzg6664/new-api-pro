package async_task

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// extractByPath 从 JSON 中按点分路径提取值，支持数组索引。
// 路径格式: "field" "parent.child" "results[0].url" "data.items[2].name"
func extractByPath(body []byte, path string) string {
	if path == "" {
		return ""
	}
	var data any
	if err := json.Unmarshal(body, &data); err != nil {
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
