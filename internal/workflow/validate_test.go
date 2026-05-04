package workflow

import (
	"path/filepath"
	"testing"
)

func TestValidateValidWorkflow(t *testing.T) {
	filePath := filepath.Join("..", "..", "testdata", "workflows", "valid.json")
	_, result, err := ValidateFile(filePath, ValidationOptions{})
	if err != nil {
		t.Fatalf("ValidateFile() error = %v", err)
	}
	if result.HasErrors() {
		t.Fatalf("HasErrors() = true, want false: %#v", result.Findings)
	}
	if result.NodeCount != 2 {
		t.Fatalf("NodeCount = %d, want 2", result.NodeCount)
	}
	if result.CredentialReferences != 1 {
		t.Fatalf("CredentialReferences = %d, want 1", result.CredentialReferences)
	}
}

func TestValidateInvalidWorkflow(t *testing.T) {
	filePath := filepath.Join("..", "..", "testdata", "workflows", "invalid.json")
	_, result, err := ValidateFile(filePath, ValidationOptions{
		EnvironmentName: "dev",
		ProductionHosts: []string{"https://company-prod.app.n8n.cloud"},
	})
	if err != nil {
		t.Fatalf("ValidateFile() error = %v", err)
	}
	if !result.HasErrors() {
		t.Fatal("HasErrors() = false, want true")
	}

	seen := map[string]bool{}
	for _, finding := range result.Findings {
		seen[finding.Code] = true
	}
	for _, code := range []string{
		"active_not_allowed",
		"missing_connections",
		"missing_settings",
		"duplicate_node_name",
		"malformed_placeholder",
		"suspicious_credential_reference",
		"hardcoded_secret",
		"production_url_in_nonprod",
	} {
		if !seen[code] {
			t.Fatalf("missing finding %q in %#v", code, result.Findings)
		}
	}
}
