package response

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestFixNoopForNonCursor(t *testing.T) {
	body := []byte(`{"ok":true}`)
	updated, err := Fix(shared.RequestMeta{}, body, Hooks{
		FixChat: func(body []byte, clientModel string) ([]byte, error) {
			t.Fatalf("unexpected hook call")
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("Fix failed: %v", err)
	}
	if string(updated) != string(body) {
		t.Fatalf("expected body passthrough, got %s", updated)
	}
}

func TestFixDispatchesByClientFormat(t *testing.T) {
	meta := shared.RequestMeta{
		CursorMode:   true,
		ClientFormat: shared.ClientFormatOpenAIResponses,
		ClientModel:  "gpt-5",
	}
	updated, err := Fix(meta, []byte(`{}`), Hooks{
		FixResponses: func(body []byte, clientModel string) ([]byte, error) {
			if clientModel != "gpt-5" {
				t.Fatalf("expected client model gpt-5, got %s", clientModel)
			}
			return []byte(`{"fixed":true}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Fix failed: %v", err)
	}
	if string(updated) != `{"fixed":true}` {
		t.Fatalf("expected dispatched response hook output, got %s", updated)
	}
}
