package proxy

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/config"
)

func TestCursorChatOpenAIRealSSEPreservesToolCallSemantics(t *testing.T) {
	original := loadRealSSESection(t, "sse.log", "原始：", "转换后：")
	if strings.TrimSpace(original) == "" {
		t.Fatal("expected original SSE fixture section")
	}

	prepared := prepareCursorRoundTrip(
		t,
		"/cursor/v1/chat/completions",
		`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"stream":true}`,
		config.Endpoint{Name: "openai", Transformer: "openai", Model: "gpt-5.4"},
	)

	events := splitFixtureSSEEvents(original)
	if len(events) == 0 {
		t.Fatal("expected SSE events in original fixture")
	}

	transformed := runCursorRoundTripStream(t, prepared, events...)

	originalTool := accumulateToolCallFromSSE(t, original)
	transformedTool := accumulateToolCallFromSSE(t, transformed)

	if transformedTool.ID != originalTool.ID {
		t.Fatalf("expected tool call id preserved, want %q got %q", originalTool.ID, transformedTool.ID)
	}
	if transformedTool.Name != originalTool.Name {
		t.Fatalf("expected tool name preserved, want %q got %q", originalTool.Name, transformedTool.Name)
	}
	if transformedTool.Arguments != originalTool.Arguments {
		t.Fatalf("expected tool arguments preserved, want %q got %q", originalTool.Arguments, transformedTool.Arguments)
	}
	if transformedTool.FinishReason != "tool_calls" {
		t.Fatalf("expected tool_calls finish reason, got %q", transformedTool.FinishReason)
	}
	if !transformedTool.DoneSeen {
		t.Fatal("expected transformed SSE to end with [DONE]")
	}
	if prelude := firstContentBeforeToolCall(t, transformed); prelude != "" {
		t.Fatalf("did not expect content prelude before first tool call, got %q in %s", prelude, transformed)
	}
}

func loadRealSSESection(t *testing.T, filename string, startMarker string, endMarker string) string {
	t.Helper()

	path := filepath.Join("..", "..", "docs", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SSE fixture %s failed: %v", path, err)
	}
	text := string(data)

	start := strings.Index(text, startMarker)
	if start < 0 {
		t.Fatalf("fixture %s missing start marker %q", path, startMarker)
	}
	start += len(startMarker)

	end := len(text)
	if endMarker != "" {
		if idx := strings.Index(text[start:], endMarker); idx >= 0 {
			end = start + idx
		}
	}

	return strings.TrimSpace(text[start:end])
}

func splitFixtureSSEEvents(section string) []string {
	chunks := strings.Split(strings.ReplaceAll(strings.TrimSpace(section), "\r\n", "\n"), "\n\n")
	events := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		events = append(events, chunk+"\n\n")
	}
	return events
}

type accumulatedToolCall struct {
	ID           string
	Name         string
	Arguments    string
	FinishReason string
	DoneSeen     bool
}

func accumulateToolCallFromSSE(t *testing.T, stream string) accumulatedToolCall {
	t.Helper()

	var result accumulatedToolCall
	scanner := bufio.NewScanner(strings.NewReader(stream))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if line == "data: [DONE]" {
			result.DoneSeen = true
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Fatalf("decode SSE payload failed: %v", err)
		}

		choices, ok := payload["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}
		if finishReason, _ := choice["finish_reason"].(string); finishReason != "" {
			result.FinishReason = finishReason
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		toolCalls, ok := delta["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}
		toolCall, ok := toolCalls[0].(map[string]interface{})
		if !ok {
			continue
		}
		if id, _ := toolCall["id"].(string); id != "" {
			result.ID = id
		}
		function, ok := toolCall["function"].(map[string]interface{})
		if !ok {
			continue
		}
		if name, _ := function["name"].(string); name != "" {
			result.Name = name
		}
		if arguments, _ := function["arguments"].(string); arguments != "" {
			result.Arguments += arguments
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE stream failed: %v", err)
	}
	if result.ID == "" || result.Name == "" || result.Arguments == "" {
		t.Fatalf("expected accumulated tool call, got %#v", result)
	}
	return result
}

func firstContentBeforeToolCall(t *testing.T, stream string) string {
	t.Helper()

	scanner := bufio.NewScanner(strings.NewReader(stream))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data: ") || line == "data: [DONE]" {
			continue
		}

		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
			t.Fatalf("decode SSE payload failed: %v", err)
		}

		choices, ok := payload["choices"].([]interface{})
		if !ok || len(choices) == 0 {
			continue
		}
		choice, ok := choices[0].(map[string]interface{})
		if !ok {
			continue
		}
		delta, ok := choice["delta"].(map[string]interface{})
		if !ok {
			continue
		}
		if toolCalls, ok := delta["tool_calls"].([]interface{}); ok && len(toolCalls) > 0 {
			return ""
		}
		if content, _ := delta["content"].(string); content != "" {
			return content
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan SSE stream failed: %v", err)
	}
	return ""
}
