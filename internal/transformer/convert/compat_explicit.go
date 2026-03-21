package convert

import (
	"encoding/json"
	"strings"

	"github.com/lich0821/ccNexus/internal/logger"
)

const thinkingPlaceholderText = "(thinking...)"

func joinTextParts(textParts []string) string {
	if len(textParts) == 0 {
		return ""
	}
	return strings.Join(textParts, "")
}

func placeholderForThinkingOnly() string {
	return thinkingPlaceholderText
}

func parseJSONObjectArguments(argsStr string, warnPrefix string) map[string]interface{} {
	if strings.TrimSpace(argsStr) == "" {
		return map[string]interface{}{}
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsStr), &args); err != nil {
		logger.Warn("%s: %v, using empty object", warnPrefix, err)
		return map[string]interface{}{}
	}
	if args == nil {
		return map[string]interface{}{}
	}
	return args
}
