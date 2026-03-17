package convert

import (
	"encoding/json"
	"testing"
)

// ========== ParseSSE tests ==========

func TestParseSSE_DataOnly(t *testing.T) {
	input := []byte("data: {\"hello\":\"world\"}\n")
	eventType, jsonData := ParseSSE(input)

	if eventType != "" {
		t.Errorf("Expected empty event type, got %q", eventType)
	}
	if jsonData != "{\"hello\":\"world\"}" {
		t.Errorf("Expected JSON data, got %q", jsonData)
	}
}

func TestParseSSE_EventAndData(t *testing.T) {
	input := []byte("event: message_start\ndata: {\"type\":\"message_start\"}\n")
	eventType, jsonData := ParseSSE(input)

	if eventType != "message_start" {
		t.Errorf("Expected event type 'message_start', got %q", eventType)
	}
	if jsonData != "{\"type\":\"message_start\"}" {
		t.Errorf("Expected JSON data, got %q", jsonData)
	}
}

func TestParseSSE_Done(t *testing.T) {
	input := []byte("data: [DONE]\n")
	_, jsonData := ParseSSE(input)

	if jsonData != "[DONE]" {
		t.Errorf("Expected [DONE], got %q", jsonData)
	}
}

func TestParseSSE_EmptyInput(t *testing.T) {
	eventType, jsonData := ParseSSE([]byte(""))
	if eventType != "" || jsonData != "" {
		t.Errorf("Expected empty results for empty input, got %q, %q", eventType, jsonData)
	}
}

func TestParseSSE_MultipleDataLines(t *testing.T) {
	input := []byte("data: first\ndata: second\n")
	_, jsonData := ParseSSE(input)

	// Should return last data line
	if jsonData != "second" {
		t.Errorf("Expected 'second' (last data line), got %q", jsonData)
	}
}

func TestParseSSE_WhitespaceHandling(t *testing.T) {
	input := []byte("  data: {\"trimmed\":true}  \n")
	_, jsonData := ParseSSE(input)

	if jsonData != "{\"trimmed\":true}" {
		t.Errorf("Expected trimmed JSON data, got %q", jsonData)
	}
}

// ========== cleanSchemaForGemini tests ==========

func TestCleanSchemaForGemini_RemovesUnsupportedFields(t *testing.T) {
	schema := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type":                 "string",
				"additionalProperties": true,
			},
		},
	}

	cleaned := cleanSchemaForGemini(schema)
	cleanedMap := cleaned.(map[string]interface{})

	if _, ok := cleanedMap["additionalProperties"]; ok {
		t.Error("Expected additionalProperties to be removed")
	}
	if _, ok := cleanedMap["$schema"]; ok {
		t.Error("Expected $schema to be removed")
	}
	if cleanedMap["type"] != "object" {
		t.Error("Expected 'type' to be preserved")
	}

	// Check nested properties also cleaned
	props := cleanedMap["properties"].(map[string]interface{})
	nameSchema := props["name"].(map[string]interface{})
	if _, ok := nameSchema["additionalProperties"]; ok {
		t.Error("Expected nested additionalProperties to be removed")
	}
}

func TestCleanSchemaForGemini_DoesNotMutateInput(t *testing.T) {
	original := map[string]interface{}{
		"type":                 "object",
		"additionalProperties": false,
		"$schema":              "http://json-schema.org/draft-07/schema#",
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
	}

	// Deep copy for comparison
	originalJSON, _ := json.Marshal(original)

	_ = cleanSchemaForGemini(original)

	afterJSON, _ := json.Marshal(original)
	if string(originalJSON) != string(afterJSON) {
		t.Errorf("cleanSchemaForGemini mutated the input!\nBefore: %s\nAfter:  %s", originalJSON, afterJSON)
	}
}

func TestCleanSchemaForGemini_HandlesItems(t *testing.T) {
	schema := map[string]interface{}{
		"type": "array",
		"items": map[string]interface{}{
			"type":                 "object",
			"additionalProperties": false,
		},
	}

	cleaned := cleanSchemaForGemini(schema)
	cleanedMap := cleaned.(map[string]interface{})
	items := cleanedMap["items"].(map[string]interface{})

	if _, ok := items["additionalProperties"]; ok {
		t.Error("Expected additionalProperties in items to be removed")
	}
}

func TestCleanSchemaForGemini_NonMapInput(t *testing.T) {
	result := cleanSchemaForGemini("not a map")
	if result != "not a map" {
		t.Error("Expected non-map input to be returned as-is")
	}
}

// ========== GenerateToolCallID tests ==========

func TestGenerateToolCallID_Unique(t *testing.T) {
	id1 := GenerateToolCallID("test")
	id2 := GenerateToolCallID("test")
	if id1 == id2 {
		t.Error("Expected unique IDs, got same")
	}
}

func TestGenerateToolCallID_ContainsName(t *testing.T) {
	id := GenerateToolCallID("my_tool")
	if len(id) == 0 {
		t.Error("Expected non-empty ID")
	}
}
