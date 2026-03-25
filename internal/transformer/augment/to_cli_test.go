package augment

import (
	"encoding/json"
	"testing"
)

func TestToCliRequest(t *testing.T) {
	// Test basic CLI request conversion
	stream := true
	ar := &AugmentRequest{
		Model:               "claude-sonnet-4-5-20250929",
		MaxTokens:           4096,
		Stream:              &stream,
		Message:             "Hello",
		WorkspaceGuidelines: "Follow workspace conventions.",
		UserGuidelines:      "You are a helpful assistant",
		Context: &ContextBlock{
			Path: "internal/transformer/augment/to_cli.go",
			Lang: "go",
		},
	}

	result, err := toCliRequest(ar)
	if err != nil {
		t.Fatalf("toCliRequest failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(result, &req); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Verify basic fields
	if req["model"] != "claude-sonnet-4-5-20250929" {
		t.Errorf("Expected model claude-sonnet-4-5-20250929, got %v", req["model"])
	}

	if req["stream"] != true {
		t.Errorf("Expected stream true, got %v", req["stream"])
	}

	// Verify metadata exists
	metadata, ok := req["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("metadata field missing or wrong type")
	}

	userID, ok := metadata["user_id"].(string)
	if !ok || userID == "" {
		t.Errorf("user_id missing or empty in metadata")
	}

	// Verify system prompt contains CLI-specific text
	system, ok := req["system"].([]interface{})
	if !ok || len(system) == 0 {
		t.Fatal("system field missing or empty")
	}

	firstBlock, ok := system[0].(map[string]interface{})
	if !ok {
		t.Fatal("first system block is not a map")
	}

	text, ok := firstBlock["text"].(string)
	if !ok {
		t.Fatal("system text missing")
	}

	if text != "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK." {
		t.Errorf("Unexpected system prompt: %s", text)
	}

	if len(system) < 2 {
		t.Fatal("expected CLI common system block")
	}
	commonText, ok := system[1].(map[string]interface{})["text"].(string)
	if !ok {
		t.Fatal("common system text missing")
	}
	if commonText != "Follow workspace conventions.\n\nYou are a helpful assistant\n\n[context]\npath=internal/transformer/augment/to_cli.go\nlang=go" {
		t.Errorf("unexpected common system prompt: %s", commonText)
	}

	t.Logf("CLI request generated successfully with user_id: %s", userID)
}

func TestCliMetadataStability(t *testing.T) {
	// Test that CLI metadata is stable across multiple calls
	ar := &AugmentRequest{
		Model:   "claude-sonnet-4-5-20250929",
		Message: "Test",
	}

	result1, _ := toCliRequest(ar)
	result2, _ := toCliRequest(ar)

	var req1, req2 map[string]interface{}
	json.Unmarshal(result1, &req1)
	json.Unmarshal(result2, &req2)

	meta1 := req1["metadata"].(map[string]interface{})
	meta2 := req2["metadata"].(map[string]interface{})

	userID1 := meta1["user_id"].(string)
	userID2 := meta2["user_id"].(string)

	if userID1 != userID2 {
		t.Errorf("user_id should be stable across calls, got %s and %s", userID1, userID2)
	}

	t.Logf("CLI metadata is stable: %s", userID1)
}

func TestCliVsClaudeSystemPrompt(t *testing.T) {
	// Compare CLI and Claude system prompts
	ar := &AugmentRequest{
		Model:   "claude-sonnet-4-5-20250929",
		Message: "Test",
	}

	cliResult, _ := toCliRequest(ar)
	claudeResult, _ := toClaudeRequest(ar)

	var cliReq, claudeReq map[string]interface{}
	json.Unmarshal(cliResult, &cliReq)
	json.Unmarshal(claudeResult, &claudeReq)

	// CLI should have system prompt
	cliSystem, cliHasSystem := cliReq["system"].([]interface{})
	if !cliHasSystem || len(cliSystem) == 0 {
		t.Error("CLI request should have system prompt")
	}

	// Claude should not have system prompt (when no user guidelines)
	claudeSystem, claudeHasSystem := claudeReq["system"]
	if claudeHasSystem && claudeSystem != nil {
		t.Error("Claude request should not have system prompt when no user guidelines")
	}

	// CLI should have metadata
	if _, ok := cliReq["metadata"]; !ok {
		t.Error("CLI request should have metadata")
	}

	// Claude should not have metadata
	if _, ok := claudeReq["metadata"]; ok {
		t.Error("Claude request should not have metadata")
	}

	t.Log("CLI and Claude requests have different structures as expected")
}
