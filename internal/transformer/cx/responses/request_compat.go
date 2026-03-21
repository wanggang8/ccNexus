package responses

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer/compat"
)

func transformResponsesRequestByShape(req []byte, model string, target string, convertResponses func([]byte, string) ([]byte, error), convertClaude func([]byte, string) ([]byte, error), convertOpenAIChat func([]byte, string) ([]byte, error)) ([]byte, error) {
	srcMap, err := compat.DecodeJSONMap(req)
	if err != nil {
		return req, nil
	}

	shape := compat.DetectRequestShapeMap(srcMap)
	switch shape {
	case compat.RequestShapeClaudeMessages:
		logger.Debug("[responses compat] request shape=%s target=%s", shape, target)
		converted, err := convertClaude(req, model)
		if err != nil {
			return nil, err
		}
		return applyResponsesTargetFieldPolicy(converted, srcMap, target, shape)
	case compat.RequestShapeOpenAIChat:
		logger.Debug("[responses compat] request shape=%s target=%s", shape, target)
		compat.NormalizeOpenAIChatCompat(srcMap)
		if target == "openai" {
			compat.OverrideModel(srcMap, model)
			audit := compat.ApplyOpenAIFieldPolicy(srcMap, srcMap)
			logResponsesCompatAudit(target, shape, audit)
			return compat.MustJSON(srcMap)
		}
		chatReq, err := compat.MustJSON(srcMap)
		if err != nil {
			return nil, err
		}
		converted, err := convertOpenAIChat(chatReq, model)
		if err != nil {
			return nil, err
		}
		return applyResponsesTargetFieldPolicy(converted, srcMap, target, shape)
	default:
		if target == "openai2" {
			return normalizeResponsesPassthrough(req, srcMap, model, shape)
		}
		converted, err := convertResponses(req, model)
		if err != nil {
			return nil, err
		}
		return applyResponsesTargetFieldPolicy(converted, srcMap, target, shape)
	}
}

func normalizeResponsesPassthrough(req []byte, srcMap map[string]interface{}, model string, shape compat.RequestShape) ([]byte, error) {
	changed := false
	if model != "" && srcMap["model"] != model {
		srcMap["model"] = model
		changed = true
	}
	if _, ok := srcMap["thinking"]; ok {
		changed = true
	}
	if _, ok := srcMap["enable_thinking"]; ok {
		changed = true
	}
	if _, ok := srcMap["budget_tokens"]; ok {
		changed = true
	}
	audit := compat.ApplyResponsesFieldPolicy(srcMap, srcMap)
	if audit.Changed {
		changed = true
	}
	logResponsesCompatAudit("openai2", shape, audit)
	if !changed {
		return req, nil
	}
	return compat.MustJSON(srcMap)
}

func applyResponsesTargetFieldPolicy(req []byte, src map[string]interface{}, target string, shape compat.RequestShape) ([]byte, error) {
	dst, err := compat.DecodeJSONMap(req)
	if err != nil {
		return req, nil
	}

	var audit compat.Audit
	switch target {
	case "openai":
		audit = compat.ApplyOpenAIFieldPolicy(dst, src)
	case "claude":
		audit = compat.ApplyClaudeFieldPolicy(dst, src)
	case "openai2":
		audit = compat.ApplyResponsesFieldPolicy(dst, src)
	}
	logResponsesCompatAudit(target, shape, audit)
	return json.Marshal(dst)
}

func logResponsesCompatAudit(target string, shape compat.RequestShape, audit compat.Audit) {
	if !audit.Changed {
		return
	}
	logger.Debug("[responses compat] target=%s shape=%s reason=%s summary=%v", target, shape, audit.Reason, audit.Summary)
}
