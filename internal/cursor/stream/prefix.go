package stream

import (
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func Prefix(meta shared.RequestMeta, state *FinalizeState, model string, transformerName string) []byte {
	if !meta.CursorMode || meta.ClientFormat != shared.ClientFormatOpenAIResponses || transformerName == "cx_resp_openai2" {
		return nil
	}

	responseID := ""
	if state != nil {
		responseID = strings.TrimSpace(state.ResponsesResponseID)
	}
	if responseID == "" {
		responseID = "resp_" + uuid.NewString()
	}

	payload := map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id":     responseID,
			"object": "response",
			"status": "in_progress",
			"output": []interface{}{},
			"model":  model,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil
	}

	if state != nil {
		state.ResponsesResponseID = responseID
		state.ResponsesCreatedEmitted = true
	}

	return []byte("event: response.created\ndata: " + string(encoded) + "\n\n")
}
