package stream

import (
	"bytes"
	"sort"
	"strings"

	"github.com/google/uuid"
	cursorcache "github.com/lich0821/ccNexus/internal/cursor/cache"
)

func FixResponsesBundle(bundle []byte, clientModel string, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache, state *FinalizeState) ([]byte, error) {
	chunks := splitSSEBundle(bundle)
	if len(chunks) == 0 {
		return bundle, nil
	}

	var output bytes.Buffer
	for _, chunk := range chunks {
		eventName, data, ok := parseSSEChunk(chunk)
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}

		if data == "[DONE]" {
			// Align with api2cursor responses streams: rely on response.completed and omit [DONE].
			continue
		}

		payload, ok := decodeJSONObject([]byte(data))
		if !ok {
			output.Write(chunk)
			if !bytes.HasSuffix(chunk, []byte("\n\n")) {
				output.WriteString("\n\n")
			}
			continue
		}

		if eventName == "" {
			eventName = stringValue(payload["type"])
		}
		if eventName == "response.created" && state != nil && state.ResponsesCreatedEmitted {
			continue
		}

		for _, item := range formatResponsesStreamEventsWithState(eventName, payload, clientModel, transformerName, cacheMessages, thinkingCache, state) {
			writeSSEChunk(&output, item.eventName, item.payload)
		}
	}

	return output.Bytes(), nil
}

func formatResponsesStreamEventsWithState(eventName string, payload map[string]interface{}, clientModel string, transformerName string, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache, state *FinalizeState) []sseItem {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	if transformerName == "cx_resp_openai2" {
		if eventName == "response.completed" {
			storeResponsesThinkingCacheFromEvent(payload, cacheMessages, thinkingCache)
		}
		ev, p := formatNativeResponsesStreamEvent(eventName, payload, clientModel)
		return []sseItem{{eventName: ev, payload: p}}
	}
	if state == nil {
		ev, p, ok := formatResponsesStreamEvent(eventName, payload, clientModel)
		if !ok {
			return nil
		}
		return []sseItem{{eventName: ev, payload: p}}
	}

	prefixEvents := make([]sseItem, 0)
	switch eventName {
	case "response.created":
		if response, ok := payload["response"].(map[string]interface{}); ok {
			state.ResponsesResponseID = stringValue(response["id"])
			payload = cloneJSONObject(payload)
			rewritten := cloneJSONObject(response)
			if rewritten["id"] == nil && state.ResponsesResponseID != "" {
				rewritten["id"] = state.ResponsesResponseID
			}
			if rewritten["object"] == nil {
				rewritten["object"] = "response"
			}
			if rewritten["status"] == nil {
				rewritten["status"] = "in_progress"
			}
			if _, ok := rewritten["output"]; !ok {
				rewritten["output"] = []interface{}{}
			}
			if clientModel != "" {
				rewritten["model"] = clientModel
			}
			payload["response"] = rewritten
		}
	case "response.output_item.added":
		trackResponsesAdded(state, payload)
		payload = enrichResponsesStreamItemEvent(state, payload, false)
	case "response.reasoning_summary_text.delta":
		state.ResponsesReasoningBuf += stringValue(payload["delta"])
	case "response.reasoning_summary_text.done":
		payload = cloneJSONObject(payload)
		if strings.TrimSpace(stringValue(payload["text"])) == "" && state.ResponsesReasoningBuf != "" {
			payload["text"] = state.ResponsesReasoningBuf
		}
	case "response.output_text.delta":
		state.ResponsesMessageText += stringValue(payload["delta"])
	case "response.output_text.done":
		payload = cloneJSONObject(payload)
		if strings.TrimSpace(stringValue(payload["text"])) == "" && state.ResponsesMessageText != "" {
			payload["text"] = state.ResponsesMessageText
		}
	case "response.function_call_arguments.delta":
		trackResponsesToolArguments(state, payload)
	case "response.function_call_arguments.done":
		trackResponsesToolArgumentsDone(state, payload)
		payload = enrichResponsesFunctionArgumentsDone(state, payload)
	case "response.output_item.done":
		trackResponsesDone(state, payload)
		payload = enrichResponsesStreamItemEvent(state, payload, true)
	case "response.completed":
		prefixEvents = append(prefixEvents, buildResponsesFinalizeDoneEvents(state, clientModel)...)
		payload = injectResponsesCompletedOutput(state, payload, clientModel)
		storeResponsesThinkingCacheFromEvent(payload, cacheMessages, thinkingCache)
	}

	ev, rewritten, ok := formatResponsesStreamEvent(eventName, payload, clientModel)
	if !ok {
		return prefixEvents
	}
	prefixEvents = append(prefixEvents, sseItem{eventName: ev, payload: rewritten})
	return prefixEvents
}

func storeResponsesThinkingCacheFromEvent(payload map[string]interface{}, cacheMessages []map[string]interface{}, thinkingCache *cursorcache.ThinkingCache) {
	if len(cacheMessages) == 0 || thinkingCache == nil || payload == nil {
		return
	}
	responsePayload := payload
	if nested, ok := payload["response"].(map[string]interface{}); ok {
		responsePayload = nested
	}
	output, ok := responsePayload["output"].([]interface{})
	if !ok || len(output) == 0 {
		return
	}
	thinkingCache.StoreFromResponsesOutput(cacheMessages, output)
}

func formatResponsesStreamEvent(eventName string, payload map[string]interface{}, clientModel string) (string, map[string]interface{}, bool) {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	if eventName == "" {
		return "", payload, false
	}

	switch eventName {
	case "response.created", "response.completed":
		if response, ok := payload["response"].(map[string]interface{}); ok {
			rewritten := cloneJSONObject(response)
			if clientModel != "" {
				rewritten["model"] = clientModel
			}
			return eventName, rewritten, true
		}
	case "response.output_item.added", "response.output_item.done":
		if item, ok := payload["item"].(map[string]interface{}); ok {
			return eventName, cloneJSONObject(item), true
		}
	case "response.content_part.added":
		if part, ok := payload["part"].(map[string]interface{}); ok {
			return eventName, cloneJSONObject(part), true
		}
	case "response.content_part.done":
		return "", nil, false
	case "response.output_text.delta":
		return eventName, map[string]interface{}{
			"type":  "output_text",
			"delta": stringValue(payload["delta"]),
		}, true
	case "response.output_text.done":
		return eventName, map[string]interface{}{
			"type": "output_text",
			"text": stringValue(payload["text"]),
		}, true
	case "response.reasoning_summary_text.delta":
		return eventName, map[string]interface{}{
			"type":  "summary_text",
			"delta": stringValue(payload["delta"]),
		}, true
	case "response.reasoning_summary_text.done":
		return eventName, map[string]interface{}{
			"type": "summary_text",
			"text": stringValue(payload["text"]),
		}, true
	case "response.function_call_arguments.delta":
		return eventName, map[string]interface{}{
			"type":  "function_call",
			"delta": stringValue(payload["delta"]),
		}, true
	case "response.function_call_arguments.done":
		return eventName, map[string]interface{}{
			"type":      "function_call",
			"arguments": stringValue(payload["arguments"]),
		}, true
	}

	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		if _, ok := rewritten["model"]; ok {
			rewritten["model"] = clientModel
		}
	}
	return eventName, rewritten, true
}

func formatNativeResponsesStreamEvent(eventName string, payload map[string]interface{}, clientModel string) (string, map[string]interface{}) {
	if eventName == "" {
		eventName = stringValue(payload["type"])
	}
	rewritten := cloneJSONObject(payload)
	if clientModel != "" {
		if model := stringValue(rewritten["model"]); model != "" {
			rewritten["model"] = clientModel
		}
		if response, ok := rewritten["response"].(map[string]interface{}); ok {
			responseCopy := cloneJSONObject(response)
			if model := stringValue(responseCopy["model"]); model != "" {
				responseCopy["model"] = clientModel
			}
			rewritten["response"] = responseCopy
		}
	}
	return eventName, rewritten
}

func trackResponsesAdded(state *FinalizeState, payload map[string]interface{}) {
	if state == nil {
		return
	}
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	switch stringValue(item["type"]) {
	case "reasoning":
		state.ResponsesReasoningID = firstNonEmptyString(stringValue(item["id"]), "rs_"+uuid.NewString())
		state.ResponsesReasoningOn = true
		if summary := extractResponsesReasoningSummary(item); summary != "" {
			state.ResponsesReasoningBuf = summary
		}
	case "message":
		state.ResponsesMessageID = firstNonEmptyString(stringValue(item["id"]), "msg_"+uuid.NewString())
		state.ResponsesMessageOn = true
		if text := extractResponsesMessageText(item); text != "" {
			state.ResponsesMessageText = text
		}
	case "function_call":
		index := responsesOutputIndex(payload)
		if index < 0 {
			index = len(state.ResponsesTools)
		}
		tool := &ResponseToolState{
			ID:        firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString()),
			CallID:    firstNonEmptyString(stringValue(item["call_id"]), newToolCallID()),
			Name:      stringValue(item["name"]),
			Arguments: stringValue(item["arguments"]),
			Active:    true,
		}
		if state.ResponsesTools == nil {
			state.ResponsesTools = make(map[int]*ResponseToolState)
		}
		state.ResponsesTools[index] = tool
	}
}

func trackResponsesToolArguments(state *FinalizeState, payload map[string]interface{}) {
	delta := stringValue(payload["delta"])
	if state == nil || delta == "" {
		return
	}
	tool := responsesToolFromPayload(state, payload)
	if tool == nil {
		index := len(state.ResponsesTools)
		tool = &ResponseToolState{
			ID:     "fc_" + uuid.NewString(),
			CallID: newToolCallID(),
			Active: true,
		}
		if state.ResponsesTools == nil {
			state.ResponsesTools = make(map[int]*ResponseToolState)
		}
		state.ResponsesTools[index] = tool
	}
	tool.Arguments += delta
}

func trackResponsesToolArgumentsDone(state *FinalizeState, payload map[string]interface{}) {
	arguments := stringValue(payload["arguments"])
	if state == nil || arguments == "" {
		return
	}
	tool := responsesToolFromPayload(state, payload)
	if tool == nil {
		return
	}
	tool.Arguments = arguments
}

func trackResponsesDone(state *FinalizeState, payload map[string]interface{}) {
	if state == nil {
		return
	}
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	switch stringValue(item["type"]) {
	case "reasoning":
		outputItem := cloneJSONObject(item)
		if len(outputItem) == 0 {
			outputItem = buildResponsesReasoningItem(state)
		}
		if _, ok := outputItem["summary"]; !ok {
			outputItem["summary"] = []interface{}{
				map[string]interface{}{"type": "summary_text", "text": state.ResponsesReasoningBuf},
			}
		}
		appendResponsesOutput(state, outputItem)
		state.ResponsesReasoningOn = false
	case "message":
		outputItem := cloneJSONObject(item)
		if len(outputItem) == 0 {
			outputItem = buildResponsesMessageItem(state)
		}
		if _, ok := outputItem["content"]; !ok {
			outputItem["content"] = []interface{}{
				map[string]interface{}{"type": "output_text", "text": state.ResponsesMessageText},
			}
		}
		appendResponsesOutput(state, outputItem)
		state.ResponsesMessageOn = false
	case "function_call":
		outputItem, tool := buildResponsesToolItemFromPayload(state, payload)
		appendResponsesOutput(state, outputItem)
		if tool != nil {
			tool.Active = false
		}
	}
}

func injectResponsesCompletedOutput(state *FinalizeState, payload map[string]interface{}, clientModel string) map[string]interface{} {
	if state == nil {
		return payload
	}

	if state.ResponsesReasoningOn && state.ResponsesReasoningBuf != "" {
		appendResponsesOutput(state, buildResponsesReasoningItem(state))
		state.ResponsesReasoningOn = false
	}
	if state.ResponsesMessageOn && state.ResponsesMessageText != "" {
		appendResponsesOutput(state, buildResponsesMessageItem(state))
		state.ResponsesMessageOn = false
	}
	for _, index := range sortedResponsesToolIndexes(state) {
		tool := state.ResponsesTools[index]
		if tool == nil || !tool.Active {
			continue
		}
		appendResponsesOutput(state, buildResponsesToolItem(tool))
		tool.Active = false
	}

	rewritten := cloneJSONObject(payload)
	response, ok := payload["response"].(map[string]interface{})
	if !ok {
		response = map[string]interface{}{}
	}
	rewrittenResponse := cloneJSONObject(response)
	if rewrittenResponse["id"] == nil && state.ResponsesResponseID != "" {
		rewrittenResponse["id"] = state.ResponsesResponseID
	}
	if clientModel != "" {
		rewrittenResponse["model"] = clientModel
	}
	output, hasOutput := rewrittenResponse["output"].([]interface{})
	if !hasOutput || len(output) == 0 {
		rewrittenResponse["output"] = responsesOutputAsInterfaces(state)
	}
	rewritten["response"] = rewrittenResponse
	return rewritten
}

func buildResponsesFinalizeDoneEvents(state *FinalizeState, clientModel string) []sseItem {
	if state == nil {
		return nil
	}
	events := make([]sseItem, 0)

	if state.ResponsesReasoningOn && state.ResponsesReasoningBuf != "" {
		item := buildResponsesReasoningItem(state)
		appendResponsesOutput(state, item)
		state.ResponsesReasoningOn = false

		ev1, p1, _ := formatResponsesStreamEvent("response.reasoning_summary_text.done", map[string]interface{}{
			"type": "response.reasoning_summary_text.done",
			"text": state.ResponsesReasoningBuf,
		}, clientModel)
		events = append(events, sseItem{eventName: ev1, payload: p1})

		ev2, p2, _ := formatResponsesStreamEvent("response.output_item.done", map[string]interface{}{
			"type": "response.output_item.done",
			"item": item,
		}, clientModel)
		events = append(events, sseItem{eventName: ev2, payload: p2})
	}

	if state.ResponsesMessageOn && state.ResponsesMessageText != "" {
		item := buildResponsesMessageItem(state)
		appendResponsesOutput(state, item)
		state.ResponsesMessageOn = false

		ev1, p1, _ := formatResponsesStreamEvent("response.output_text.done", map[string]interface{}{
			"type": "response.output_text.done",
			"text": state.ResponsesMessageText,
		}, clientModel)
		events = append(events, sseItem{eventName: ev1, payload: p1})

		ev2, p2, _ := formatResponsesStreamEvent("response.output_item.done", map[string]interface{}{
			"type": "response.output_item.done",
			"item": item,
		}, clientModel)
		events = append(events, sseItem{eventName: ev2, payload: p2})
	}

	for _, index := range sortedResponsesToolIndexes(state) {
		tool := state.ResponsesTools[index]
		if tool == nil || !tool.Active {
			continue
		}
		item := buildResponsesToolItem(tool)
		appendResponsesOutput(state, item)
		tool.Active = false

		ev1, p1, _ := formatResponsesStreamEvent("response.function_call_arguments.done", map[string]interface{}{
			"type":      "response.function_call_arguments.done",
			"arguments": tool.Arguments,
			"call_id":   tool.CallID,
		}, clientModel)
		events = append(events, sseItem{eventName: ev1, payload: p1})

		ev2, p2, _ := formatResponsesStreamEvent("response.output_item.done", map[string]interface{}{
			"type": "response.output_item.done",
			"item": item,
		}, clientModel)
		events = append(events, sseItem{eventName: ev2, payload: p2})
	}

	return events
}

func buildResponsesReasoningItem(state *FinalizeState) map[string]interface{} {
	return map[string]interface{}{
		"type": "reasoning",
		"id":   firstNonEmptyString(state.ResponsesReasoningID, "rs_"+uuid.NewString()),
		"summary": []interface{}{
			map[string]interface{}{"type": "summary_text", "text": state.ResponsesReasoningBuf},
		},
	}
}

func buildResponsesReasoningAddedItem(state *FinalizeState) map[string]interface{} {
	return map[string]interface{}{
		"type":    "reasoning",
		"id":      firstNonEmptyString(state.ResponsesReasoningID, "rs_"+uuid.NewString()),
		"summary": []interface{}{},
	}
}

func buildResponsesMessageAddedItem(state *FinalizeState) map[string]interface{} {
	return map[string]interface{}{
		"type":    "message",
		"id":      firstNonEmptyString(state.ResponsesMessageID, "msg_"+uuid.NewString()),
		"status":  "in_progress",
		"role":    "assistant",
		"content": []interface{}{},
	}
}

func enrichResponsesStreamItemEvent(state *FinalizeState, payload map[string]interface{}, done bool) map[string]interface{} {
	if state == nil {
		return payload
	}
	rewritten := cloneJSONObject(payload)
	item, ok := payload["item"].(map[string]interface{})
	if !ok {
		item = payload
	}
	itemType := stringValue(item["type"])
	var enriched map[string]interface{}

	switch itemType {
	case "reasoning":
		if done {
			enriched = buildResponsesReasoningItem(state)
		} else {
			enriched = buildResponsesReasoningAddedItem(state)
		}
	case "message":
		if done {
			enriched = buildResponsesMessageItem(state)
		} else {
			enriched = buildResponsesMessageAddedItem(state)
		}
	case "function_call":
		enriched, _ = buildResponsesToolItemFromPayload(state, payload)
		if !done {
			enriched["status"] = "in_progress"
			if _, ok := enriched["arguments"]; !ok {
				enriched["arguments"] = ""
			}
		}
	default:
		return payload
	}

	for key, value := range item {
		enriched[key] = value
	}
	rewritten["item"] = enriched
	return rewritten
}

func enrichResponsesFunctionArgumentsDone(state *FinalizeState, payload map[string]interface{}) map[string]interface{} {
	if state == nil {
		return payload
	}
	rewritten := cloneJSONObject(payload)
	if strings.TrimSpace(stringValue(rewritten["arguments"])) != "" {
		if _, ok := rewritten["type"]; !ok {
			rewritten["type"] = "function_call"
		}
		return rewritten
	}
	tool := responsesToolFromPayload(state, rewritten)
	if tool == nil || strings.TrimSpace(tool.Arguments) == "" {
		return rewritten
	}
	rewritten["arguments"] = tool.Arguments
	if _, ok := rewritten["type"]; !ok {
		rewritten["type"] = "function_call"
	}
	return rewritten
}

func buildResponsesMessageItem(state *FinalizeState) map[string]interface{} {
	return map[string]interface{}{
		"type":   "message",
		"id":     firstNonEmptyString(state.ResponsesMessageID, "msg_"+uuid.NewString()),
		"status": "completed",
		"role":   "assistant",
		"content": []interface{}{
			map[string]interface{}{"type": "output_text", "text": state.ResponsesMessageText},
		},
	}
}

func buildResponsesToolItem(tool *ResponseToolState) map[string]interface{} {
	if tool == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"type":      "function_call",
		"id":        firstNonEmptyString(tool.ID, "fc_"+uuid.NewString()),
		"status":    "completed",
		"call_id":   firstNonEmptyString(tool.CallID, newToolCallID()),
		"name":      tool.Name,
		"arguments": tool.Arguments,
	}
}

func buildResponsesToolItemFromPayload(state *FinalizeState, payload map[string]interface{}) (map[string]interface{}, *ResponseToolState) {
	item := payload
	if nested, ok := payload["item"].(map[string]interface{}); ok {
		item = nested
	}

	index := responsesOutputIndex(payload)
	tool := state.ResponsesTools[index]
	if tool == nil {
		tool = findResponsesToolByID(state, stringValue(item["id"]))
	}
	if tool == nil {
		tool = latestActiveResponsesTool(state)
	}
	if tool == nil {
		tool = &ResponseToolState{
			ID:        firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString()),
			CallID:    firstNonEmptyString(stringValue(item["call_id"]), newToolCallID()),
			Name:      stringValue(item["name"]),
			Arguments: stringValue(item["arguments"]),
		}
	}

	if tool.ID == "" {
		tool.ID = firstNonEmptyString(stringValue(item["id"]), "fc_"+uuid.NewString())
	}
	if tool.CallID == "" {
		tool.CallID = firstNonEmptyString(stringValue(item["call_id"]), newToolCallID())
	}
	if tool.Name == "" {
		tool.Name = stringValue(item["name"])
	}
	if tool.Arguments == "" {
		tool.Arguments = stringValue(item["arguments"])
	}

	outputItem := cloneJSONObject(item)
	if _, ok := outputItem["status"]; !ok {
		outputItem["status"] = "completed"
	}
	if _, ok := outputItem["id"]; !ok {
		outputItem["id"] = tool.ID
	}
	if _, ok := outputItem["call_id"]; !ok {
		outputItem["call_id"] = tool.CallID
	}
	if _, ok := outputItem["name"]; !ok {
		outputItem["name"] = tool.Name
	}
	if _, ok := outputItem["arguments"]; !ok {
		outputItem["arguments"] = tool.Arguments
	}
	if _, ok := outputItem["type"]; !ok {
		outputItem["type"] = "function_call"
	}
	return outputItem, tool
}

func appendResponsesOutput(state *FinalizeState, item map[string]interface{}) {
	if state == nil || len(item) == 0 {
		return
	}
	id := stringValue(item["id"])
	for index, existing := range state.ResponsesOutput {
		if id != "" && id == stringValue(existing["id"]) {
			state.ResponsesOutput[index] = cloneJSONObject(item)
			return
		}
	}
	state.ResponsesOutput = append(state.ResponsesOutput, cloneJSONObject(item))
}

func latestActiveResponsesTool(state *FinalizeState) *ResponseToolState {
	for _, index := range reverseResponsesToolIndexes(state) {
		tool := state.ResponsesTools[index]
		if tool != nil && tool.Active {
			return tool
		}
	}
	return nil
}

func responsesToolFromPayload(state *FinalizeState, payload map[string]interface{}) *ResponseToolState {
	if state == nil {
		return nil
	}
	if index := responsesOutputIndex(payload); index >= 0 {
		if tool := state.ResponsesTools[index]; tool != nil {
			return tool
		}
	}
	return latestActiveResponsesTool(state)
}

func findResponsesToolByID(state *FinalizeState, id string) *ResponseToolState {
	if state == nil || id == "" {
		return nil
	}
	for _, tool := range state.ResponsesTools {
		if tool != nil && tool.ID == id {
			return tool
		}
	}
	return nil
}

func responsesOutputAsInterfaces(state *FinalizeState) []interface{} {
	if state == nil || len(state.ResponsesOutput) == 0 {
		return []interface{}{}
	}
	output := make([]interface{}, 0, len(state.ResponsesOutput))
	for _, item := range state.ResponsesOutput {
		output = append(output, cloneJSONObject(item))
	}
	return output
}

func extractResponsesMessageText(item map[string]interface{}) string {
	content, ok := item["content"].([]interface{})
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, partValue := range content {
		part, ok := partValue.(map[string]interface{})
		if !ok {
			continue
		}
		if stringValue(part["type"]) != "output_text" {
			continue
		}
		builder.WriteString(stringValue(part["text"]))
	}
	return builder.String()
}

func extractResponsesReasoningSummary(item map[string]interface{}) string {
	summary, ok := item["summary"].([]interface{})
	if !ok {
		return ""
	}
	var builder strings.Builder
	for _, partValue := range summary {
		part, ok := partValue.(map[string]interface{})
		if !ok {
			continue
		}
		builder.WriteString(stringValue(part["text"]))
	}
	return builder.String()
}

func responsesOutputIndex(payload map[string]interface{}) int {
	if index, ok := payload["output_index"].(float64); ok {
		return int(index)
	}
	return -1
}

func sortedResponsesToolIndexes(state *FinalizeState) []int {
	if state == nil || len(state.ResponsesTools) == 0 {
		return nil
	}
	indexes := make([]int, 0, len(state.ResponsesTools))
	for index := range state.ResponsesTools {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	return indexes
}

func reverseResponsesToolIndexes(state *FinalizeState) []int {
	indexes := sortedResponsesToolIndexes(state)
	if len(indexes) == 0 {
		return nil
	}
	sort.Sort(sort.Reverse(sort.IntSlice(indexes)))
	return indexes
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
