package augment

import (
	"fmt"
	"strings"
)

const (
	historySummaryToolResultMaxChars      = 8000
	historySummaryToolResultTailChars     = 512
	historySummaryToolUseInputMaxChars    = 2000
	historySummaryToolUseInputTailChars   = 256
	historySummaryDefaultEndPartTailChars = 2048
)

func preprocessHistoryForAPI(ar *AugmentRequest) ([]ChatHistoryEntry, []Node) {
	if ar == nil {
		return nil, nil
	}

	history := append([]ChatHistoryEntry(nil), ar.ChatHistory...)
	currentNodes := ar.EffectiveCurrentNodes()

	currentExchange := ChatHistoryEntry{
		RequestNodes: currentNodes,
	}

	combined := make([]ChatHistoryEntry, 0, len(history)+1)
	combined = append(combined, history...)
	combined = append(combined, currentExchange)

	start := -1
	for i := len(combined) - 1; i >= 0; i-- {
		if chatHistoryEntryHasSummary(combined[i]) {
			start = i
			break
		}
	}
	if start == -1 {
		return history, currentNodes
	}

	processed := append([]ChatHistoryEntry(nil), combined[start:]...)
	if len(processed) == 0 {
		return history, currentNodes
	}

	processed[0] = compactHistorySummaryEntry(processed[0])

	last := processed[len(processed)-1]
	processedCurrentNodes := last.EffectiveRequestNodes()
	if len(processedCurrentNodes) == 0 {
		processedCurrentNodes = currentNodes
	}

	return processed[:len(processed)-1], processedCurrentNodes
}

func chatHistoryEntryHasSummary(entry ChatHistoryEntry) bool {
	return findHistorySummaryNode(entry.EffectiveRequestNodes()) != nil
}

func findHistorySummaryNode(nodes []Node) *Node {
	for i := range nodes {
		if nodes[i].Type == 10 && nodes[i].HistorySummary != nil {
			return &nodes[i]
		}
	}
	return nil
}

func compactHistorySummaryEntry(entry ChatHistoryEntry) ChatHistoryEntry {
	nodes := entry.EffectiveRequestNodes()
	summaryNode := findHistorySummaryNode(nodes)
	if summaryNode == nil || summaryNode.HistorySummary == nil {
		return entry
	}

	var toolResults []*ToolResultNode
	otherNodes := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		switch {
		case node.Type == 10 && node.HistorySummary != nil:
			continue
		case node.Type == 1 && node.ToolResultNode != nil:
			toolResults = append(toolResults, node.ToolResultNode)
		default:
			otherNodes = append(otherNodes, node)
		}
	}

	text := renderHistorySummaryNodeValue(summaryNode.HistorySummary, toolResults)
	if strings.TrimSpace(text) != "" {
		otherNodes = append([]Node{{
			Type:     0,
			TextNode: &TextNode{Content: text},
		}}, otherNodes...)
	}

	entry.RequestNodes = otherNodes
	entry.StructuredRequestNodes = nil
	entry.Nodes = nil
	return entry
}

func renderHistorySummaryNodeValue(node *HistorySummaryNode, extraToolResults []*ToolResultNode) string {
	if node == nil {
		return ""
	}

	template := strings.TrimSpace(firstNonEmpty(
		node.MessageTemplate,
		node.MessageTemplateAlt,
	))
	if template == "" {
		return ""
	}

	historyEnd := normalizeHistorySummaryEnd(node)
	if len(extraToolResults) > 0 {
		historyEnd = append(historyEnd, ChatHistoryEntry{
			RequestNodes: buildHistorySummaryToolResultNodes(extraToolResults),
		})
	}

	endPartFull := renderHistorySummaryEnd(historyEnd)
	maxChars := firstPositiveInt(node.EndPartFullMaxChars, node.EndPartFullMaxCharsAlt)
	tailChars := firstPositiveInt(node.EndPartFullTailChars, node.EndPartFullTailCharsAlt)
	if maxChars > 0 {
		if tailChars <= 0 {
			tailChars = historySummaryDefaultEndPartTailChars
		}
		endPartFull = truncateMiddleWithTail(endPartFull, maxChars, tailChars)
	}

	replacements := map[string]string{
		"{summary}":                              firstNonEmpty(node.SummaryText, node.SummaryTextAlt),
		"{summarization_request_id}":             firstNonEmpty(node.SummarizationRequestID, node.SummarizationRequestIDAlt),
		"{beginning_part_dropped_num_exchanges}": fmt.Sprintf("%d", firstPositiveInt(node.HistoryBeginningDroppedNumExchanges, node.HistoryBeginningDroppedNumExchangesAlt)),
		"{middle_part_abridged}":                 firstNonEmpty(node.HistoryMiddleAbridgedText, node.HistoryMiddleAbridgedTextAlt),
		"{end_part_full}":                        endPartFull,
	}

	rendered := template
	for placeholder, value := range replacements {
		rendered = strings.ReplaceAll(rendered, placeholder, value)
	}
	return strings.TrimSpace(rendered)
}

func normalizeHistorySummaryEnd(node *HistorySummaryNode) []ChatHistoryEntry {
	if node == nil {
		return nil
	}
	rawItems := node.HistoryEnd
	if len(rawItems) == 0 {
		rawItems = node.HistoryEndAlt
	}
	if len(rawItems) == 0 {
		return nil
	}

	out := make([]ChatHistoryEntry, 0, len(rawItems))
	for _, item := range rawItems {
		out = append(out, ChatHistoryEntry{
			RequestMessage: firstString(item, "request_message", "requestMessage"),
			ResponseText:   firstString(item, "response_text", "responseText"),
			RequestNodes:   normalizeNodes(firstArray(item, "request_nodes", "requestNodes")),
			ResponseNodes:  normalizeNodes(firstArray(item, "response_nodes", "responseNodes")),
		})
	}
	return out
}

func buildHistorySummaryToolResultNodes(results []*ToolResultNode) []Node {
	if len(results) == 0 {
		return nil
	}
	nodes := make([]Node, 0, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		nodes = append(nodes, Node{
			Type:           1,
			ToolResultNode: result,
		})
	}
	return nodes
}

func renderHistorySummaryEnd(entries []ChatHistoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		parts = append(parts, renderHistorySummaryExchange(entry))
	}
	return strings.Join(parts, "\n")
}

func renderHistorySummaryExchange(entry ChatHistoryEntry) string {
	ctx := buildHistorySummaryExchangeCtx(entry)
	lines := []string{
		"<exchange>",
		"  <user_request_or_tool_results>",
		ctx.UserMessage,
	}
	for _, tr := range ctx.ToolResults {
		lines = append(lines,
			fmt.Sprintf("    <tool_result tool_use_id=\"%s\" is_error=\"%t\">", tr.ID, tr.IsError),
			tr.Content,
			"    </tool_result>",
		)
	}
	lines = append(lines, "  </user_request_or_tool_results>")
	if ctx.HasResponse {
		lines = append(lines, "  <agent_response_or_tool_uses>")
		if ctx.Thinking != "" {
			lines = append(lines,
				"    <thinking>",
				ctx.Thinking,
				"    </thinking>",
			)
		}
		lines = append(lines, ctx.ResponseText)
		for _, tu := range ctx.ToolUses {
			lines = append(lines,
				fmt.Sprintf("    <tool_use name=\"%s\" tool_use_id=\"%s\">", tu.Name, tu.ID),
				tu.Input,
				"    </tool_use>",
			)
		}
		lines = append(lines, "  </agent_response_or_tool_uses>")
	}
	lines = append(lines, "</exchange>")
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

type historySummaryExchangeCtx struct {
	UserMessage  string
	ToolResults  []historySummaryToolResult
	HasResponse  bool
	Thinking     string
	ResponseText string
	ToolUses     []historySummaryToolUse
}

type historySummaryToolResult struct {
	ID      string
	Content string
	IsError bool
}

type historySummaryToolUse struct {
	Name  string
	ID    string
	Input string
}

func buildHistorySummaryExchangeCtx(entry ChatHistoryEntry) historySummaryExchangeCtx {
	ctx := historySummaryExchangeCtx{
		UserMessage: extractHistorySummaryUserMessage(entry),
	}

	for _, node := range entry.EffectiveRequestNodes() {
		if node.Type != 1 || node.ToolResultNode == nil {
			continue
		}
		id := strings.TrimSpace(node.ToolResultNode.EffectiveToolUseID())
		if id == "" {
			continue
		}
		content := strings.TrimSpace(stringifyToolResultContent(buildOpenAIToolResultContent(node.ToolResultNode)))
		content = truncateMiddleWithTail(content, historySummaryToolResultMaxChars, historySummaryToolResultTailChars)
		ctx.ToolResults = append(ctx.ToolResults, historySummaryToolResult{
			ID:      id,
			Content: content,
			IsError: node.ToolResultNode.IsError,
		})
	}

	var thinkingParts []string
	for _, node := range entry.EffectiveResponseNodes() {
		switch node.Type {
		case 8:
			if node.Thinking != nil {
				if text := strings.TrimSpace(node.Thinking.Summary); text != "" {
					thinkingParts = append(thinkingParts, text)
				}
			}
		case 5, 7:
			if node.ToolUse != nil {
				if node.Type == 7 && hasCompletedToolUse(entry.EffectiveResponseNodes()) {
					continue
				}
				id := strings.TrimSpace(node.ToolUse.ToolUseID)
				name := strings.TrimSpace(node.ToolUse.ToolName)
				if id == "" || name == "" {
					continue
				}
				input := strings.TrimSpace(node.ToolUse.InputJSON)
				if input == "" {
					input = "{}"
				}
				ctx.ToolUses = append(ctx.ToolUses, historySummaryToolUse{
					Name:  name,
					ID:    id,
					Input: truncateMiddleWithTail(input, historySummaryToolUseInputMaxChars, historySummaryToolUseInputTailChars),
				})
			}
		}
	}

	ctx.Thinking = strings.Join(thinkingParts, "\n")
	ctx.ResponseText = assistantResponseText(entry.ResponseText, entry.EffectiveResponseNodes())
	ctx.HasResponse = ctx.Thinking != "" || ctx.ResponseText != "" || len(ctx.ToolUses) > 0
	return ctx
}

func extractHistorySummaryUserMessage(entry ChatHistoryEntry) string {
	var parts []string
	for _, node := range entry.EffectiveRequestNodes() {
		if node.Type == 0 && node.TextNode != nil {
			if text := strings.TrimSpace(node.TextNode.EffectiveContent()); text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return strings.TrimSpace(entry.RequestMessage)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func truncateMiddleWithTail(text string, maxChars int, tailChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if tailChars < 0 {
		tailChars = 0
	}
	if tailChars > maxChars-1 {
		tailChars = maxChars - 1
	}
	headChars := maxChars - 1 - tailChars
	if headChars < 0 {
		headChars = 0
	}
	if headChars+tailChars >= len(text) {
		return text
	}
	return text[:headChars] + "…" + text[len(text)-tailChars:]
}
