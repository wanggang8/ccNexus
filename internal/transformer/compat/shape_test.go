package compat

import "testing"

func TestDetectRequestShape_ClaudeMessagesLike(t *testing.T) {
	req := []byte(`{
		"model": "claude-4.6-opus-high-thinking",
		"system": [{"type": "text", "text": "You are helpful."}],
		"messages": [
			{"role": "user", "content": [{"type": "text", "text": "Hello"}]}
		],
		"tools": [
			{"name": "read_file", "description": "Read a file", "input_schema": {"type": "object"}}
		]
	}`)

	if got := DetectRequestShape(req); got != RequestShapeClaudeMessages {
		t.Fatalf("expected Claude messages shape, got %s", got)
	}
}

func TestDetectRequestShape_OpenAIChatLike(t *testing.T) {
	req := []byte(`{
		"model": "gpt-4o",
		"messages": [
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": "Hello"}
		],
		"tools": [
			{"type": "function", "function": {"name": "read_file", "parameters": {"type": "object"}}}
		]
	}`)

	if got := DetectRequestShape(req); got != RequestShapeOpenAIChat {
		t.Fatalf("expected OpenAI chat shape, got %s", got)
	}
}

func TestDetectRequestShape_OpenAIResponsesLike(t *testing.T) {
	req := []byte(`{
		"model": "gpt-5.4",
		"input": [{"role": "system", "content": "You are GPT-5.4."}],
		"include": ["reasoning.encrypted_content"],
		"reasoning": {"effort": "medium", "summary": "auto"},
		"stream": true
	}`)

	if got := DetectRequestShape(req); got != RequestShapeOpenAIResponses {
		t.Fatalf("expected OpenAI responses shape, got %s", got)
	}
}

func TestDetectRequestShape_PrefersResponsesWhenMessagesAndInputCoexist(t *testing.T) {
	req := []byte(`{
		"model": "gpt-5.4",
		"messages": [{"role": "user", "content": "Hello from chat"}],
		"input": [{"type": "message", "role": "user", "content": [{"type": "input_text", "text": "Hello from responses"}]}],
		"reasoning": {"effort": "medium"}
	}`)

	if got := DetectRequestShape(req); got != RequestShapeOpenAIResponses {
		t.Fatalf("expected mixed body with input/reasoning to prefer responses shape, got %s", got)
	}
}
