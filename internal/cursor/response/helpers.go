package response

import (
	"encoding/json"
	"fmt"
)

func decodeJSONObject(body []byte) (map[string]interface{}, bool) {
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
