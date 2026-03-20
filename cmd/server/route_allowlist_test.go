package main

import "testing"

func TestIsAllowedPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "proxy messages", path: "/v1/messages", want: true},
		{name: "proxy chat completions", path: "/v1/chat/completions", want: true},
		{name: "proxy responses", path: "/v1/responses", want: true},
		{name: "proxy root", path: "/", want: true},
		{name: "proxy health", path: "/health", want: true},
		{name: "ui redirect path", path: "/ui", want: true},
		{name: "ui subtree", path: "/ui/index.html", want: true},
		{name: "api exact", path: "/api/auth/status", want: true},
		{name: "api subtree", path: "/api/endpoints/custom", want: true},
		{name: "blocked login", path: "/login", want: false},
		{name: "blocked favicon", path: "/favicon.ico", want: false},
		{name: "near miss does not match prefix", path: "/api/endpointsx", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isAllowedPath(tt.path); got != tt.want {
				t.Fatalf("isAllowedPath(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

