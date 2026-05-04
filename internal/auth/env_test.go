package auth

import (
	"testing"

	"github.com/LogicMonitor-IT/n8nctl/internal/config"
	clierrors "github.com/LogicMonitor-IT/n8nctl/internal/errors"
)

func TestResolveAPIKey(t *testing.T) {
	key, err := ResolveAPIKey(func(name string) string {
		if name == "N8N_DEV_API_KEY" {
			return "secret"
		}
		return ""
	}, "dev", config.Environment{APIKeyEnv: "N8N_DEV_API_KEY"})
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if key != "secret" {
		t.Fatalf("key = %q, want secret", key)
	}
}

func TestResolveAPIKeyMissingReturnsSafetyError(t *testing.T) {
	_, err := ResolveAPIKey(func(string) string { return "" }, "dev", config.Environment{APIKeyEnv: "N8N_DEV_API_KEY"})
	if err == nil {
		t.Fatal("ResolveAPIKey() error = nil, want error")
	}
	if clierrors.Code(err) != clierrors.CodeAPIKeyMissing {
		t.Fatalf("code = %q, want %q", clierrors.Code(err), clierrors.CodeAPIKeyMissing)
	}
	details := clierrors.Details(err)
	if details["summary"] == "" {
		t.Fatalf("details missing summary: %#v", details)
	}
	if details["instructions"] == nil {
		t.Fatalf("details missing instructions: %#v", details)
	}
}

func TestResolveAPIKeyRejectsUnresolvedOnePasswordReference(t *testing.T) {
	_, err := ResolveAPIKey(func(name string) string {
		if name == "N8N_PROD_API_KEY" {
			return "op://Employee/n8nctl-api-key-prod/credential"
		}
		return ""
	}, "prod", config.Environment{APIKeyEnv: "N8N_PROD_API_KEY"})
	if err == nil {
		t.Fatal("ResolveAPIKey() error = nil, want error")
	}
	if clierrors.Code(err) != clierrors.CodeAPIKeyMissing {
		t.Fatalf("code = %q, want %q", clierrors.Code(err), clierrors.CodeAPIKeyMissing)
	}
	if problem := clierrors.Details(err)["problem"]; problem != "op_reference_not_resolved" {
		t.Fatalf("problem = %v, want op_reference_not_resolved", problem)
	}
}

func TestResolveAPIKeyUsesAlias(t *testing.T) {
	key, err := ResolveAPIKey(func(name string) string {
		if name == "TEAM_N8N_KEY" {
			return "alias-secret"
		}
		return ""
	}, "dev", config.Environment{APIKeyEnv: "N8N_DEV_API_KEY", APIKeyEnvAliases: []string{"TEAM_N8N_KEY"}})
	if err != nil {
		t.Fatalf("ResolveAPIKey() error = %v", err)
	}
	if key != "alias-secret" {
		t.Fatalf("key = %q, want alias-secret", key)
	}
}
