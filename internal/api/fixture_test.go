package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alejandro-sg/n8nctl/pkg/n8n"
)

func TestWorkflowWritePayloadStripsReadOnlyFixtureFields(t *testing.T) {
	workflow := readAPIWorkflowFixture(t)

	payload, err := workflowWritePayload(workflow, false)
	if err != nil {
		t.Fatalf("workflowWritePayload() error = %v", err)
	}

	for _, field := range []string{
		"active",
		"id",
		"versionId",
		"projectId",
		"staticData",
		"pinData",
		"meta",
	} {
		if _, ok := payload[field]; ok {
			t.Fatalf("payload contains read-only field %q: %#v", field, payload)
		}
	}
	if payload["name"] != "Fixture Workflow" {
		t.Fatalf("payload name = %#v", payload["name"])
	}
}

func TestClientFixtureDeployActivateAndTransferFailure(t *testing.T) {
	workflow := readAPIWorkflowFixture(t)
	var updatePayload map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/workflows/wf-fixture":
			if err := json.NewDecoder(r.Body).Decode(&updatePayload); err != nil {
				t.Fatalf("decode update payload: %v", err)
			}
			workflow.Name = updatePayload["name"].(string)
			_ = json.NewEncoder(w).Encode(workflow)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/workflows/wf-fixture/activate":
			workflow.Active = true
			_ = json.NewEncoder(w).Encode(workflow)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/workflows/wf-fixture/transfer":
			w.WriteHeader(http.StatusNotImplemented)
			writeFixture(t, w, filepath.Join("..", "..", "testdata", "api", "api_error.json"))
		default:
			http.Error(w, `{"message":"unexpected request"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", server.Client())
	updated, err := client.UpdateWorkflow(context.Background(), "wf-fixture", workflow)
	if err != nil {
		t.Fatalf("UpdateWorkflow() error = %v", err)
	}
	if updated.ID.String() != "wf-fixture" {
		t.Fatalf("updated ID = %q", updated.ID.String())
	}
	if _, ok := updatePayload["active"]; ok {
		t.Fatalf("update payload leaked active: %#v", updatePayload)
	}

	activated, err := client.ActivateWorkflow(context.Background(), "wf-fixture")
	if err != nil {
		t.Fatalf("ActivateWorkflow() error = %v", err)
	}
	if !activated.Active {
		t.Fatalf("activated.Active = false")
	}

	err = client.TransferWorkflow(context.Background(), "wf-fixture", "proj-other", true)
	if err == nil {
		t.Fatal("TransferWorkflow() error = nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("TransferWorkflow() error = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotImplemented || !strings.Contains(apiErr.Message, "transfer is unavailable") {
		t.Fatalf("apiErr = %#v", apiErr)
	}
}

func readAPIWorkflowFixture(t *testing.T) n8n.Workflow {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "workflow_full.json"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow n8n.Workflow
	if err := json.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func writeFixture(t *testing.T, w http.ResponseWriter, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(body)
}
