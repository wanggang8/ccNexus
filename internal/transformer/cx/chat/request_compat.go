package chat

import (
	"encoding/json"

	"github.com/lich0821/ccNexus/internal/logger"
	"github.com/lich0821/ccNexus/internal/transformer/compat"
)

func transformChatRequestByShape(req []byte, model string, target string, convertOpenAI func([]byte, string) ([]byte, error), convertClaude func([]byte, string) ([]byte, error), convertResponses func([]byte, string) ([]byte, error)) ([]byte, error) {
	srcMap, err := compat.DecodeJSONMap(req)
	if err != nil {
		return req, nil
	}

	shape := compat.DetectRequestShapeMap(srcMap)
	switch shape {
	case compat.RequestShapeClaudeMessages:
		logger.Debug("[chat compat] request shape=%s target=%s", shape, target)
		if target == "claude" {
			compat.OverrideModel(srcMap, model)
			audit := compat.ApplyClaudeFieldPolicy(srcMap, srcMap)
			logCompatAudit(target, shape, audit)
			return compat.MustJSON(srcMap)
		}
		converted, err := convertClaude(req, model)
		if err != nil {
			return nil, err
		}
		return applyTargetFieldPolicy(converted, srcMap, target, shape)
	case compat.RequestShapeOpenAIResponses:
		logger.Debug("[chat compat] request shape=%s target=%s", shape, target)
		if target == "openai2" {
			compat.OverrideModel(srcMap, model)
			audit := compat.ApplyResponsesFieldPolicy(srcMap, srcMap)
			logCompatAudit(target, shape, audit)
			return compat.MustJSON(srcMap)
		}
		converted, err := convertResponses(req, model)
		if err != nil {
			return nil, err
		}
		return applyTargetFieldPolicy(converted, srcMap, target, shape)
	default:
		converted, err := convertOpenAI(req, model)
		if err != nil {
			return nil, err
		}
		if shape == compat.RequestShapeOpenAIChat {
			return applyTargetFieldPolicy(converted, srcMap, target, shape)
		}
		return converted, nil
	}
}

func applyTargetFieldPolicy(req []byte, src map[string]interface{}, target string, shape compat.RequestShape) ([]byte, error) {
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
	logCompatAudit(target, shape, audit)
	return json.Marshal(dst)
}

func logCompatAudit(target string, shape compat.RequestShape, audit compat.Audit) {
	if !audit.Changed {
		return
	}
	logger.Debug("[chat compat] target=%s shape=%s reason=%s summary=%v", target, shape, audit.Reason, audit.Summary)
}
