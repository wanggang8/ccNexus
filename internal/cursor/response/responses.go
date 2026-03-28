package response

import (
	"encoding/json"

	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func FixResponsesBody(body []byte, clientModel string, cacheMessages []map[string]interface{}, transformerName string, thinkingCache *cursorcache.ThinkingCache) ([]byte, error) {
	payload, ok := decodeJSONObject(body)
	if !ok {
		return body, nil
	}
	if clientModel != "" {
		payload["model"] = clientModel
	}
	if transformerName == "cx_resp_claude" {
		normalizeResponsesFunctionCalls(payload)
	}
	if len(cacheMessages) > 0 && thinkingCache != nil {
		if output, ok := payload["output"].([]interface{}); ok {
			thinkingCache.StoreFromResponsesOutput(cacheMessages, output)
		}
	}
	return json.Marshal(payload)
}

func normalizeResponsesFunctionCalls(payload map[string]interface{}) {
	if payload == nil {
		return
	}
	output, ok := payload["output"].([]interface{})
	if !ok || len(output) == 0 {
		return
	}

	for _, rawItem := range output {
		item, ok := rawItem.(map[string]interface{})
		if !ok || stringValue(item["type"]) != "function_call" {
			continue
		}
		normalizeResponsesFunctionCallItem(item)
	}
}

func normalizeResponsesFunctionCallItem(item map[string]interface{}) {
	if item == nil {
		return
	}
	argsStr := stringValue(item["arguments"])
	if argsStr == "" {
		return
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		return
	}

	args = applyToolArgFixes(stringValue(item["name"]), args)
	encoded, err := json.Marshal(args)
	if err != nil {
		return
	}
	item["arguments"] = string(encoded)
}
