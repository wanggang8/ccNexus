package entry

import "testing"

func TestPrepareBuildsCursorMeta(t *testing.T) {
	prepared := Prepare("/cursor/v1/chat/completions", []byte(`{"model":"gpt-5","messages":[]}`))
	if !prepared.Meta.CursorMode {
		t.Fatalf("expected cursor mode")
	}
	if prepared.Meta.EffectivePath != "/v1/chat/completions" {
		t.Fatalf("expected stripped path, got %s", prepared.Meta.EffectivePath)
	}
	if prepared.Meta.ClientModel != "gpt-5" {
		t.Fatalf("expected client model gpt-5, got %s", prepared.Meta.ClientModel)
	}
}

func TestPrepareLeavesNonCursorPathUntouched(t *testing.T) {
	prepared := Prepare("/v1/responses", []byte(`{"model":"gpt-5"}`))
	if prepared.Meta.CursorMode {
		t.Fatalf("expected non-cursor mode")
	}
	if prepared.Meta.EffectivePath != "/v1/responses" {
		t.Fatalf("expected original path preserved, got %s", prepared.Meta.EffectivePath)
	}
}
