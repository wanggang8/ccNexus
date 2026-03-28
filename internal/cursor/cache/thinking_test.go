package cache

import (
	"fmt"
	"testing"
	"time"
)

func TestThinkingCacheInjectsStoredReasoning(t *testing.T) {
	cache := NewThinkingCache()
	messages := []map[string]interface{}{
		{"role": "user", "content": "hi"},
		{"role": "assistant", "content": "hello"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "continue"},
	}
	cache.StoreFromResponse(messages, map[string]interface{}{"reasoning_content": "cached think"})

	injected := cache.Inject(messages)
	if injected[2]["reasoning_content"] != "cached think" {
		t.Fatalf("expected cached reasoning to be injected, got %#v", injected[2]["reasoning_content"])
	}
}

func TestThinkingCacheCleanupRemovesExpiredEntries(t *testing.T) {
	cache := NewThinkingCache()
	cache.Store["expired"] = Entry{
		Reasoning: "old",
		StoredAt:  time.Now().Add(-(TTL + time.Hour)),
	}
	for i := 0; i < 100; i++ {
		cache.Store[fmt.Sprintf("keep-%03d", i)] = Entry{
			Reasoning: "keep",
			StoredAt:  time.Now(),
		}
	}

	cache.cleanup()

	if _, ok := cache.Store["expired"]; ok {
		t.Fatalf("expected expired entry to be removed")
	}
}
