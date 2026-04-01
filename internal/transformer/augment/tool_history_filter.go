package augment

import "strings"

// mergeToolUseIDsFromResponseNodes records tool_use ids emitted in assistant response
// nodes (type 5 / type 7 per hasCompletedToolUse), for matching later tool_result nodes.
func mergeToolUseIDsFromResponseNodes(allowed map[string]bool, respNodes []Node) {
	if allowed == nil {
		return
	}
	preferCompleted := hasCompletedToolUse(respNodes)
	for _, n := range respNodes {
		if n.ToolUse == nil {
			continue
		}
		if n.Type != 5 && !(n.Type == 7 && !preferCompleted) {
			continue
		}
		id := strings.TrimSpace(n.ToolUse.ToolUseID)
		if id != "" {
			allowed[id] = true
		}
	}
}

// filterRequestNodesToolResultsByAllowedIDs keeps valid tool_result nodes and degrades
// unmatched historical tool_result nodes into plain text so the payload remains
// valid without silently losing information.
//
// When allowed is empty (no tool_use seen yet in history), nodes are returned
// unchanged so orphan tool_result paths and repairClaude/repairOpenAI logic
// still apply.
func filterRequestNodesToolResultsByAllowedIDs(nodes []Node, allowed map[string]bool) []Node {
	if len(nodes) == 0 {
		return nil
	}
	if len(allowed) == 0 {
		return nodes
	}
	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Type == 1 && n.ToolResultNode != nil {
			id := strings.TrimSpace(n.ToolResultNode.EffectiveToolUseID())
			if id == "" || !allowed[id] {
				if text := strings.TrimSpace(buildHistoricalToolResultNodeText(n.ToolResultNode)); text != "" {
					out = append(out, Node{Type: 0, TextNode: &TextNode{Content: text}})
				}
				continue
			}
		}
		out = append(out, n)
	}
	return out
}
