package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/lich0821/ccNexus/internal/transformer"
)

func TestOpenAIReqToOpenAI2DefaultsToolChoiceAutoWhenToolsPresent(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"stream":true,
		"messages":[{"role":"user","content":"test"}],
		"tools":[{"type":"function","function":{"name":"Write","description":"Write file","parameters":{"type":"object"}}}]
	}`

	reqBytes, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["tool_choice"] != "auto" {
		t.Fatalf("expected tool_choice=auto, got %#v", req["tool_choice"])
	}
	if _, ok := req["store"]; ok {
		t.Fatalf("did not expect store in generic openai2 conversion, got %#v", req["store"])
	}
	if _, ok := req["instructions"]; ok {
		t.Fatalf("did not expect instructions without system prompt, got %#v", req["instructions"])
	}
}

func TestOpenAIReqToOpenAI2JoinsSystemMessagesAndExtractsArrayText(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"system","content":"rule one"},
			{"role":"system","content":[{"type":"text","text":"rule two"}]},
			{"role":"user","content":[{"type":"text","text":"hello"},{"type":"input_text","text":" world"}]}
		]
	}`

	reqBytes, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if req["instructions"] != "rule one\n\nrule two" {
		t.Fatalf("expected joined instructions, got %#v", req["instructions"])
	}
	input := req["input"].([]interface{})
	if len(input) != 1 {
		t.Fatalf("expected 1 input item, got %d", len(input))
	}
	content := input[0].(map[string]interface{})["content"].([]interface{})
	if content[0].(map[string]interface{})["text"] != "hello world" {
		t.Fatalf("expected extracted text content, got %#v", content[0])
	}
}

func TestOpenAIReqToOpenAI2PreservesImageParts(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image_url","image_url":{"url":"https://example.com/cat.png","detail":"high"}},
				{"type":"text","text":" please"}
			]}
		]
	}`

	reqBytes, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	input := req["input"].([]interface{})
	content := input[0].(map[string]interface{})["content"].([]interface{})
	if len(content) != 3 {
		t.Fatalf("expected 3 content parts, got %#v", content)
	}
	if content[1].(map[string]interface{})["type"] != "input_image" {
		t.Fatalf("expected input_image part, got %#v", content[1])
	}
	if content[1].(map[string]interface{})["image_url"] != "https://example.com/cat.png" {
		t.Fatalf("expected preserved image url, got %#v", content[1])
	}
}

func TestOpenAI2RespToOpenAIPreservesTotalTokens(t *testing.T) {
	openai2Resp := `{
		"id":"resp_123",
		"object":"response",
		"status":"completed",
		"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":99}
	}`

	respBytes, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed response failed: %v", err)
	}

	usage, ok := resp["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage object, got %#v", resp["usage"])
	}

	if usage["total_tokens"] != float64(99) {
		t.Fatalf("expected total_tokens=99, got %#v", usage["total_tokens"])
	}
}

func TestOpenAI2StreamToOpenAIIncludesUsageOnCompleted(t *testing.T) {
	ctx := transformer.NewStreamContext()

	created := `data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`
	if out, err := OpenAI2StreamToOpenAI([]byte(created), ctx, "gpt-4.1"); err != nil {
		t.Fatalf("response.created failed: %v", err)
	} else if out != nil {
		t.Fatalf("expected nil output for response.created, got %s", string(out))
	}

	completed := `data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":7,"output_tokens":3,"total_tokens":42}}}`
	out, err := OpenAI2StreamToOpenAI([]byte(completed), ctx, "gpt-4.1")
	if err != nil {
		t.Fatalf("response.completed failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed chunk, got nil")
	}

	_, jsonData := parseSSE(out)
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		t.Fatalf("unmarshal chunk failed: %v, raw=%s", err, jsonData)
	}

	usage, ok := chunk["usage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected usage in final chunk, got %#v", chunk["usage"])
	}
	if usage["prompt_tokens"] != float64(7) {
		t.Fatalf("expected prompt_tokens=7, got %#v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"] != float64(3) {
		t.Fatalf("expected completion_tokens=3, got %#v", usage["completion_tokens"])
	}
	if usage["total_tokens"] != float64(42) {
		t.Fatalf("expected total_tokens=42, got %#v", usage["total_tokens"])
	}
}

func TestOpenAIReqToOpenAI2PreservesAssistantToolChain(t *testing.T) {
	openaiReq := `{
		"model":"gpt-4.1",
		"messages":[
			{"role":"user","content":"use tool"},
			{"role":"assistant","content":"calling tool","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Write","arguments":"{\"path\":\"/tmp/a.txt\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"ok"}
		]
	}`

	reqBytes, err := OpenAIReqToOpenAI2([]byte(openaiReq), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAIReqToOpenAI2 failed: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	input := req["input"].([]interface{})
	if len(input) != 4 {
		t.Fatalf("expected 4 input items, got %d", len(input))
	}

	toolCall := input[2].(map[string]interface{})
	if toolCall["type"] != "function_call" || toolCall["call_id"] != "call_1" {
		t.Fatalf("expected preserved function_call, got %#v", toolCall)
	}

	toolOutput := input[3].(map[string]interface{})
	if toolOutput["type"] != "function_call_output" || toolOutput["call_id"] != "call_1" || toolOutput["output"] != "ok" {
		t.Fatalf("expected preserved function_call_output, got %#v", toolOutput)
	}
}

func TestOpenAI2ReqToOpenAIAttachesReasoningToAssistantMessage(t *testing.T) {
	openai2Req := `{
		"model":"gpt-4.1",
		"input":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"think first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		]
	}`

	reqBytes, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var req transformer.OpenAIRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}
	if req.Messages[0].ReasoningContent != "think first" {
		t.Fatalf("expected reasoning_content to be attached, got %#v", req.Messages[0].ReasoningContent)
	}
}

func TestOpenAIRespToOpenAI2IncludesReasoningOutputItem(t *testing.T) {
	openaiResp := `{
		"id":"chatcmpl_1",
		"object":"chat.completion",
		"model":"gpt-4.1",
		"choices":[{"index":0,"message":{"role":"assistant","content":"answer","reasoning_content":"think first"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`

	respBytes, err := OpenAIRespToOpenAI2([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToOpenAI2 failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed resp failed: %v", err)
	}

	output := resp["output"].([]interface{})
	if len(output) != 2 {
		t.Fatalf("expected reasoning + message output, got %d items", len(output))
	}
	if output[0].(map[string]interface{})["type"] != "reasoning" {
		t.Fatalf("expected first output item to be reasoning, got %#v", output[0])
	}
	if resp["model"] != "gpt-4.1" {
		t.Fatalf("expected model to be preserved, got %#v", resp["model"])
	}
	if resp["status"] != "completed" {
		t.Fatalf("expected completed status, got %#v", resp["status"])
	}
	if output[0].(map[string]interface{})["id"] == "" {
		t.Fatalf("expected reasoning id, got %#v", output[0])
	}
	if output[1].(map[string]interface{})["status"] != "completed" {
		t.Fatalf("expected completed message item, got %#v", output[1])
	}
}

func TestOpenAIRespToOpenAI2MapsLengthToIncomplete(t *testing.T) {
	openaiResp := `{
		"id":"chatcmpl_1",
		"object":"chat.completion",
		"model":"gpt-4.1",
		"choices":[{"index":0,"message":{"role":"assistant","content":"answer"},"finish_reason":"length"}],
		"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}
	}`

	respBytes, err := OpenAIRespToOpenAI2([]byte(openaiResp))
	if err != nil {
		t.Fatalf("OpenAIRespToOpenAI2 failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed resp failed: %v", err)
	}

	if resp["status"] != "incomplete" {
		t.Fatalf("expected incomplete status, got %#v", resp["status"])
	}
}

func TestOpenAI2RespToOpenAIRestoresReasoningContent(t *testing.T) {
	openai2Resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"completed",
		"output":[
			{"type":"reasoning","summary":[{"type":"summary_text","text":"think first"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
	}`

	respBytes, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed response failed: %v", err)
	}

	message := resp["choices"].([]interface{})[0].(map[string]interface{})["message"].(map[string]interface{})
	if message["reasoning_content"] != "think first" {
		t.Fatalf("expected reasoning_content restored, got %#v", message["reasoning_content"])
	}
}

func TestOpenAI2RespToOpenAIMapsIncompleteToLength(t *testing.T) {
	openai2Resp := `{
		"id":"resp_1",
		"object":"response",
		"status":"incomplete",
		"output":[
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}
		],
		"usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}
	}`

	respBytes, err := OpenAI2RespToOpenAI([]byte(openai2Resp), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2RespToOpenAI failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal transformed response failed: %v", err)
	}

	choice := resp["choices"].([]interface{})[0].(map[string]interface{})
	if choice["finish_reason"] != "length" {
		t.Fatalf("expected finish_reason=length, got %#v", choice["finish_reason"])
	}
}

func TestOpenAIStreamToOpenAI2EmitsReasoningEvents(t *testing.T) {
	ctx := transformer.NewStreamContext()

	event := []byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"reasoning_content":"think first"}}]}`)
	out, err := OpenAIStreamToOpenAI2(event, ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed output, got nil")
	}

	output := string(out)
	if !strings.Contains(output, `"type":"response.reasoning_summary_text.delta"`) {
		t.Fatalf("expected reasoning_summary_text delta event, got %s", output)
	}
	if !strings.Contains(output, `"delta":"think first"`) {
		t.Fatalf("expected reasoning delta payload, got %s", output)
	}
}

func TestOpenAI2ReqToOpenAISupportsEasyInputMessage(t *testing.T) {
	openai2Req := `{
		"model":"gpt-4.1",
		"input":[
			{"role":"user","content":"hello"}
		]
	}`

	reqBytes, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var req transformer.OpenAIRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
		t.Fatalf("expected easy input message preserved, got %#v", req.Messages)
	}
}

func TestOpenAI2ReqToOpenAIPreservesInputImageParts(t *testing.T) {
	openai2Req := `{
		"model":"gpt-4.1",
		"input":[
			{"type":"message","role":"user","content":[
				{"type":"input_text","text":"look"},
				{"type":"input_image","image_url":"https://example.com/cat.png","detail":"high"}
			]}
		]
	}`

	reqBytes, err := OpenAI2ReqToOpenAI([]byte(openai2Req), "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2ReqToOpenAI failed: %v", err)
	}

	var req transformer.OpenAIRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		t.Fatalf("unmarshal transformed req failed: %v", err)
	}

	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(req.Messages))
	}

	content, ok := req.Messages[0].Content.([]interface{})
	if !ok {
		t.Fatalf("expected multimodal content array, got %#v", req.Messages[0].Content)
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %#v", content)
	}
	imagePart := content[1].(map[string]interface{})
	imageURL := imagePart["image_url"].(map[string]interface{})
	if imagePart["type"] != "image_url" || imageURL["url"] != "https://example.com/cat.png" {
		t.Fatalf("expected preserved image_url part, got %#v", imagePart)
	}
	if imageURL["detail"] != "high" {
		t.Fatalf("expected preserved image detail, got %#v", imageURL)
	}
}

func TestOpenAI2StreamToOpenAIEmitsReasoningContentDelta(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"

	event := `data: {"type":"response.reasoning_summary_text.delta","delta":"think first"}`
	out, err := OpenAI2StreamToOpenAI([]byte(event), ctx, "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected transformed chunk, got nil")
	}

	if !strings.Contains(string(out), `"reasoning_content":"think first"`) {
		t.Fatalf("expected reasoning_content delta, got %s", string(out))
	}
}

func TestOpenAI2StreamToOpenAIHandlesInterleavedToolCalls(t *testing.T) {
	ctx := transformer.NewStreamContext()

	events := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":\"REA"}`,
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"write_file","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":\"OUT"}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":1,"delta":"DME.md\"}"}`,
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","status":"completed"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"PUT.md\"}"}`,
		`data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_2","call_id":"call_2","name":"write_file","status":"completed"}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed"}}`,
	}

	var output strings.Builder
	for _, event := range events {
		out, err := OpenAI2StreamToOpenAI([]byte(event), ctx, "gpt-4.1")
		if err != nil {
			t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
		}
		output.Write(out)
	}

	out := output.String()
	if strings.Count(out, `"tool_calls":[`) != 2 {
		t.Fatalf("expected 2 tool call chunks, got %s", out)
	}
	if !strings.Contains(out, `"id":"call_1"`) || !strings.Contains(out, `"index":0`) || !strings.Contains(out, `"name":"read_file"`) || !strings.Contains(out, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected first tool call to keep its own arguments, got %s", out)
	}
	if !strings.Contains(out, `"id":"call_2"`) || !strings.Contains(out, `"index":1`) || !strings.Contains(out, `"name":"write_file"`) || !strings.Contains(out, `"arguments":"{\"path\":\"OUTPUT.md\"}"`) {
		t.Fatalf("expected second tool call to keep its own arguments, got %s", out)
	}
}

func TestOpenAI2StreamToOpenAIAssignsToolIndexesIndependentlyFromOutputIndexes(t *testing.T) {
	ctx := transformer.NewStreamContext()

	events := []string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","status":"in_progress"}}`,
		`data: {"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","arguments":"","status":"in_progress"}}`,
		`data: {"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"path\":\"README.md\"}"}`,
		`data: {"type":"response.output_item.done","output_index":2,"item":{"type":"function_call","id":"fc_1","call_id":"call_1","name":"read_file","status":"completed"}}`,
	}

	var output strings.Builder
	for _, event := range events {
		out, err := OpenAI2StreamToOpenAI([]byte(event), ctx, "gpt-4.1")
		if err != nil {
			t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
		}
		output.Write(out)
	}

	out := output.String()
	if !strings.Contains(out, `"index":0`) {
		t.Fatalf("expected first tool call to stay index 0 even when output_index starts at 2, got %s", out)
	}
	if strings.Contains(out, `"index":1`) {
		t.Fatalf("did not expect first tool call index to mirror output_index offset, got %s", out)
	}
}

func TestOpenAIStreamToOpenAI2HandlesInterleavedToolCalls(t *testing.T) {
	ctx := transformer.NewStreamContext()

	events := []string{
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"read_file"}}]}}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"REA"}}]}}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"write_file"}}]}}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"OUT"}}]}}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"DME.md\"}"}}]}}]}`,
		`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"PUT.md\"}"}}]},"finish_reason":"tool_calls"}]}`,
	}

	var output strings.Builder
	for _, event := range events {
		out, err := OpenAIStreamToOpenAI2([]byte(event), ctx)
		if err != nil {
			t.Fatalf("OpenAIStreamToOpenAI2 failed: %v", err)
		}
		output.Write(out)
	}

	out := output.String()
	if strings.Count(out, `"type":"response.output_item.done"`) != 2 {
		t.Fatalf("expected 2 completed function_call items, got %s", out)
	}
	if !strings.Contains(out, `"call_id":"call_1"`) || !strings.Contains(out, `"name":"read_file"`) || !strings.Contains(out, `"arguments":"{\"path\":\"README.md\"}"`) {
		t.Fatalf("expected first function_call to keep its own arguments, got %s", out)
	}
	if !strings.Contains(out, `"call_id":"call_2"`) || !strings.Contains(out, `"name":"write_file"`) || !strings.Contains(out, `"arguments":"{\"path\":\"OUTPUT.md\"}"`) {
		t.Fatalf("expected second function_call to keep its own arguments, got %s", out)
	}
	if strings.Index(out, `"call_id":"call_1"`) > strings.Index(out, `"call_id":"call_2"`) {
		t.Fatalf("expected tool calls to finish in tool index order, got %s", out)
	}
}

func TestOpenAI2StreamToOpenAIUsesExistingUsageWhenCompletedOmitsValues(t *testing.T) {
	ctx := transformer.NewStreamContext()
	ctx.MessageID = "resp_1"
	ctx.InputTokens = 7
	ctx.OutputTokens = 3

	out, err := OpenAI2StreamToOpenAI([]byte(`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","status":"completed","usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0}}}`), ctx, "gpt-4.1")
	if err != nil {
		t.Fatalf("OpenAI2StreamToOpenAI failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected final chunk, got nil")
	}

	_, jsonData := parseSSE(out)
	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &chunk); err != nil {
		t.Fatalf("unmarshal chunk failed: %v", err)
	}
	usage := chunk["usage"].(map[string]interface{})
	if usage["prompt_tokens"] != float64(7) || usage["completion_tokens"] != float64(3) || usage["total_tokens"] != float64(10) {
		t.Fatalf("expected preserved context usage, got %#v", usage)
	}
}

func TestOpenAIStreamToOpenAI2EmitsCompletedBeforeDoneOnBareDone(t *testing.T) {
	ctx := transformer.NewStreamContext()

	if _, err := OpenAIStreamToOpenAI2([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"content":"hello"}}]}`), ctx); err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 content chunk failed: %v", err)
	}

	out, err := OpenAIStreamToOpenAI2([]byte("data: [DONE]"), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 bare done failed: %v", err)
	}
	if out == nil {
		t.Fatal("expected synthesized completed output, got nil")
	}

	outStr := string(out)
	completedIndex := strings.Index(outStr, `"type":"response.completed"`)
	doneIndex := strings.Index(outStr, "data: [DONE]")
	if completedIndex == -1 || doneIndex == -1 || completedIndex > doneIndex {
		t.Fatalf("expected response.completed before [DONE], got %s", outStr)
	}
}

func TestOpenAIStreamToOpenAI2ReturnsErrorAfterPartialOutput(t *testing.T) {
	ctx := transformer.NewStreamContext()

	first, err := OpenAIStreamToOpenAI2([]byte(`data: {"id":"chatcmpl_1","object":"chat.completion.chunk","model":"gpt-4.1","choices":[{"index":0,"delta":{"content":"hello"}}]}`), ctx)
	if err != nil {
		t.Fatalf("OpenAIStreamToOpenAI2 first chunk failed: %v", err)
	}
	if first == nil || !strings.Contains(string(first), `"delta":"hello"`) {
		t.Fatalf("expected partial output before error, got %s", string(first))
	}

	second, err := OpenAIStreamToOpenAI2([]byte(`data: {"error":{"message":"boom","type":"server_error"}}`), ctx)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected upstream error, got out=%s err=%v", string(second), err)
	}
	if second != nil {
		t.Fatalf("did not expect output chunk on error, got %s", string(second))
	}
}
