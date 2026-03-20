package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

// ========== Multi-tool-call streaming tests ==========

func TestOpenAIStreamToOpenAI2_MultipleToolCalls(t *testing.T) {
	ctx := transformer.NewStreamContext()

	// Step 1: Initial message
	init := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"role":"assistant","content":null},"finish_reason":null}]}`
	_, err := OpenAIStreamToOpenAI2([]byte(init), ctx)
	if err != nil {
		t.Fatalf("Init chunk failed: %v", err)
	}

	// Step 2: First tool call (index=0)
	idx0 := 0
	tc1 := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"read_file","arguments":""}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc1), ctx)
	if err != nil {
		t.Fatalf("Tool call 1 init failed: %v", err)
	}

	// Step 3: First tool call arguments
	tc1args := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"/a\"}"}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc1args), ctx)
	if err != nil {
		t.Fatalf("Tool call 1 args failed: %v", err)
	}

	// Step 4: Second tool call (index=1)
	idx1 := 1
	tc2 := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_def","type":"function","function":{"name":"write_file","arguments":""}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc2), ctx)
	if err != nil {
		t.Fatalf("Tool call 2 init failed: %v", err)
	}

	// Step 5: Second tool call arguments
	tc2args := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"/b\",\"content\":\"hello\"}"}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc2args), ctx)
	if err != nil {
		t.Fatalf("Tool call 2 args failed: %v", err)
	}

	// Step 6: Third tool call (index=2)
	tc3 := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"id":"call_ghi","type":"function","function":{"name":"list_dir","arguments":""}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc3), ctx)
	if err != nil {
		t.Fatalf("Tool call 3 init failed: %v", err)
	}

	tc3args := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":2,"function":{"arguments":"{\"dir\":\"/\"}"}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc3args), ctx)
	if err != nil {
		t.Fatalf("Tool call 3 args failed: %v", err)
	}

	// Step 7: Finish
	fr := "tool_calls"
	finish := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	result, err := OpenAIStreamToOpenAI2([]byte(finish), ctx)
	if err != nil {
		t.Fatalf("Finish chunk failed: %v", err)
	}

	resultStr := string(result)

	// Verify ALL 3 tool calls are completed
	if !strings.Contains(resultStr, "call_abc") {
		t.Error("Expected call_abc in result")
	}
	if !strings.Contains(resultStr, "call_def") {
		t.Error("Expected call_def in result")
	}
	if !strings.Contains(resultStr, "call_ghi") {
		t.Error("Expected call_ghi in result")
	}

	// Count arguments.done events - should be 3
	argsDoneCount := strings.Count(resultStr, "response.function_call_arguments.done")
	if argsDoneCount != 3 {
		t.Errorf("Expected 3 arguments.done events, got %d", argsDoneCount)
	}

	// Count output_item.done events for tool calls - should be 3
	itemDoneCount := strings.Count(resultStr, "response.output_item.done")
	if itemDoneCount != 3 {
		t.Errorf("Expected 3 output_item.done events, got %d", itemDoneCount)
	}

	// Verify response.completed present
	if !strings.Contains(resultStr, "response.completed") {
		t.Error("Expected response.completed event")
	}

	// Verify ActiveToolCalls state
	if len(ctx.ActiveToolCalls) != 3 {
		t.Errorf("Expected 3 ActiveToolCalls, got %d", len(ctx.ActiveToolCalls))
	}

	// Verify each tool call's arguments
	for _, atc := range ctx.ActiveToolCalls {
		switch atc.ID {
		case "call_abc":
			if atc.Name != "read_file" || atc.Arguments != "{\"path\":\"/a\"}" {
				t.Errorf("call_abc: unexpected name=%q args=%q", atc.Name, atc.Arguments)
			}
		case "call_def":
			if atc.Name != "write_file" || atc.Arguments != "{\"path\":\"/b\",\"content\":\"hello\"}" {
				t.Errorf("call_def: unexpected name=%q args=%q", atc.Name, atc.Arguments)
			}
		case "call_ghi":
			if atc.Name != "list_dir" || atc.Arguments != "{\"dir\":\"/\"}" {
				t.Errorf("call_ghi: unexpected name=%q args=%q", atc.Name, atc.Arguments)
			}
		}
	}

	// Suppress unused variable warnings
	_ = idx0
	_ = idx1
	_ = fr
}

func TestOpenAIStreamToOpenAI2_TextThenToolCall(t *testing.T) {
	ctx := transformer.NewStreamContext()

	// Step 1: Text content
	text := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Let me help."},"finish_reason":null}]}`
	result1, err := OpenAIStreamToOpenAI2([]byte(text), ctx)
	if err != nil {
		t.Fatalf("Text chunk failed: %v", err)
	}
	if !strings.Contains(string(result1), "response.output_text.delta") {
		t.Error("Expected text delta event")
	}

	// Step 2: Tool call after text
	tc := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":"{\"q\":\"test\"}"}}]},"finish_reason":null}]}`
	_, err = OpenAIStreamToOpenAI2([]byte(tc), ctx)
	if err != nil {
		t.Fatalf("Tool call chunk failed: %v", err)
	}

	// Step 3: Finish
	finish := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`
	result3, err := OpenAIStreamToOpenAI2([]byte(finish), ctx)
	if err != nil {
		t.Fatalf("Finish chunk failed: %v", err)
	}

	resultStr := string(result3)

	// Should close text block AND complete tool call
	if !strings.Contains(resultStr, "response.output_text.done") {
		t.Error("Expected output_text.done event")
	}
	if !strings.Contains(resultStr, "response.function_call_arguments.done") {
		t.Error("Expected function_call_arguments.done event")
	}
	if !strings.Contains(resultStr, "response.completed") {
		t.Error("Expected response.completed event")
	}
}

func TestOpenAIStreamToOpenAI2_ErrorResponse(t *testing.T) {
	ctx := transformer.NewStreamContext()

	errSSE := `data: {"error":{"message":"Rate limit exceeded","type":"rate_limit_error"}}`
	_, err := OpenAIStreamToOpenAI2([]byte(errSSE), ctx)
	if err == nil {
		t.Error("Expected error for error response")
	}
	if !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Errorf("Expected rate limit error message, got: %v", err)
	}
}

func TestOpenAIStreamToOpenAI2_InvalidJSON(t *testing.T) {
	ctx := transformer.NewStreamContext()

	invalidSSE := `data: {not valid json`
	result, err := OpenAIStreamToOpenAI2([]byte(invalidSSE), ctx)
	if err != nil {
		t.Fatalf("Should not return error for invalid JSON, got: %v", err)
	}
	if result != nil {
		t.Error("Expected nil result for invalid JSON")
	}
}

func TestOpenAIStreamToOpenAI2_UsageInCompletion(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageStartSent = true
	ctx.MessageID = "chatcmpl-1"
	ctx.ContentBlockStarted = true
	ctx.ContentText = "Hello"
	ctx.InputTokens = 10
	ctx.OutputTokens = 5

	finish := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
	result, err := OpenAIStreamToOpenAI2([]byte(finish), ctx)
	if err != nil {
		t.Fatalf("Finish chunk failed: %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "\"input_tokens\":10") {
		t.Error("Expected input_tokens=10 in completed response")
	}
	if !strings.Contains(resultStr, "\"output_tokens\":5") {
		t.Error("Expected output_tokens=5 in completed response")
	}
}

func TestOpenAIStreamToOpenAI2_PreservesAuthoritativeTotalTokens(t *testing.T) {
	ctx := transformer.NewStreamContext()

	chunk := `data: {"id":"chatcmpl-1","object":"chat.completion.chunk","model":"gpt-4","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":99}}`
	result, err := OpenAIStreamToOpenAI2([]byte(chunk), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}

	if !strings.Contains(string(result), `"total_tokens":99`) {
		t.Fatalf("expected authoritative total_tokens=99, got: %s", string(result))
	}
}

// ========== OpenAI2StreamToOpenAI multi-tool tests ==========

func TestOpenAI2StreamToOpenAI_MultipleToolCalls(t *testing.T) {
	ctx := transformer.NewStreamContext()

	// Created event
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.created","response":{"id":"resp_1"}}`), ctx, "gpt-4")

	// Tool 1
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_1","name":"read_file"}}`), ctx, "gpt-4")
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"/a\"}"}`), ctx, "gpt-4")
	out1, err := OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_1","name":"read_file","arguments":"{\"path\":\"/a\"}"}}`), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("Tool 1 done failed: %v", err)
	}

	// Verify first tool call produces an OpenAI chunk
	if out1 == nil {
		t.Fatal("Expected output for tool 1 done event")
	}
	_, jsonStr := ParseSSE(out1)
	var chunk1 map[string]interface{}
	json.Unmarshal([]byte(jsonStr), &chunk1)
	choices1 := chunk1["choices"].([]interface{})
	delta1 := choices1[0].(map[string]interface{})["delta"].(map[string]interface{})
	toolCalls1 := delta1["tool_calls"].([]interface{})
	tc1 := toolCalls1[0].(map[string]interface{})
	if tc1["index"] != float64(0) {
		t.Errorf("Expected first tool call index 0, got %v", tc1["index"])
	}

	// Tool 2
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.added","item":{"type":"function_call","call_id":"call_2","name":"write_file"}}`), ctx, "gpt-4")
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.function_call_arguments.delta","delta":"{\"path\":\"/b\"}"}`), ctx, "gpt-4")
	out2, _ := OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_2","name":"write_file","arguments":"{\"path\":\"/b\"}"}}`), ctx, "gpt-4")
	if out2 == nil {
		t.Fatal("Expected output for tool 2 done event")
	}

	// Completed
	completed, err := OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":20,"output_tokens":10,"total_tokens":30}}}`), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("Completed event failed: %v", err)
	}

	_, completedJSON := ParseSSE(completed)
	var completedChunk map[string]interface{}
	json.Unmarshal([]byte(completedJSON), &completedChunk)

	choices := completedChunk["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("Expected finish_reason 'tool_calls', got %v", choice["finish_reason"])
	}
}

func TestOpenAI2StreamToOpenAI_UsesDoneArgumentsForFinalToolCall(t *testing.T) {
	ctx := transformer.NewStreamContext()

	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.created","response":{"id":"resp_done"}}`), ctx, "gpt-4")
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_done","name":"read_file"}}`), ctx, "gpt-4")
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":"}`), ctx, "gpt-4")
	_, _ = OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.function_call_arguments.done","output_index":2,"arguments":"{\"path\":\"/tmp/a\"}"}`), ctx, "gpt-4")

	out, err := OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","call_id":"call_done","name":"read_file"}}`), ctx, "gpt-4")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected output for output_item.done")
	}
	if !strings.Contains(string(out), `{\"path\":\"/tmp/a\"}`) {
		t.Fatalf("expected final arguments from done event, got: %s", string(out))
	}
}

// ========== Tool choice mapping tests ==========

func TestMapOpenAIToolChoiceToOpenAI2_String(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected interface{}
	}{
		{"auto", "auto"},
		{"none", "none"},
		{"required", "required"},
		{nil, nil},
	}

	for _, tt := range tests {
		result := mapOpenAIToolChoiceToOpenAI2(tt.input)
		if result != tt.expected {
			t.Errorf("mapOpenAIToolChoiceToOpenAI2(%v) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestMapOpenAIToolChoiceToOpenAI2_FunctionObject(t *testing.T) {
	input := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": "my_func",
		},
	}

	result := mapOpenAIToolChoiceToOpenAI2(input)
	resultMap := result.(map[string]interface{})

	if resultMap["type"] != "function" {
		t.Errorf("Expected type 'function', got %v", resultMap["type"])
	}
	if resultMap["name"] != "my_func" {
		t.Errorf("Expected name 'my_func', got %v", resultMap["name"])
	}
}

func TestMapOpenAIToolChoiceToOpenAI2_NonFunctionType(t *testing.T) {
	input := map[string]interface{}{
		"type": "something_else",
	}
	result := mapOpenAIToolChoiceToOpenAI2(input)
	if result != nil {
		t.Errorf("Expected nil for non-function type, got %v", result)
	}
}

func TestMapOpenAI2ToolChoiceToOpenAI_FunctionObject(t *testing.T) {
	input := map[string]interface{}{
		"type": "function",
		"name": "my_func",
	}

	result := mapOpenAI2ToolChoiceToOpenAI(input)
	resultMap := result.(map[string]interface{})

	if resultMap["type"] != "function" {
		t.Errorf("Expected type 'function', got %v", resultMap["type"])
	}

	fn := resultMap["function"].(map[string]string)
	if fn["name"] != "my_func" {
		t.Errorf("Expected function name 'my_func', got %v", fn["name"])
	}
}

func TestMapOpenAI2ToolChoiceToOpenAI_String(t *testing.T) {
	result := mapOpenAI2ToolChoiceToOpenAI("auto")
	if result != "auto" {
		t.Errorf("Expected 'auto', got %v", result)
	}
}

func TestMapOpenAI2ToolChoiceToOpenAI_Nil(t *testing.T) {
	result := mapOpenAI2ToolChoiceToOpenAI(nil)
	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}
}

// ========== Request conversion with tool calls in context ==========

func TestOpenAIReqToOpenAI2_AssistantToolCalls(t *testing.T) {
	req := `{
		"model":"gpt-4",
		"messages":[
			{"role":"user","content":"Help me"},
			{"role":"assistant","content":"","tool_calls":[
				{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"/a\"}"}},
				{"id":"call_2","type":"function","function":{"name":"write","arguments":"{\"path\":\"/b\"}"}}
			]},
			{"role":"tool","content":"file A contents","tool_call_id":"call_1"},
			{"role":"tool","content":"ok","tool_call_id":"call_2"}
		]
	}`

	result, err := OpenAIReqToOpenAI2([]byte(req), "gpt-4o")
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	var resp map[string]interface{}
	json.Unmarshal(result, &resp)

	input := resp["input"].([]interface{})
	// user message, 2 function_calls, 2 function_call_outputs = 5 items
	if len(input) != 5 {
		t.Fatalf("Expected 5 input items, got %d", len(input))
	}

	// Verify function_call items
	fc1 := input[1].(map[string]interface{})
	if fc1["type"] != "function_call" || fc1["call_id"] != "call_1" {
		t.Errorf("Expected function_call with call_1, got %v", fc1)
	}

	fc2 := input[2].(map[string]interface{})
	if fc2["type"] != "function_call" || fc2["call_id"] != "call_2" {
		t.Errorf("Expected function_call with call_2, got %v", fc2)
	}

	// Verify function_call_output items
	fco1 := input[3].(map[string]interface{})
	if fco1["type"] != "function_call_output" || fco1["call_id"] != "call_1" {
		t.Errorf("Expected function_call_output with call_1, got %v", fco1)
	}
}

func TestOpenAI2ReqToOpenAI_MultipleFunctionCalls(t *testing.T) {
	req := `{
		"model":"gpt-4o",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"Do both"}]},
			{"type":"function_call","call_id":"c1","name":"read","arguments":"{\"p\":1}"},
			{"type":"function_call","call_id":"c2","name":"write","arguments":"{\"p\":2}"},
			{"type":"function_call_output","call_id":"c1","output":"result1"},
			{"type":"function_call_output","call_id":"c2","output":"result2"}
		]
	}`

	result, err := OpenAI2ReqToOpenAI([]byte(req), "gpt-4")
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	var openaiReq transformer.OpenAIRequest
	json.Unmarshal(result, &openaiReq)

	// user, assistant(2 tool_calls), tool(c1), tool(c2) = 4 messages
	if len(openaiReq.Messages) != 4 {
		t.Fatalf("Expected 4 messages, got %d", len(openaiReq.Messages))
	}

	// Assistant message should have 2 tool calls
	assistantMsg := openaiReq.Messages[1]
	if len(assistantMsg.ToolCalls) != 2 {
		t.Errorf("Expected 2 tool calls in assistant message, got %d", len(assistantMsg.ToolCalls))
	}
}

// ========== Response conversion with multiple tool calls ==========

func TestOpenAIRespToOpenAI2_MultipleToolCalls(t *testing.T) {
	resp := `{
		"id":"chatcmpl-1",
		"object":"chat.completion",
		"model":"gpt-4",
		"choices":[{
			"index":0,
			"message":{
				"role":"assistant",
				"content":"Let me help.",
				"tool_calls":[
					{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"p\":1}"}},
					{"id":"call_2","type":"function","function":{"name":"write","arguments":"{\"p\":2}"}}
				]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}
	}`

	result, err := OpenAIRespToOpenAI2([]byte(resp))
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	var openai2Resp map[string]interface{}
	json.Unmarshal(result, &openai2Resp)

	output := openai2Resp["output"].([]interface{})
	// message + 2 function_calls = 3
	if len(output) != 3 {
		t.Fatalf("Expected 3 output items, got %d", len(output))
	}

	// First should be message with text
	msg := output[0].(map[string]interface{})
	if msg["type"] != "message" {
		t.Errorf("Expected first output type 'message', got %v", msg["type"])
	}

	// Second and third should be function_calls
	for i := 1; i <= 2; i++ {
		fc := output[i].(map[string]interface{})
		if fc["type"] != "function_call" {
			t.Errorf("Output[%d]: expected function_call, got %v", i, fc["type"])
		}
	}
}

func TestOpenAI2RespToOpenAI_MultipleToolCalls(t *testing.T) {
	resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Sure"}]},
			{"type":"function_call","call_id":"c1","name":"read","arguments":"{\"p\":1}"},
			{"type":"function_call","call_id":"c2","name":"write","arguments":"{\"p\":2}"}
		],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
	}`

	result, err := OpenAI2RespToOpenAI([]byte(resp), "gpt-4")
	if err != nil {
		t.Fatalf("Failed: %v", err)
	}

	var openaiResp map[string]interface{}
	json.Unmarshal(result, &openaiResp)

	choices := openaiResp["choices"].([]interface{})
	choice := choices[0].(map[string]interface{})

	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("Expected finish_reason 'tool_calls', got %v", choice["finish_reason"])
	}

	message := choice["message"].(map[string]interface{})
	toolCalls := message["tool_calls"].([]interface{})
	if len(toolCalls) != 2 {
		t.Fatalf("Expected 2 tool_calls, got %d", len(toolCalls))
	}

	if message["content"] != "Sure" {
		t.Errorf("Expected content 'Sure', got %v", message["content"])
	}
}
