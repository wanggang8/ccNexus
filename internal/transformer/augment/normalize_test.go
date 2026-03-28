package augment

import "testing"

func TestParseRequest_NormalizesAliasesAndHistoryVariants(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5-codex",
		"prompt":"继续分析",
		"chatHistory":[
			{
				"requestMessage":"先看历史",
				"structuredOutputNodes":[
					{
						"type":5,
						"toolUse":{"toolName":"search","toolUseId":"call_1","inputJson":"{\"q\":\"augment\"}"}
					}
				]
			},
			{
				"requestNodes":[
					{
						"type":1,
						"toolResultNode":{
							"toolCallId":"call_1",
							"contentNodes":[{"type":"text","text":"docs found"}]
						}
					}
				]
			}
		],
		"structuredRequestNodes":[
			{"type":0,"textNode":{"text":"请继续"}}
		],
		"toolDefinitions":[
			{
				"name":"search",
				"description":"Search docs",
				"inputSchema":{"type":"object","properties":{"q":{"type":"string"}}},
				"mcpServerName":"web",
				"mcpToolName":"search_docs"
			}
		],
		"userGuidelines":"简洁回答",
		"workspaceGuidelines":"基于本地代码",
		"agentMemories":"之前已经确认使用 responses api"
	}`)

	req, err := ParseRequest(body)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}

	if req.Message != "继续分析" {
		t.Fatalf("expected prompt alias to populate message, got %q", req.Message)
	}
	if len(req.StructuredRequestNodes) != 1 || req.StructuredRequestNodes[0].TextNode == nil || req.StructuredRequestNodes[0].TextNode.EffectiveContent() != "请继续" {
		t.Fatalf("expected structured request nodes to be normalized, got %#v", req.StructuredRequestNodes)
	}
	if len(req.ChatHistory) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(req.ChatHistory))
	}
	respNodes := req.ChatHistory[0].EffectiveResponseNodes()
	if len(respNodes) != 1 || respNodes[0].ToolUse == nil || respNodes[0].ToolUse.ToolUseID != "call_1" {
		t.Fatalf("expected structuredOutputNodes alias to populate response nodes, got %#v", respNodes)
	}
	reqNodes := req.ChatHistory[1].EffectiveRequestNodes()
	if len(reqNodes) != 1 || reqNodes[0].ToolResultNode == nil || reqNodes[0].ToolResultNode.EffectiveToolUseID() != "call_1" {
		t.Fatalf("expected requestNodes alias to populate tool result, got %#v", reqNodes)
	}
	if len(req.ToolDefinitions) != 1 {
		t.Fatalf("expected tool definitions to normalize, got %d", len(req.ToolDefinitions))
	}
	if req.ToolDefinitions[0].McpServerName != "web" || req.ToolDefinitions[0].McpToolName != "search_docs" {
		t.Fatalf("expected camelCase MCP fields to normalize, got %#v", req.ToolDefinitions[0])
	}
	if req.UserGuidelines != "简洁回答" || req.WorkspaceGuidelines != "基于本地代码" || req.AgentMemories == "" {
		t.Fatalf("expected guidelines and memories to normalize, got %#v", req)
	}
}

func TestParseRequest_NormalizesSpecialNodesAndSystemFields(t *testing.T) {
	body := []byte(`{
		"personaType":3,
		"byokSystemPrompt":"extra system",
		"requestIdOverride":"req-override-1",
		"nodes":[
			{"type":3,"imageIdNode":{"imageId":"img_1","format":4}},
			{"type":5,"editEventsNode":{"source":"editor","editEvents":[{"path":"a.go","edits":[{"afterLineStart":12,"beforeLineStart":10,"beforeText":"old","afterText":"new"}]}]}},
			{"type":6,"checkpointRefNode":{"requestId":"req_1","fromTimestamp":1,"toTimestamp":2,"source":"history"}},
			{"type":7,"changePersonalityNode":{"personalityType":2,"customInstructions":"think broad"}},
			{"type":8,"fileNode":{"fileData":"SGVsbG8=","format":"text/plain"}},
			{"type":9,"fileIdNode":{"fileId":"file_1","fileName":"demo.txt"}}
		],
		"chatHistory":[
			{"structuredOutputNodes":[
				{"type":0,"content":"partial"},
				{"type":2,"content":"final"},
				{"type":7,"toolUse":{"toolUseId":"call_1","toolName":"search","inputJson":"{}"}}
			]}
		]
	}`)

	req, err := ParseRequest(body)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if req.PersonaType != 3 || req.ByokSystemPrompt != "extra system" {
		t.Fatalf("expected system aliases to normalize, got persona=%d byok=%q", req.PersonaType, req.ByokSystemPrompt)
	}
	if req.RequestIDOverride != "req-override-1" {
		t.Fatalf("expected requestIdOverride alias to normalize, got %q", req.RequestIDOverride)
	}
	nodes := req.EffectiveCurrentNodes()
	if len(nodes) != 6 {
		t.Fatalf("expected 6 special nodes, got %d", len(nodes))
	}
	if nodes[0].ImageIDNode == nil || nodes[1].EditEventsNode == nil || nodes[2].CheckpointRef == nil || nodes[3].Personality == nil || nodes[4].FileNode == nil || nodes[5].FileIDNode == nil {
		t.Fatalf("expected special nodes to normalize, got %#v", nodes)
	}
	respNodes := req.ChatHistory[0].EffectiveResponseNodes()
	if len(respNodes) != 3 {
		t.Fatalf("expected 3 response nodes, got %d", len(respNodes))
	}
	if respNodes[1].TextNode == nil || respNodes[1].TextNode.EffectiveContent() != "final" {
		t.Fatalf("expected type=2 main_text_finished to normalize as text, got %#v", respNodes[1])
	}
	if respNodes[2].ToolUse == nil || respNodes[2].ToolUse.ToolName != "search" {
		t.Fatalf("expected type=7 response tool_use_start to normalize as tool use, got %#v", respNodes[2])
	}
}
