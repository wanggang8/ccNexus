package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

type cursorRealFixture struct {
	name         string
	requestBody  string
	lastUserText string
	toolNames    []string
}

func TestCursorChatRealFixturesAcrossBackends(t *testing.T) {
	fixtures := []cursorRealFixture{
		loadCursorRealFixture(t, "requst-1.log"),
		loadCursorRealFixture(t, "request-2.log"),
	}

	tests := []struct {
		name     string
		endpoint config.Endpoint
		validate func(*testing.T, cursorRealFixture, preparedCursorRoundTrip)
	}{
		{
			name:     "openai",
			endpoint: config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5"},
			validate: func(t *testing.T, fixture cursorRealFixture, prepared preparedCursorRoundTrip) {
				t.Helper()

				if _, ok := prepared.requestPayload["system"]; ok {
					t.Fatalf("expected top-level system removed for openai chat payload, got %#v", prepared.requestPayload["system"])
				}
				if _, ok := prepared.requestPayload["previous_response_id"]; ok {
					t.Fatalf("expected openai chat payload to strip previous_response_id, got %#v", prepared.requestPayload["previous_response_id"])
				}
				if prepared.requestPayload["tool_choice"] != "auto" {
					t.Fatalf("expected openai chat tool_choice auto, got %#v", prepared.requestPayload["tool_choice"])
				}
				messages := prepared.requestPayload["messages"].([]interface{})
				first := messages[0].(map[string]interface{})
				if first["role"] != "system" {
					t.Fatalf("expected normalized system message prepended, got %#v", first)
				}
				if !strings.Contains(stringValueForTest(first["content"]), "You are an AI coding assistant") {
					t.Fatalf("expected system text preserved, got %#v", first["content"])
				}
				if hasCacheControlInOpenAIMessages(messages) {
					t.Fatalf("expected openai chat payload to strip cache_control from content blocks, got %#v", messages)
				}
				lastUser := lastMessageByRole(messages, "user")
				if !strings.Contains(stringValueForTest(lastUser["content"]), fixture.lastUserText) {
					t.Fatalf("expected latest user query preserved, want %q got %#v", fixture.lastUserText, lastUser["content"])
				}
				if got := extractOpenAIToolNames(prepared.requestPayload["tools"]); !reflect.DeepEqual(got, fixture.toolNames) {
					t.Fatalf("expected openai tools to preserve names %v, got %v", fixture.toolNames, got)
				}
				assertOpenAIShellToolSchema(t, prepared.requestPayload["tools"])
			},
		},
		{
			name:     "claude",
			endpoint: config.Endpoint{Name: "claude", Transformer: "claude", Model: "claude-sonnet-4-20250514"},
			validate: func(t *testing.T, fixture cursorRealFixture, prepared preparedCursorRoundTrip) {
				t.Helper()

				if _, ok := prepared.requestPayload["previous_response_id"]; ok {
					t.Fatalf("expected claude chat payload to strip previous_response_id, got %#v", prepared.requestPayload["previous_response_id"])
				}
				systemText := flattenTextForTest(prepared.requestPayload["system"])
				if !strings.Contains(systemText, "You are an AI coding assistant") {
					t.Fatalf("expected claude request to preserve system prompt, got %#v", prepared.requestPayload["system"])
				}
				if toolChoice := prepared.requestPayload["tool_choice"]; toolChoice != nil {
					toolChoiceMap, ok := toolChoice.(map[string]interface{})
					if !ok || toolChoiceMap["type"] != "auto" {
						t.Fatalf("expected claude tool_choice to be omitted or auto object, got %#v", toolChoice)
					}
				}
				messages := prepared.requestPayload["messages"].([]interface{})
				if first := messages[0].(map[string]interface{}); first["role"] == "system" {
					t.Fatalf("expected claude system promoted out of messages, got %#v", first)
				}
				if !claudePayloadHasBlockType(messages, "tool_use") {
					t.Fatalf("expected claude payload to preserve tool_use blocks, got %#v", messages)
				}
				if !claudePayloadHasBlockType(messages, "tool_result") {
					t.Fatalf("expected claude payload to preserve tool_result blocks, got %#v", messages)
				}
				if !strings.Contains(flattenClaudeMessagesForTest(messages), fixture.lastUserText) {
					t.Fatalf("expected claude payload to preserve latest user query %q, got %#v", fixture.lastUserText, messages[len(messages)-1])
				}
				if got := extractClaudeToolNames(prepared.requestPayload["tools"]); !reflect.DeepEqual(got, fixture.toolNames) {
					t.Fatalf("expected claude tools to preserve names %v, got %v", fixture.toolNames, got)
				}
				assertClaudeShellToolSchema(t, prepared.requestPayload["tools"])
			},
		},
		{
			name:     "gemini",
			endpoint: config.Endpoint{Name: "gemini", Transformer: "gemini", Model: "gemini-2.5-pro"},
			validate: func(t *testing.T, fixture cursorRealFixture, prepared preparedCursorRoundTrip) {
				t.Helper()

				if _, ok := prepared.requestPayload["previous_response_id"]; ok {
					t.Fatalf("expected gemini chat payload to strip previous_response_id, got %#v", prepared.requestPayload["previous_response_id"])
				}
				systemInstruction := prepared.requestPayload["systemInstruction"].(map[string]interface{})
				systemText := flattenTextForTest(systemInstruction["parts"])
				if !strings.Contains(systemText, "You are an AI coding assistant") {
					t.Fatalf("expected gemini request to preserve system instruction, got %#v", prepared.requestPayload["systemInstruction"])
				}
				contents := prepared.requestPayload["contents"].([]interface{})
				if !geminiPayloadHasPart(contents, "functionCall") {
					t.Fatalf("expected gemini payload to preserve functionCall parts, got %#v", contents)
				}
				if !geminiPayloadHasPart(contents, "functionResponse") {
					t.Fatalf("expected gemini payload to preserve functionResponse parts, got %#v", contents)
				}
				if !strings.Contains(flattenGeminiContentsForTest(contents), fixture.lastUserText) {
					t.Fatalf("expected gemini payload to preserve latest user query %q, got %#v", fixture.lastUserText, contents[len(contents)-1])
				}
				if got := extractGeminiToolNames(prepared.requestPayload["tools"]); !reflect.DeepEqual(got, fixture.toolNames) {
					t.Fatalf("expected gemini tools to preserve names %v, got %v", fixture.toolNames, got)
				}
				assertGeminiShellToolSchema(t, prepared.requestPayload["tools"])
			},
		},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		for _, tt := range tests {
			tt := tt
			t.Run(fixture.name+"/"+tt.name, func(t *testing.T) {
				prepared := prepareCursorRoundTrip(t, "/cursor/v1/chat/completions", fixture.requestBody, tt.endpoint)
				tt.validate(t, fixture, prepared)
			})
		}
	}
}

func loadCursorRealFixture(t *testing.T, filename string) cursorRealFixture {
	t.Helper()

	path := filepath.Join("..", "..", "docs", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s failed: %v", path, err)
	}
	text := string(data)

	start := -1
	for _, marker := range []string{"原始请求：", "原始："} {
		idx := strings.Index(text, marker)
		if idx >= 0 && (start == -1 || idx < start) {
			start = idx
		}
	}
	if start < 0 {
		t.Fatalf("fixture %s missing request marker", path)
	}

	jsonStart := strings.Index(text[start:], "{")
	if jsonStart < 0 {
		t.Fatalf("fixture %s missing json body", path)
	}
	jsonStart += start

	decoder := json.NewDecoder(strings.NewReader(text[jsonStart:]))
	var payload map[string]interface{}
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode fixture request failed: %v", err)
	}

	messages, ok := payload["messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("fixture %s missing messages", path)
	}
	lastUserText := stringValueForTest(lastMessageByRole(messages, "user")["content"])
	if strings.TrimSpace(lastUserText) == "" {
		t.Fatalf("fixture %s missing last user text", path)
	}
	toolNames := extractOpenAIToolNames(payload["tools"])
	if len(toolNames) == 0 {
		t.Fatalf("fixture %s missing tools", path)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture request failed: %v", err)
	}

	return cursorRealFixture{
		name:         filename,
		requestBody:  string(encoded),
		lastUserText: lastUserText,
		toolNames:    toolNames,
	}
}

func stringValueForTest(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []interface{}:
		return flattenTextForTest(typed)
	case map[string]interface{}:
		return flattenTextForTest([]interface{}{typed})
	default:
		return ""
	}
}

func flattenTextForTest(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, raw := range typed {
			switch item := raw.(type) {
			case string:
				parts = append(parts, item)
			case map[string]interface{}:
				if text, ok := item["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

func hasCacheControlInOpenAIMessages(messages []interface{}) bool {
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if ok && block["cache_control"] != nil {
				return true
			}
		}
	}
	return false
}

func lastMessageByRole(messages []interface{}, role string) map[string]interface{} {
	for i := len(messages) - 1; i >= 0; i-- {
		message, ok := messages[i].(map[string]interface{})
		if ok && message["role"] == role {
			return message
		}
	}
	return map[string]interface{}{}
}

func claudePayloadHasBlockType(messages []interface{}, blockType string) bool {
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		content, ok := message["content"].([]interface{})
		if !ok {
			continue
		}
		for _, rawBlock := range content {
			block, ok := rawBlock.(map[string]interface{})
			if ok && block["type"] == blockType {
				return true
			}
		}
	}
	return false
}

func flattenClaudeMessagesForTest(messages []interface{}) string {
	parts := make([]string, 0, len(messages))
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			continue
		}
		parts = append(parts, flattenTextForTest(message["content"]))
	}
	return strings.Join(parts, "\n")
}

func geminiPayloadHasPart(contents []interface{}, key string) bool {
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			continue
		}
		parts, ok := content["parts"].([]interface{})
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]interface{})
			if ok && part[key] != nil {
				return true
			}
		}
	}
	return false
}

func flattenGeminiContentsForTest(contents []interface{}) string {
	parts := make([]string, 0, len(contents))
	for _, rawContent := range contents {
		content, ok := rawContent.(map[string]interface{})
		if !ok {
			continue
		}
		parts = append(parts, flattenTextForTest(content["parts"]))
	}
	return strings.Join(parts, "\n")
}

func extractOpenAIToolNames(value interface{}) []string {
	rawTools, ok := value.([]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		if function, ok := tool["function"].(map[string]interface{}); ok {
			if name, _ := function["name"].(string); name != "" {
				names = append(names, name)
				continue
			}
		}
		if name, _ := tool["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func extractClaudeToolNames(value interface{}) []string {
	rawTools, ok := value.([]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0, len(rawTools))
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := tool["name"].(string); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func extractGeminiToolNames(value interface{}) []string {
	rawTools, ok := value.([]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0)
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		rawDecls, ok := tool["functionDeclarations"].([]interface{})
		if !ok {
			continue
		}
		for _, rawDecl := range rawDecls {
			decl, ok := rawDecl.(map[string]interface{})
			if !ok {
				continue
			}
			if name, _ := decl["name"].(string); name != "" {
				names = append(names, name)
			}
		}
	}
	return names
}

func assertOpenAIShellToolSchema(t *testing.T, value interface{}) {
	t.Helper()
	rawTools, ok := value.([]interface{})
	if !ok {
		t.Fatalf("expected openai tools array, got %#v", value)
	}
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		function, ok := tool["function"].(map[string]interface{})
		if !ok || function["name"] != "Shell" {
			continue
		}
		parameters, ok := function["parameters"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected Shell parameters schema, got %#v", function["parameters"])
		}
		properties, ok := parameters["properties"].(map[string]interface{})
		if !ok || properties["command"] == nil {
			t.Fatalf("expected Shell parameters to preserve command property, got %#v", parameters)
		}
		return
	}
	t.Fatalf("expected Shell tool in transformed openai payload")
}

func assertClaudeShellToolSchema(t *testing.T, value interface{}) {
	t.Helper()
	rawTools, ok := value.([]interface{})
	if !ok {
		t.Fatalf("expected claude tools array, got %#v", value)
	}
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok || tool["name"] != "Shell" {
			continue
		}
		schema, ok := tool["input_schema"].(map[string]interface{})
		if !ok {
			t.Fatalf("expected Shell input_schema, got %#v", tool["input_schema"])
		}
		properties, ok := schema["properties"].(map[string]interface{})
		if !ok || properties["command"] == nil {
			t.Fatalf("expected Shell input_schema to preserve command property, got %#v", schema)
		}
		return
	}
	t.Fatalf("expected Shell tool in transformed claude payload")
}

func assertGeminiShellToolSchema(t *testing.T, value interface{}) {
	t.Helper()
	rawTools, ok := value.([]interface{})
	if !ok {
		t.Fatalf("expected gemini tools array, got %#v", value)
	}
	for _, rawTool := range rawTools {
		tool, ok := rawTool.(map[string]interface{})
		if !ok {
			continue
		}
		rawDecls, ok := tool["functionDeclarations"].([]interface{})
		if !ok {
			continue
		}
		for _, rawDecl := range rawDecls {
			decl, ok := rawDecl.(map[string]interface{})
			if !ok || decl["name"] != "Shell" {
				continue
			}
			parameters, ok := decl["parameters"].(map[string]interface{})
			if !ok {
				t.Fatalf("expected Shell parameters schema, got %#v", decl["parameters"])
			}
			properties, ok := parameters["properties"].(map[string]interface{})
			if !ok || properties["command"] == nil {
				t.Fatalf("expected Shell parameters to preserve command property, got %#v", parameters)
			}
			return
		}
	}
	t.Fatalf("expected Shell tool in transformed gemini payload")
}
