package augment

import (
	"encoding/json"
	"sort"
	"strings"
)

func toOpenAI2Request(ar *AugmentRequest) ([]byte, error) {
	instructions := buildCommonSystemText(ar)
	input := buildOpenAI2Input(ar)
	tools := buildOpenAI2Tools(ar.EffectiveTools())

	req := map[string]interface{}{
		"model":  ar.Model,
		"input":  input,
		"stream": ar.IsStreaming(),
	}
	if instructions != "" {
		req["instructions"] = instructions
	}
	if ar.MaxTokens > 0 {
		req["max_output_tokens"] = ar.MaxTokens
	}
	if len(tools) > 0 {
		req["tools"] = tools
		req["tool_choice"] = "auto"
	}

	req = sanitizeProviderRequest("openai2", req)
	return json.Marshal(req)
}

func buildOpenAI2Input(ar *AugmentRequest) []map[string]interface{} {
	var input []map[string]interface{}
	history, currentNodes := preprocessHistoryForAPI(ar)
	for i := range history {
		entry := &history[i]
		reqNodes := entry.EffectiveRequestNodes()
		if content := buildOpenAI2UserContent(reqNodes, entry.RequestMessage, nil, false, "", false); content != nil {
			input = append(input, map[string]interface{}{"type": "message", "role": "user", "content": content})
		}

		respNodes := entry.EffectiveResponseNodes()
		if text := assistantResponseText(entry.ResponseText, respNodes); text != "" {
			input = append(input, map[string]interface{}{"type": "message", "role": "assistant", "content": text})
		}

		for _, toolCall := range extractResponseToolCalls(respNodes) {
			input = append(input, map[string]interface{}{
				"type":      "function_call",
				"call_id":   toolCall.ID,
				"name":      toolCall.Name,
				"arguments": toolCall.Arguments,
			})
		}

		if i+1 < len(history) {
			input = append(input, buildOpenAI2FunctionCallOutputs(history[i+1].EffectiveRequestNodes())...)
		}
	}

	input = append(input, buildOpenAI2FunctionCallOutputs(currentNodes)...)
	if content := buildOpenAI2UserContent(currentNodes, ar.Message, ar.EffectiveContext(), true, ar.MessageSource, ar.DisableSelectedCodeDetails); content != nil {
		input = append(input, map[string]interface{}{"type": "message", "role": "user", "content": content})
	}

	return repairOpenAI2Input(input)
}

func buildOpenAI2UserContent(nodes []Node, fallbackText string, ctx *ContextBlock, includeContext bool, messageSource string, disableSelectedCodeDetails bool) interface{} {
	context := ctx
	if !includeContext {
		context = nil
	}
	nonToolNodes := excludeToolResultNodes(nodes)
	text := buildUserPromptText(nil, nonToolNodes, fallbackText, context, messageSource, disableSelectedCodeDetails)
	imageBlocks := extractImageBlocks(nonToolNodes)
	if text == "" && len(imageBlocks) == 0 {
		return nil
	}
	if len(imageBlocks) == 0 {
		return text
	}
	parts := make([]map[string]interface{}, 0, len(imageBlocks)+1)
	if text != "" {
		parts = append(parts, map[string]interface{}{"type": "input_text", "text": text})
	}
	for _, block := range imageBlocks {
		source, _ := block["source"].(map[string]interface{})
		mediaType, _ := source["media_type"].(string)
		data, _ := source["data"].(string)
		if data == "" {
			continue
		}
		if mediaType == "" {
			mediaType = defaultImageMediaType
		}
		parts = append(parts, map[string]interface{}{
			"type":      "input_image",
			"image_url": "data:" + mediaType + ";base64," + data,
			"detail":    "auto",
		})
	}
	return parts
}

func buildOpenAI2FunctionCallOutputs(nodes []Node) []map[string]interface{} {
	toolResults := extractToolResultNodes(nodes)
	if len(toolResults) == 0 {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(toolResults))
	for _, tr := range toolResults {
		callID := strings.TrimSpace(tr.EffectiveToolUseID())
		if callID == "" {
			continue
		}
		out = append(out, map[string]interface{}{
			"type":    "function_call_output",
			"call_id": callID,
			"output":  stringifyOpenAI2ToolResult(tr),
		})
	}
	return out
}

func stringifyOpenAI2ToolResult(tr *ToolResultNode) string {
	if tr == nil {
		return ""
	}
	return stringifyToolResultContent(buildOpenAIToolResultContent(tr))
}

func buildOpenAI2Tools(defs []ToolDefinition) []map[string]interface{} {
	if len(defs) == 0 {
		return nil
	}
	tools := make([]map[string]interface{}, 0, len(defs))
	for _, def := range defs {
		if strings.TrimSpace(def.Name) == "" {
			continue
		}
		tool := map[string]interface{}{
			"type":       "function",
			"name":       def.Name,
			"parameters": coerceOpenAI2StrictSchema(def.EffectiveInputSchema(), 0),
			"strict":     true,
		}
		if strings.TrimSpace(def.Description) != "" {
			tool["description"] = def.Description
		}
		tools = append(tools, tool)
	}
	return tools
}

func coerceOpenAI2StrictSchema(schema map[string]interface{}, depth int) map[string]interface{} {
	if schema == nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []string{}, "additionalProperties": false}
	}
	return coerceOpenAI2SchemaValue(schema, depth).(map[string]interface{})
}

func coerceOpenAI2SchemaValue(value interface{}, depth int) interface{} {
	if depth > 50 {
		return value
	}
	switch v := value.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(v))
		for key, val := range v {
			out[key] = coerceOpenAI2SchemaValue(val, depth+1)
		}

		hasProps := false
		props, ok := out["properties"].(map[string]interface{})
		if ok {
			hasProps = true
		}

		objectType := false
		switch t := out["type"].(type) {
		case string:
			objectType = strings.EqualFold(strings.TrimSpace(t), "object")
		case []interface{}:
			for _, item := range t {
				if strings.EqualFold(strings.TrimSpace(toString(item)), "object") {
					objectType = true
					break
				}
			}
		}

		if objectType || hasProps {
			if !objectType {
				out["type"] = "object"
			}
			if !hasProps {
				props = map[string]interface{}{}
				out["properties"] = props
			}
			keys := make([]string, 0, len(props))
			for key := range props {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out["required"] = keys
			out["additionalProperties"] = false
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(v))
		for _, item := range v {
			out = append(out, coerceOpenAI2SchemaValue(item, depth+1))
		}
		return out
	default:
		return value
	}
}

func repairOpenAI2Input(input []map[string]interface{}) []map[string]interface{} {
	if len(input) == 0 {
		return input
	}

	type functionCall struct {
		CallID    string
		Name      string
		Arguments string
	}

	out := make([]map[string]interface{}, 0, len(input)+2)
	var pending map[string]functionCall
	var bufferedOrphans []map[string]interface{}

	injectMissing := func() {
		if len(pending) == 0 {
			pending = nil
			return
		}
		keys := make([]string, 0, len(pending))
		for id := range pending {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			call := pending[id]
			out = append(out, map[string]interface{}{
				"type":    "function_call_output",
				"call_id": call.CallID,
				"output":  buildMissingToolResultContent("call_id", call.CallID, call.Name, call.Arguments),
			})
		}
		pending = nil
	}

	flushOrphans := func() {
		if len(bufferedOrphans) == 0 {
			return
		}
		for _, item := range bufferedOrphans {
			out = append(out, map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": buildTaggedOrphanContent("orphan_function_call_output", "call_id", item["call_id"], item["output"]),
			})
		}
		bufferedOrphans = nil
	}

	closePending := func() {
		injectMissing()
		flushOrphans()
	}

	for _, item := range input {
		itemType, _ := item["type"].(string)
		if pending != nil {
			switch itemType {
			case "function_call":
				out = append(out, item)
				callID, _ := item["call_id"].(string)
				name, _ := item["name"].(string)
				args, _ := item["arguments"].(string)
				if callID != "" {
					pending[callID] = functionCall{CallID: callID, Name: name, Arguments: args}
				}
				continue
			case "function_call_output":
				callID, _ := item["call_id"].(string)
				if callID != "" {
					if _, ok := pending[callID]; ok {
						delete(pending, callID)
						out = append(out, item)
						if len(pending) == 0 {
							pending = nil
							flushOrphans()
						}
					} else {
						bufferedOrphans = append(bufferedOrphans, item)
					}
					continue
				}
			}
			closePending()
		}

		switch itemType {
		case "function_call":
			out = append(out, item)
			callID, _ := item["call_id"].(string)
			name, _ := item["name"].(string)
			args, _ := item["arguments"].(string)
			if callID != "" {
				if pending == nil {
					pending = make(map[string]functionCall)
				}
				pending[callID] = functionCall{CallID: callID, Name: name, Arguments: args}
				bufferedOrphans = nil
			}
		case "function_call_output":
			out = append(out, map[string]interface{}{
				"type":    "message",
				"role":    "user",
				"content": buildTaggedOrphanContent("orphan_function_call_output", "call_id", item["call_id"], item["output"]),
			})
		default:
			out = append(out, item)
		}
	}

	closePending()
	return out
}
