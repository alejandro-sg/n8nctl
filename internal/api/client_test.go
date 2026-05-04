package api

import (
	"context"
	"strings"
	"testing"
)

func TestAllowedEndpoint(t *testing.T) {
	tests := []struct {
		method   string
		endpoint string
		want     bool
	}{
		{method: "GET", endpoint: "/workflows", want: true},
		{method: "PUT", endpoint: "/workflows/wf-1", want: true},
		{method: "POST", endpoint: "/workflows/wf-1/activate", want: true},
		{method: "POST", endpoint: "/executions/ex-1/retry", want: true},
		{method: "GET", endpoint: "/credentials/schema/googleApi", want: true},
		{method: "GET", endpoint: "/rest/workflows", want: false},
		{method: "PATCH", endpoint: "/workflows/wf-1", want: false},
		{method: "GET", endpoint: "/workflows/../projects", want: false},
	}

	for _, tt := range tests {
		if got := AllowedEndpoint(tt.method, tt.endpoint); got != tt.want {
			t.Fatalf("AllowedEndpoint(%q, %q) = %t, want %t", tt.method, tt.endpoint, got, tt.want)
		}
	}
}

func TestDoJSONDeniesUnallowlistedEndpointBeforeHTTP(t *testing.T) {
	client := NewClient("https://example.test", "test-key", nil)
	err := client.doJSON(context.Background(), "PATCH", "/workflows/wf-1", nil, nil, nil)
	if err == nil {
		t.Fatal("doJSON() error = nil, want allowlist denial")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Fatalf("doJSON() error = %v, want allowlist denial", err)
	}
}
