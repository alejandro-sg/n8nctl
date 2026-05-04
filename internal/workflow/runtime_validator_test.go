package workflow

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestValidateWithRuntimeFindsSetupIssues(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for runtime validator test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.json")
	body := []byte(`{
  "name": "Runtime Issues",
  "nodes": [
    {
      "id": "node-1",
      "name": "HTTP",
      "type": "n8n-nodes-base.httpRequest",
      "typeVersion": 1,
      "parameters": {
        "authentication": "genericCredentialType"
      }
    },
    {
      "id": "node-2",
      "name": "Child",
      "type": "n8n-nodes-base.executeWorkflow",
      "typeVersion": 1,
      "parameters": {}
    }
  ],
  "connections": {
    "Missing": {
      "main": [[{"node": "Child", "type": "main", "index": 0}]]
    }
  },
  "settings": {}
}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := ValidateWithRuntime(path)
	if err != nil {
		t.Fatalf("ValidateWithRuntime() error = %v", err)
	}
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[finding.Code] = true
	}
	for _, code := range []string{
		"missing_required_parameter",
		"missing_subworkflow",
		"connection_from_missing_node",
	} {
		if !seen[code] {
			t.Fatalf("missing finding %q in %#v", code, findings)
		}
	}
}

func TestValidateWithRuntimeAllowsGoogleDriveServiceAccount(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is required for runtime validator test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "workflow.json")
	body := []byte(`{
  "name": "Service Account Drive",
  "nodes": [
    {
      "id": "node-1",
      "name": "Resolve Shared Drive",
      "type": "n8n-nodes-base.googleDrive",
      "typeVersion": 3,
      "parameters": {},
      "credentials": {
        "googleApi": {
          "id": "cred-service-account",
          "name": "svc-talent-mapping"
        }
      }
    }
  ],
  "connections": {},
  "settings": {}
}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	findings, err := ValidateWithRuntime(path)
	if err != nil {
		t.Fatalf("ValidateWithRuntime() error = %v", err)
	}
	for _, finding := range findings {
		if finding.Code == "missing_required_credential" {
			t.Fatalf("unexpected missing credential finding: %#v", finding)
		}
	}
}
