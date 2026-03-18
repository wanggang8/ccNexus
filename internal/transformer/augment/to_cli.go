package augment

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

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

	messages := buildClaudeMessages(ar)
	tools := buildClaudeTools(ar.EffectiveTools())
	system := buildCliSystem(ar.UserGuidelines)

	// Prompt Caching — three levels (tools → system → last history message).
	if len(tools) > 0 {
		setClaudeCacheControl(tools[len(tools)-1])
	}
	if len(system) > 0 {
		setClaudeCacheControlBlock(system[0])
	}
	if histEnd := len(messages) - countCurrentMessages(ar); histEnd > 0 {
		addCacheControlToMessage(messages[histEnd-1])
	}

	req := map[string]interface{}{
		"model":      ar.Model,
		"max_tokens": effectiveMaxTokens(ar.MaxTokens),
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

	// T4: Enable interleaved thinking for CLI mode (supported via anthropic-beta header)
	req["thinking"] = map[string]interface{}{
		"type":          "enabled",
		"budget_tokens": 10000,
	}

	return json.Marshal(req)
}

// buildCliSystem builds the system content array with CLI-specific prompt.
func buildCliSystem(userGuidelines string) []map[string]interface{} {
	system := []map[string]interface{}{
		{
			"type":          "text",
			"text":          "You are Claude Code, Anthropic's official CLI for Claude, running within the Claude Agent SDK.",
			"cache_control": map[string]interface{}{"type": "ephemeral"},
		},
	}

	if userGuidelines != "" {
		system = append(system, map[string]interface{}{
			"type": "text",
			"text": userGuidelines,
		})
	}

	return system
}
