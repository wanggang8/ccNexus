package augment

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// supportsThinking checks if the model supports extended/interleaved thinking.
// Claude 4+ series (sonnet-4, opus-4, haiku-4.5, sonnet-4.5, etc.) support thinking.
// Claude 3.x and earlier do not.
func supportsThinking(model string) bool {
	m := strings.ToLower(model)

	// Claude 4+ model patterns
	thinkingPrefixes := []string{
		"claude-sonnet-4",
		"claude-opus-4",
		"claude-haiku-4",
		"claude-4",
	}
	for _, prefix := range thinkingPrefixes {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}

	// Aliases that map to Claude 4+ models
	thinkingAliases := []string{
		"claude-sonnet", // latest sonnet = 4.x
		"claude-opus",   // latest opus = 4.x
		"claude-haiku",  // latest haiku = 4.x
	}
	for _, alias := range thinkingAliases {
		if m == alias {
			return true
		}
	}

	return false
}

var (
	cliMetadata     *CLIMetadata
	cliMetadataOnce sync.Once
)

// CLIMetadata holds stable user_id and session_id for CLI requests
type CLIMetadata struct {
	StableUserID string
	SessionID    string
}

// initCLIMetadata initializes CLI metadata (called once per process)
func initCLIMetadata() {
	cliMetadataOnce.Do(func() {
		// Generate stable user_id based on hostname and username
		hostname, _ := os.Hostname()
		username := os.Getenv("USER")
		if username == "" {
			username = os.Getenv("USERNAME") // Windows
		}
		if username == "" {
			username = "unknown"
		}

		userInfo := fmt.Sprintf("%s_%s", hostname, username)
		hash := sha256.Sum256([]byte(userInfo))
		stableUserID := hex.EncodeToString(hash[:])

		// Generate process-level session_id
		sessionBytes := make([]byte, 16)
		rand.Read(sessionBytes)
		sessionID := hex.EncodeToString(sessionBytes)

		cliMetadata = &CLIMetadata{
			StableUserID: stableUserID,
			SessionID:    sessionID,
		}
	})
}

// toCliRequest converts an AugmentRequest to Claude Code CLI format.
// This is similar to toClaudeRequest but adds CLI-specific metadata and system prompt.
func toCliRequest(ar *AugmentRequest) ([]byte, error) {
	initCLIMetadata()

	messages, currentMessageCount := buildClaudeMessagesWithCurrentCount(ar)
	tools := buildClaudeTools(ar.EffectiveTools())
	system := buildCliSystem(ar)
	maxTokens := effectiveMaxTokens(ar.MaxTokens)

	// Prompt Caching — three levels (tools → system → last history message).
	if len(tools) > 0 {
		setClaudeCacheControl(tools[len(tools)-1])
	}
	if len(system) > 0 {
		setClaudeCacheControlBlock(system[0])
	}
	if histEnd := len(messages) - currentMessageCount; histEnd > 0 {
		addCacheControlToMessage(messages[histEnd-1])
	}

	req := map[string]interface{}{
		"model":      ar.Model,
		"max_tokens": maxTokens,
		"stream":     ar.IsStreaming(),
		"messages":   messages,
		"metadata": map[string]interface{}{
			"user_id": fmt.Sprintf("user_%s_account__session_%s", cliMetadata.StableUserID, cliMetadata.SessionID),
		},
	}
	if len(tools) > 0 {
		req["tools"] = tools
	}
	if len(system) > 0 {
		req["system"] = system
	}

	// T4: Enable thinking when explicitly requested, or fall back to model-based
	// defaults for Claude Code compatibility.
	if thinking := buildClaudeThinkingConfig(ar.Thinking, ar.EnableThinking, ar.Model, maxTokens, true); thinking != nil {
		req["thinking"] = thinking
	}

	req = sanitizeProviderRequest("cli", req)
	ensureClaudeFirstMessageIsUser(req)
	return json.Marshal(req)
}

// buildCliSystem builds the system content array with CLI-specific prompt.
func buildCliSystem(ar *AugmentRequest) []map[string]interface{} {
	system := []map[string]interface{}{
		{
			"type":          "text",
			"text":          "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
	}

	if commonText := buildCommonSystemText(ar); commonText != "" {
		system = append(system, map[string]interface{}{
			"type": "text",
			"text": commonText,
		})
	}

	return system
}
