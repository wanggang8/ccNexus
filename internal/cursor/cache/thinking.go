package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Entry struct {
	Reasoning string
	StoredAt  time.Time
}

type ThinkingCache struct {
	Store map[string]Entry
}

const TTL = 24 * time.Hour

func NewThinkingCache() *ThinkingCache {
	return &ThinkingCache{
		Store: make(map[string]Entry),
	}
}

func (c *ThinkingCache) Inject(messages []map[string]interface{}) []map[string]interface{} {
	if c == nil || len(messages) == 0 {
		return messages
	}
	sessionID := thinkingSessionID(messages)
	if sessionID == "" {
		return messages
	}

	now := time.Now()
	injected := make([]map[string]interface{}, 0, len(messages))
	for _, message := range messages {
		cloned := cloneJSONObject(message)
		if stringValue(cloned["role"]) == "assistant" && stringValue(cloned["reasoning_content"]) == "" {
			key := sessionID + ":" + thinkingMessageHash(cloned)
			if entry, ok := c.Store[key]; ok && now.Sub(entry.StoredAt) < TTL {
				cloned["reasoning_content"] = entry.Reasoning
			}
		}
		injected = append(injected, cloned)
	}
	return injected
}

func (c *ThinkingCache) StoreFromResponse(messages []map[string]interface{}, assistantMessage map[string]interface{}) {
	if c == nil || len(messages) == 0 || len(assistantMessage) == 0 {
		return
	}
	reasoning := stringValue(assistantMessage["reasoning_content"])
	if reasoning == "" {
		return
	}

	sessionID := thinkingSessionID(messages)
	if sessionID == "" {
		return
	}

	key := sessionID + ":" + thinkingMessageHash(map[string]interface{}{
		"role":       "assistant",
		"content":    "",
		"tool_calls": []interface{}{},
	})
	c.Store[key] = Entry{
		Reasoning: reasoning,
		StoredAt:  time.Now(),
	}
	c.cleanup()
}

func (c *ThinkingCache) StoreFromResponsesOutput(messages []map[string]interface{}, output interface{}) {
	if c == nil || len(messages) == 0 {
		return
	}
	items, ok := output.([]interface{})
	if !ok {
		return
	}
	reasoning := extractResponsesReasoningFromOutput(items)
	if reasoning == "" {
		return
	}
	c.StoreFromResponse(messages, map[string]interface{}{"reasoning_content": reasoning})
}

func InjectClaudeThinking(message map[string]interface{}, reasoning string) {
	if message == nil || reasoning == "" {
		return
	}

	content := message["content"]
	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			message["content"] = []interface{}{
				map[string]interface{}{"type": "thinking", "thinking": reasoning},
			}
			return
		}
		message["content"] = []interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
			map[string]interface{}{"type": "text", "text": value},
		}
	case []interface{}:
		for _, item := range value {
			block, ok := item.(map[string]interface{})
			if ok && stringValue(block["type"]) == "thinking" {
				return
			}
		}
		message["content"] = append([]interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
		}, value...)
	default:
		message["content"] = []interface{}{
			map[string]interface{}{"type": "thinking", "thinking": reasoning},
		}
	}
}

func InjectGeminiThought(content map[string]interface{}, reasoning string) {
	if content == nil || reasoning == "" {
		return
	}
	rawParts, ok := content["parts"].([]interface{})
	if !ok {
		content["parts"] = []interface{}{
			map[string]interface{}{"text": reasoning, "thought": true},
		}
		return
	}
	for _, rawPart := range rawParts {
		part, ok := rawPart.(map[string]interface{})
		if ok && part["thought"] == true {
			return
		}
	}
	content["parts"] = append([]interface{}{
		map[string]interface{}{"text": reasoning, "thought": true},
	}, rawParts...)
}

func (c *ThinkingCache) cleanup() {
	if c == nil || len(c.Store) < 100 {
		return
	}
	now := time.Now()
	for key, entry := range c.Store {
		if now.Sub(entry.StoredAt) >= TTL {
			delete(c.Store, key)
		}
	}
}

func thinkingSessionID(messages []map[string]interface{}) string {
	firstUser := ""
	firstAssistant := ""
	for _, message := range messages {
		role := stringValue(message["role"])
		if role == "system" || role == "developer" {
			continue
		}
		if role == "user" && firstUser == "" {
			firstUser = normalizeThinkingContent(message["content"])
		} else if role == "assistant" && firstAssistant == "" {
			firstAssistant = normalizeThinkingContent(message["content"])
		}
		if firstUser != "" && firstAssistant != "" {
			break
		}
	}
	if firstUser == "" || firstAssistant == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(firstUser + "|" + firstAssistant))
	return fmt.Sprintf("%x", sum)[:16]
}

func thinkingMessageHash(message map[string]interface{}) string {
	content := normalizeThinkingContent(message["content"])
	toolIDs := make([]string, 0)
	if toolCalls, ok := message["tool_calls"].([]interface{}); ok {
		for _, rawToolCall := range toolCalls {
			toolCall, ok := rawToolCall.(map[string]interface{})
			if !ok {
				continue
			}
			toolIDs = append(toolIDs, normalizeToolID(stringValue(toolCall["id"])))
		}
	}
	sort.Strings(toolIDs)
	raw, _ := json.Marshal(map[string]interface{}{"c": content, "t": toolIDs})
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum)[:16]
}

func normalizeThinkingContent(content interface{}) string {
	switch value := content.(type) {
	case []interface{}:
		parts := make([]string, 0, len(value))
		for _, item := range value {
			switch block := item.(type) {
			case string:
				parts = append(parts, block)
			case map[string]interface{}:
				if stringValue(block["type"]) == "text" {
					parts = append(parts, stringValue(block["text"]))
				}
			}
		}
		return stripThinkFragments(strings.Join(parts, "\n"))
	case string:
		return stripThinkFragments(value)
	default:
		if value == nil {
			return ""
		}
		return stripThinkFragments(fmt.Sprint(value))
	}
}

func stripThinkFragments(text string) string {
	cleaned := text
	for {
		start := strings.Index(cleaned, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(cleaned[start+len("<think>"):], "</think>")
		if end < 0 {
			cleaned = cleaned[:start]
			break
		}
		cleaned = cleaned[:start] + cleaned[start+len("<think>")+end+len("</think>"):]
	}
	return strings.TrimSpace(cleaned)
}

func normalizeToolID(id string) string {
	if id == "" {
		return ""
	}
	var builder strings.Builder
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' {
			builder.WriteRune(ch)
		}
	}
	return builder.String()
}

func extractResponsesReasoningFromOutput(items []interface{}) string {
	var builder strings.Builder
	for _, itemValue := range items {
		item, ok := itemValue.(map[string]interface{})
		if !ok || stringValue(item["type"]) != "reasoning" {
			continue
		}
		summary, ok := item["summary"].([]interface{})
		if !ok {
			continue
		}
		for _, partValue := range summary {
			part, ok := partValue.(map[string]interface{})
			if !ok {
				continue
			}
			builder.WriteString(stringValue(part["text"]))
		}
	}
	return builder.String()
}

func cloneJSONObject(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		cloned := make(map[string]interface{}, len(payload))
		for key, value := range payload {
			cloned[key] = value
		}
		return cloned
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(raw, &cloned); err != nil {
		cloned = make(map[string]interface{}, len(payload))
		for key, value := range payload {
			cloned[key] = value
		}
	}
	return cloned
}

func stringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
