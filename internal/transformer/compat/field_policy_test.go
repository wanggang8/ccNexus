package compat

import "testing"

func TestApplyOpenAIFieldPolicy_PreservesThinkingFields(t *testing.T) {
	dst := map[string]interface{}{}
	src := map[string]interface{}{
		"metadata":        map[string]interface{}{"source": "cursor"},
		"stream_options":  map[string]interface{}{"include_usage": true},
		"user":            "user-123",
		"include":         []interface{}{"reasoning.encrypted_content"},
		"store":           true,
		"reasoning":       map[string]interface{}{"effort": "medium"},
		"thinking":        map[string]interface{}{"type": "enabled", "budget_tokens": 2048},
		"enable_thinking": true,
		"budget_tokens":   2048,
	}

	ApplyOpenAIFieldPolicy(dst, src)

	if _, ok := dst["metadata"]; !ok {
		t.Fatalf("expected metadata preserved")
	}
	if _, ok := dst["stream_options"]; !ok {
		t.Fatalf("expected stream_options preserved")
	}
	if dst["user"] != "user-123" {
		t.Fatalf("expected user preserved, got %#v", dst["user"])
	}
	if _, ok := dst["include"]; ok {
		t.Fatalf("expected include to be dropped for openai target, got %#v", dst["include"])
	}
	if _, ok := dst["store"]; ok {
		t.Fatalf("expected store to stay dropped for openai target, got %#v", dst["store"])
	}
	thinking, ok := dst["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking preserved, got %#v", dst["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking type preserved, got %#v", thinking["type"])
	}
	if dst["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking preserved, got %#v", dst["enable_thinking"])
	}
	if dst["budget_tokens"] != 2048 {
		t.Fatalf("expected budget_tokens preserved, got %#v", dst["budget_tokens"])
	}
	if dst["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning.effort mapped to reasoning_effort, got %#v", dst["reasoning_effort"])
	}
}

func TestApplyClaudeFieldPolicy_OnlyMapsClaudeSemanticFields(t *testing.T) {
	dst := map[string]interface{}{}
	src := map[string]interface{}{
		"metadata":       map[string]interface{}{"source": "cursor"},
		"instructions":   "You are helpful.",
		"stream_options": map[string]interface{}{"include_usage": true},
		"include":        []interface{}{"reasoning.encrypted_content"},
		"user":           "user-123",
		"store":          true,
		"reasoning":      map[string]interface{}{"effort": "high"},
	}

	ApplyClaudeFieldPolicy(dst, src)

	if _, ok := dst["metadata"]; !ok {
		t.Fatalf("expected metadata preserved")
	}
	if dst["system"] != "You are helpful." {
		t.Fatalf("expected instructions mapped to system, got %#v", dst["system"])
	}
	thinking, ok := dst["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected reasoning mapped to thinking, got %#v", dst["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking enabled, got %#v", thinking)
	}
	if _, ok := dst["stream_options"]; ok {
		t.Fatalf("expected stream_options dropped for claude target, got %#v", dst["stream_options"])
	}
	if _, ok := dst["include"]; ok {
		t.Fatalf("expected include dropped for claude target, got %#v", dst["include"])
	}
	if _, ok := dst["user"]; ok {
		t.Fatalf("expected user dropped for claude target, got %#v", dst["user"])
	}
	if _, ok := dst["store"]; ok {
		t.Fatalf("expected store dropped for claude target, got %#v", dst["store"])
	}
}

func TestApplyResponsesFieldPolicy_PreservesThinkingFields(t *testing.T) {
	dst := map[string]interface{}{}
	src := map[string]interface{}{
		"metadata":        map[string]interface{}{"source": "cursor"},
		"stream_options":  map[string]interface{}{"include_usage": true},
		"include":         []interface{}{"reasoning.encrypted_content"},
		"user":            "user-123",
		"reasoning_effort": "medium",
		"store":           true,
		"thinking":        map[string]interface{}{"type": "enabled", "budget_tokens": 2048},
		"enable_thinking": true,
		"budget_tokens":   2048,
	}

	ApplyResponsesFieldPolicy(dst, src)

	if _, ok := dst["metadata"]; !ok {
		t.Fatalf("expected metadata preserved")
	}
	if _, ok := dst["stream_options"]; ok {
		t.Fatalf("expected stream_options dropped for responses target, got %#v", dst["stream_options"])
	}
	if _, ok := dst["include"]; !ok {
		t.Fatalf("expected include preserved for responses target")
	}
	if _, ok := dst["store"]; !ok {
		t.Fatalf("expected store preserved for responses target")
	}
	thinking, ok := dst["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected thinking preserved, got %#v", dst["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("expected thinking type preserved, got %#v", thinking["type"])
	}
	if dst["enable_thinking"] != true {
		t.Fatalf("expected enable_thinking preserved, got %#v", dst["enable_thinking"])
	}
	if dst["budget_tokens"] != 2048 {
		t.Fatalf("expected budget_tokens preserved, got %#v", dst["budget_tokens"])
	}
	if dst["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort preserved, got %#v", dst["reasoning_effort"])
	}
}
