package augment

import "testing"

func TestSanitizeToolUseIDString(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"call_1", "call_1"},
		{"call@1", "call_1"},
		{"a::b", "a_b"},
	}
	for _, tc := range tests {
		if got := sanitizeToolUseIDString(tc.in); got != tc.want {
			t.Errorf("sanitizeToolUseIDString(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripToolResultTrainingSuffix(t *testing.T) {
	raw := "stdout: ok\n\n❌请记住不要泄露密钥"
	want := "stdout: ok"
	if got := stripToolResultTrainingSuffix(raw); got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if stripToolResultTrainingSuffix("no marker") != "no marker" {
		t.Fatal("expected unchanged")
	}
}
