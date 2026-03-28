package stream

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

type FinalizeState struct {
	InThinkingTag           bool
	ToolCallsSeen           bool
	OpenAI2ChatStarted      bool
	OpenAI2ChatResponseID   string
	OpenAI2ChatNextToolSlot int
	OpenAI2ChatSawToolCall  bool
	OpenAI2ChatToolSlots    map[int]int
	OpenAI2ChatLastUsage    map[string]interface{}
	// OpenAI2CallIDToSlot maps Responses function_call call_id -> chat tool_calls index (api2cursor uses call_id keys).
	OpenAI2CallIDToSlot map[string]int
	// RespOpenAIUpstream* is state for ExpandRespOpenAIUpstreamChatSSE (upstream Chat SSE before cx_resp_openai transform only).
	RespOpenAIUpstreamThinkInTag bool
	RespOpenAIUpstreamThinkBuf   string
	RespOpenAIUpstreamToolSeen   bool
	MessagesReasoningBuf    string
	MessagesThinkingShown   bool
	MessagesIndexOffset     int
	ResponsesCreatedEmitted bool
	ResponsesResponseID     string
	ResponsesReasoningID    string
	ResponsesReasoningBuf   string
	ResponsesReasoningOn    bool
	ResponsesMessageID      string
	ResponsesMessageText    string
	ResponsesMessageOn      bool
	ResponsesTools          map[int]*ResponseToolState
	ResponsesOutput         []map[string]interface{}
}

type ResponseToolState struct {
	ID        string
	CallID    string
	Name      string
	Arguments string
	Active    bool
}

func Finalize(meta shared.RequestMeta, state *FinalizeState, model string) []byte {
	if !meta.CursorMode || meta.ClientFormat != shared.ClientFormatOpenAIChat || state == nil || !state.InThinkingTag {
		return nil
	}
	return finalizeChatThinkingChunk(state, model)
}

func finalizeChatThinkingChunk(state *FinalizeState, model string) []byte {
	if state == nil || !state.InThinkingTag {
		return nil
	}
	state.InThinkingTag = false
	payload := map[string]interface{}{
		"id":     "",
		"object": "chat.completion.chunk",
		"model":  model,
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"delta": map[string]interface{}{
					"content": "\n</think>\n\n",
				},
				"finish_reason": nil,
			},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return []byte("data: " + string(encoded) + "\n\n")
}
