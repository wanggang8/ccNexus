package compat

import "encoding/json"

// RequestShape describes the high-level semantic shape of an incoming request body.
type RequestShape string

const (
	RequestShapeUnknown         RequestShape = "unknown"
	RequestShapeOpenAIChat      RequestShape = "openai_chat"
	RequestShapeOpenAIResponses RequestShape = "openai_responses"
	RequestShapeClaudeMessages  RequestShape = "claude_messages"
)

// DetectRequestShape inspects a request payload and returns the most likely protocol shape.
func DetectRequestShape(req []byte) RequestShape {
	var data map[string]interface{}
	if err := json.Unmarshal(req, &data); err != nil {
		return RequestShapeUnknown
	}
	return DetectRequestShapeMap(data)
}

// DetectRequestShapeMap inspects a decoded request map and returns the most likely protocol shape.
func DetectRequestShapeMap(data map[string]interface{}) RequestShape {
	if IsClaudeMessagesLike(data) {
		return RequestShapeClaudeMessages
	}
	if IsOpenAIResponsesLike(data) {
		return RequestShapeOpenAIResponses
	}
	if IsOpenAIChatLike(data) {
		return RequestShapeOpenAIChat
	}
	return RequestShapeUnknown
}

func IsClaudeMessagesLike(data map[string]interface{}) bool {
	messages, ok := data["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	if _, hasSystem := data["system"]; hasSystem {
		return true
	}

	if tools, ok := data["tools"].([]interface{}); ok {
		for _, tool := range tools {
			toolMap, ok := tool.(map[string]interface{})
			if !ok {
				continue
			}
			if _, ok := toolMap["input_schema"]; ok {
				return true
			}
		}
	}

	for _, message := range messages {
		msgMap, ok := message.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := msgMap["content"].([]interface{})
		if !ok || len(content) == 0 {
			continue
		}
		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			blockType, _ := block["type"].(string)
			switch blockType {
			case "tool_result", "tool_use", "thinking":
				return true
			case "image":
				if _, ok := block["source"]; ok {
					return true
				}
			}
		}
	}

	return false
}

func IsOpenAIChatLike(data map[string]interface{}) bool {
	messages, ok := data["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		return false
	}

	if _, hasSystem := data["system"]; hasSystem {
		return false
	}

	if tools, ok := data["tools"].([]interface{}); ok {
		for _, tool := range tools {
			toolMap, ok := tool.(map[string]interface{})
			if !ok {
				continue
			}
			if toolMap["type"] == "function" {
				return true
			}
		}
	}

	for _, message := range messages {
		msgMap, ok := message.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		switch role {
		case "system", "user", "assistant", "tool":
		default:
			continue
		}
		if _, ok := msgMap["tool_calls"]; ok {
			return true
		}
		if _, ok := msgMap["tool_call_id"]; ok {
			return true
		}
		if content, ok := msgMap["content"].(string); ok && content != "" {
			return true
		}
	}

	return true
}

func IsOpenAIResponsesLike(data map[string]interface{}) bool {
	if _, ok := data["input"]; ok {
		return true
	}
	if _, ok := data["include"]; ok {
		return true
	}
	if _, ok := data["store"]; ok {
		return true
	}
	if _, ok := data["reasoning"]; ok {
		return true
	}
	return false
}
