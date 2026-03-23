package request

import (
	"encoding/json"
	"strings"
)

// InferModelID extracts model id from request payload.
func InferModelID(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	for _, key := range []string{"model", "model_id"} {
		value, exists := payload[key]
		if !exists {
			continue
		}
		modelID, ok := value.(string)
		if !ok {
			continue
		}
		return strings.TrimSpace(modelID)
	}
	return ""
}
