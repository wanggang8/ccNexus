package stream

import (
	"testing"

	"github.com/lich0821/ccNexus/internal/cursor/shared"
)

func TestFixNoopForNonCursor(t *testing.T) {
	body := []byte("data: x\n\n")
	updated, err := Fix(shared.RequestMeta{}, body, func(bundle []byte) ([]byte, error) {
		t.Fatalf("unexpected hook call")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Fix failed: %v", err)
	}
	if string(updated) != string(body) {
		t.Fatalf("expected passthrough bundle, got %s", updated)
	}
}
