package augment

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/lich0821/ccNexus/internal/tokencount"
)

// EstimateInputTokensForTransformedRequest estimates input tokens from the
// provider-specific request payload that Augment actually sends upstream.
func EstimateInputTokensForTransformedRequest(body []byte, targetType string) int {
	req, err := buildCountTokensRequestFromTransformedRequest(body, targetType)
	if err != nil || req == nil {
		return 0
	}
	if len(req.Messages) == 0 && req.System == nil && len(req.Tools) == 0 {
		return 0
	}
	if estimated := tokencount.EstimateInputTokens(req); estimated > 0 {
		return estimated
	}
	return 0
}

func buildCountTokensRequestFromTransformedRequest(body []byte, targetType string) (*tokencount.CountTokensRequest, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var raw map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}

	switch strings.ToLower(strings.TrimSpace(targetType)) {
	case "claude", "cli":
		return &tokencount.CountTokensRequest{
			Model:    firstString(raw, "model"),
			Messages: convertStructuredMessagesForTokenCount(firstArray(raw, "messages")),
			System:   cloneJSONValue(firstValue(raw, "system")),
			Tools:    convertClaudeToolsForTokenCount(firstArray(raw, "tools")),
		}, nil
	case "openai":
		return &tokencount.CountTokensRequest{
			Model:    firstString(raw, "model"),
			Messages: convertOpenAIMessagesForTokenCount(firstArray(raw, "messages")),
			Tools:    convertOpenAIToolsForTokenCount(firstArray(raw, "tools")),
		}, nil
	case "openai2":
		return &tokencount.CountTokensRequest{
			Model:    firstString(raw, "model"),
			Messages: convertOpenAI2ItemsForTokenCount(firstArray(raw, "input")),
			System:   cloneJSONValue(firstValue(raw, "instructions")),
			Tools:    convertOpenAI2ToolsForTokenCount(firstArray(raw, "tools")),
		}, nil
	default:
		return nil, nil
	}
}

func convertStructuredMessagesForTokenCount(items []interface{}) []tokencount.MessageParam {
	out := make([]tokencount.MessageParam, 0, len(items))
	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msg["content"]
		if !ok || content == nil {
			continue
		}
		role := strings.TrimSpace(firstString(msg, "role"))
		if role == "" {
			role = "user"
		}
		out = append(out, tokencount.MessageParam{Role: role, Content: cloneJSONValue(content)})
	}
	return out
}

func convertOpenAIMessagesForTokenCount(items []interface{}) []tokencount.MessageParam {
	out := make([]tokencount.MessageParam, 0, len(items))
	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		content := openAIMessageContentForTokenCount(msg)
		if content == nil {
			continue
		}
		role := strings.TrimSpace(firstString(msg, "role"))
		if role == "" {
			role = "user"
		}
		out = append(out, tokencount.MessageParam{Role: role, Content: content})
	}
	return out
}

func openAIMessageContentForTokenCount(msg map[string]interface{}) interface{} {
	payload := make(map[string]interface{}, 3)
	if content, ok := msg["content"]; ok && content != nil {
		payload["content"] = cloneJSONValue(content)
	}
	if toolCalls, ok := msg["tool_calls"]; ok && toolCalls != nil {
		payload["tool_calls"] = cloneJSONValue(toolCalls)
	}
	if toolCallID := strings.TrimSpace(toString(msg["tool_call_id"])); toolCallID != "" {
		payload["tool_call_id"] = toolCallID
	}
	if len(payload) == 0 {
		return nil
	}
	if len(payload) == 1 {
		if content, ok := payload["content"]; ok {
			return content
		}
	}
	return payload
}

func convertOpenAI2ItemsForTokenCount(items []interface{}) []tokencount.MessageParam {
	out := make([]tokencount.MessageParam, 0, len(items))
	for _, item := range items {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		role := strings.TrimSpace(firstString(msg, "role"))
		if role == "" {
			switch strings.ToLower(strings.TrimSpace(firstString(msg, "type"))) {
			case "function_call", "reasoning":
				role = "assistant"
			case "function_call_output":
				role = "tool"
			default:
				role = "user"
			}
		}
		out = append(out, tokencount.MessageParam{Role: role, Content: cloneJSONValue(msg)})
	}
	return out
}

func convertOpenAIToolsForTokenCount(items []interface{}) []tokencount.Tool {
	out := make([]tokencount.Tool, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		fn := firstMap(tool, "function")
		if fn == nil || strings.TrimSpace(firstString(fn, "name")) == "" {
			continue
		}
		out = append(out, tokencount.Tool{Name: firstString(fn, "name"), Description: firstString(fn, "description"), InputSchema: cloneJSONValue(firstValue(fn, "parameters"))})
	}
	return out
}

func convertClaudeToolsForTokenCount(items []interface{}) []tokencount.Tool {
	out := make([]tokencount.Tool, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]interface{})
		if !ok || strings.TrimSpace(firstString(tool, "name")) == "" {
			continue
		}
		out = append(out, tokencount.Tool{Name: firstString(tool, "name"), Description: firstString(tool, "description"), InputSchema: cloneJSONValue(firstValue(tool, "input_schema"))})
	}
	return out
}

func convertOpenAI2ToolsForTokenCount(items []interface{}) []tokencount.Tool {
	out := make([]tokencount.Tool, 0, len(items))
	for _, item := range items {
		tool, ok := item.(map[string]interface{})
		if !ok || strings.TrimSpace(firstString(tool, "name")) == "" {
			continue
		}
		out = append(out, tokencount.Tool{Name: firstString(tool, "name"), Description: firstString(tool, "description"), InputSchema: cloneJSONValue(firstValue(tool, "parameters"))})
	}
	return out
}
