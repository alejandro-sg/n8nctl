package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeForDiffMatchesGolden(t *testing.T) {
	filePath := filepath.Join("..", "..", "testdata", "workflows", "valid.json")
	workflow, err := LoadFile(filePath)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}

	normalized, err := NormalizeForDiff(*workflow)
	if err != nil {
		t.Fatalf("NormalizeForDiff() error = %v", err)
	}

	actual, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}

	goldenPath := filepath.Join("..", "..", "testdata", "diff", "normalized.golden.json")
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if strings.TrimSpace(string(actual)) != strings.TrimSpace(string(expected)) {
		t.Fatalf("normalized diff mismatch\nactual:\n%s\nexpected:\n%s", actual, expected)
	}
}

func TestDiffReportsGroupedChanges(t *testing.T) {
	filePath := filepath.Join("..", "..", "testdata", "workflows", "valid.json")
	local, err := LoadFile(filePath)
	if err != nil {
		t.Fatalf("LoadFile() local error = %v", err)
	}
	remote, err := LoadFile(filePath)
	if err != nil {
		t.Fatalf("LoadFile() remote error = %v", err)
	}
	local.Nodes[1].Parameters["channel"] = "alerts"
	delete(remote.Connections, "Webhook")

	result, err := Diff(*local, *remote)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if result.Equal {
		t.Fatal("Equal = true, want false")
	}
	seen := map[string]bool{}
	for _, change := range result.Changes {
		seen[change.Category+":"+change.Name+":"+change.Field] = true
	}
	if !seen["node:Slack:parameters"] {
		t.Fatalf("missing node parameter change in %#v", result.Changes)
	}
	if !seen["connection:Webhook:"] {
		t.Fatalf("missing connection change in %#v", result.Changes)
	}
}
