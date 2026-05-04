package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LogicMonitor-IT/n8nctl/internal/buildinfo"
	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

func TestInitCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	exitCode := ExecuteWithArgs([]string{"init"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv: func(string) string {
			return ""
		},
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".n8nctl.yaml")); err != nil {
		t.Fatalf(".n8nctl.yaml not created: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dir, ".n8nctl.yaml"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(body), "default_env: prod") {
		t.Fatalf(".n8nctl.yaml = %s, want default_env: prod", string(body))
	}
}

func TestInitCreatesOnePasswordProjectFiles(t *testing.T) {
	dir := t.TempDir()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	exitCode := ExecuteWithArgs([]string{"init", "--with-1password", "--onepassword-vault", "Employee", "--onepassword-item-prefix", "n8nctl-api-key", "--json"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv:     func(string) string { return "" },
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	envPath := filepath.Join(dir, ".env.1password")
	loaderPath := filepath.Join(dir, ".n8nctl", "load-1password-env.sh")
	for _, path := range []string{filepath.Join(dir, ".n8nctl.yaml"), envPath, loaderPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s not created: %v", path, err)
		}
	}
	envBody, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(envBody), `N8N_PROD_API_KEY="op://Employee/n8nctl-api-key-prod/credential"`) {
		t.Fatalf(".env.1password = %s", string(envBody))
	}
	loaderInfo, err := os.Stat(loaderPath)
	if err != nil {
		t.Fatal(err)
	}
	if loaderInfo.Mode()&0o111 == 0 {
		t.Fatalf("loader mode = %v, want executable", loaderInfo.Mode())
	}
	if !strings.Contains(out.String(), `"onePasswordEnvPath"`) {
		t.Fatalf("stdout = %s", out.String())
	}
}

func TestInitWithOnePasswordCanAddFilesWhenConfigExists(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".n8nctl.yaml"), []byte("existing: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	exitCode := ExecuteWithArgs([]string{"init", "--with-1password"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv:     func(string) string { return "" },
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), "Skipped existing") {
		t.Fatalf("stdout = %s, want skipped existing config", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".env.1password")); err != nil {
		t.Fatalf(".env.1password not created: %v", err)
	}
}

func TestVersionCommandOutputsBuildInfo(t *testing.T) {
	original := buildinfo.Current()
	buildinfo.Version = "v1.2.3"
	buildinfo.Commit = "abc123"
	buildinfo.Date = "2026-04-24T00:00:00Z"
	buildinfo.BuiltBy = "test"
	defer func() {
		buildinfo.Version = original.Version
		buildinfo.Commit = original.Commit
		buildinfo.Date = original.Date
		buildinfo.BuiltBy = original.BuiltBy
	}()

	dir := t.TempDir()
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	exitCode := ExecuteWithArgs([]string{"version", "--json"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv:     func(string) string { return "" },
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})

	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), `"version": "v1.2.3"`) {
		t.Fatalf("stdout = %s", out.String())
	}
}

func TestWorkflowListUsesPagination(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{
		fakeWorkflow("wf-1", "Alert A", false),
		fakeWorkflow("wf-2", "Alert B", false),
		fakeWorkflow("wf-3", "Alert C", false),
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "list", "--env", "dev", "--limit", "3", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}

	var payload struct {
		Workflows []n8n.Workflow `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(payload.Workflows) != 3 {
		t.Fatalf("len(workflows) = %d, want 3", len(payload.Workflows))
	}
}

func TestWorkflowListFiltersByResolvedProject(t *testing.T) {
	server := newFakeN8NServer()
	server.projects = append(server.projects, &n8n.Project{ID: n8n.ID("proj-2"), Name: "Project B"})
	workflowA := fakeWorkflow("wf-1", "Alert A", false)
	workflowB := fakeWorkflow("wf-2", "Alert B", false)
	workflowB.ProjectID = n8n.ID("proj-2")
	server.workflows = []*n8n.Workflow{workflowA, workflowB}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "list", "--env", "dev", "--project", "Project B", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if strings.Contains(stdout, `"name": "Alert A"`) || !strings.Contains(stdout, `"name": "Alert B"`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestProjectListReturnsCompactMetadataAndCounts(t *testing.T) {
	server := newFakeN8NServer()
	server.projects = []*n8n.Project{
		{ID: n8n.ID("proj-1"), Name: "Project A", Role: "owner", Type: "team"},
		{ID: n8n.ID("proj-2"), Name: "Project B", Role: "member", Type: "team"},
	}
	workflowA := fakeWorkflow("wf-1", "Alert A", false)
	workflowB := fakeWorkflow("wf-2", "Alert B", false)
	workflowC := fakeWorkflow("wf-3", "Alert C", false)
	workflowB.ProjectID = n8n.ID("proj-2")
	workflowC.ProjectID = n8n.ID("proj-2")
	server.workflows = []*n8n.Workflow{workflowA, workflowB, workflowC}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"project", "list", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if strings.Contains(stdout, `"nodes"`) || strings.Contains(stdout, `"connections"`) {
		t.Fatalf("stdout contains workflow body fields: %s", stdout)
	}

	var payload struct {
		Projects []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Role          string `json:"role"`
			Type          string `json:"type"`
			WorkflowCount int    `json:"workflowCount"`
		} `json:"projects"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; stdout = %s", err, stdout)
	}
	if got, want := len(payload.Projects), 2; got != want {
		t.Fatalf("len(projects) = %d, want %d; stdout = %s", got, want, stdout)
	}
	if payload.Projects[0].ID != "proj-1" || payload.Projects[0].Role != "owner" || payload.Projects[0].WorkflowCount != 1 {
		t.Fatalf("project[0] = %#v", payload.Projects[0])
	}
	if payload.Projects[1].ID != "proj-2" || payload.Projects[1].Role != "member" || payload.Projects[1].WorkflowCount != 2 {
		t.Fatalf("project[1] = %#v", payload.Projects[1])
	}
}

func TestEnvDoctorReportsAliasAndUnresolvedSecret(t *testing.T) {
	dir := t.TempDir()
	configBody := `default_env: dev
environments:
  dev:
    base_url: https://dev.example.com
    api_key_env: N8N_DEV_API_KEY
    api_key_env_aliases:
      - TEAM_N8N_DEV_KEY
  prod:
    base_url: https://prod.example.com
    api_key_env: N8N_PROD_API_KEY
`
	if err := os.WriteFile(filepath.Join(dir, ".n8nctl.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	exitCode := ExecuteWithArgs([]string{"env", "doctor", "--all", "--json"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv: func(name string) string {
			switch name {
			case "TEAM_N8N_DEV_KEY":
				return "alias-secret"
			case "N8N_PROD_API_KEY":
				return "op://Employee/n8nctl-api-key-prod/credential"
			default:
				return ""
			}
		},
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	stdout := out.String()
	for _, expected := range []string{`"resolvedVar": "TEAM_N8N_DEV_KEY"`, `"status": "ok"`, `"status": "unresolved_secret"`} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("stdout = %s, want %q", stdout, expected)
		}
	}
}

func TestEnvLoadRendersOnePasswordShell(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env.1password"), []byte(`N8N_PROD_API_KEY="op://Employee/n8nctl-api-key-prod/credential"`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	exitCode := ExecuteWithArgs([]string{"env", "load", "--loader", "1password", "--format", "sh"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv:     func(string) string { return "" },
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), `export N8N_PROD_API_KEY="$(op read 'op://Employee/n8nctl-api-key-prod/credential')"`) {
		t.Fatalf("stdout = %s", out.String())
	}
}

func TestWorkflowDiffIgnoresServerManagedFields(t *testing.T) {
	server := newFakeN8NServer()
	remote := readWorkflowFixture(t, filepath.Join("..", "testdata", "workflows", "valid.json"))
	now := time.Now().UTC()
	remote.ID = n8n.ID("wf-1")
	remote.ProjectID = n8n.ID("proj-1")
	remote.Active = true
	remote.CreatedAt = &now
	remote.UpdatedAt = &now
	server.workflows = []*n8n.Workflow{&remote}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "diff", "workflow.json", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"equal": true`) {
		t.Fatalf("stdout = %s, want equal=true", stdout)
	}
}

func TestWorkflowDeployDryRunDoesNotMutate(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--dry-run", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"status": "dry-run"`) {
		t.Fatalf("stdout = %s", stdout)
	}
	if server.hasMutationEvent() {
		t.Fatalf("mutation events = %#v, want none", server.events)
	}
}

func TestWorkflowDeployDeactivatesActiveRemoteBeforeUpdate(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{
		func() *n8n.Workflow {
			wf := fakeWorkflow("wf-1", "Slack Alert", true)
			return wf
		}(),
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"status": "updated"`) {
		t.Fatalf("stdout = %s", stdout)
	}
	if got, want := server.events, []string{"deactivate:wf-1", "update:wf-1"}; !equalStrings(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestWorkflowDeployUpdateStripsReadOnlyFields(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{fakeWorkflow("wf-1", "Slack Alert", false)}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	workflow := readWorkflowFixture(t, filepath.Join("..", "testdata", "workflows", "valid.json"))
	now := time.Unix(100, 0).UTC()
	workflow.ID = n8n.ID("local-id")
	workflow.ProjectID = n8n.ID("proj-1")
	workflow.Active = true
	workflow.CreatedAt = &now
	workflow.UpdatedAt = &now
	workflow.VersionID = "version-id"
	workflow.TriggerCount = 3
	workflow.StaticData = map[string]any{"foo": "bar"}
	workflow.PinData = map[string]any{"Webhook": []any{map[string]any{"json": map[string]any{"message": "pinned"}}}}
	workflow.Meta = map[string]any{"templateCredsSetupCompleted": true}
	workflow.Tags = []n8n.Tag{{ID: n8n.ID("tag-1"), Name: "tag"}}
	workflow.Shared = []n8n.SharedItem{{ProjectID: n8n.ID("proj-1")}}
	workflow.ActiveVersion = map[string]any{"id": "active-version"}
	writeWorkflowFile(t, filepath.Join(dir, "workflow.json"), workflow)

	_, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--id", "wf-1", "--allow-active", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}

	for _, field := range []string{"active", "id", "projectId", "createdAt", "updatedAt", "versionId", "versionCounter", "shared", "tags", "triggerCount", "activeVersion", "activeVersionId", "isArchived", "staticData", "pinData", "meta"} {
		if _, ok := server.lastUpdatePayload[field]; ok {
			t.Fatalf("update payload unexpectedly included %q: %#v", field, server.lastUpdatePayload)
		}
	}
	for _, field := range []string{"name", "nodes", "connections", "settings"} {
		if _, ok := server.lastUpdatePayload[field]; !ok {
			t.Fatalf("update payload missing %q: %#v", field, server.lastUpdatePayload)
		}
	}
}

func TestWorkflowDeployAPIErrorIncludesResponseBody(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{fakeWorkflow("wf-1", "Slack Alert", false)}
	server.updateErrorStatus = http.StatusBadRequest
	server.updateErrorBody = `{"message":"request/body/active is read-only"}`
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	_, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--id", "wf-1", "--json"})
	if exitCode != 5 {
		t.Fatalf("exitCode = %d, want 5; stderr = %s", exitCode, stderr)
	}
	for _, expected := range []string{`"apiMessage": "request/body/active is read-only"`, `"apiBody": "{\"message\":\"request/body/active is read-only\"}"`, `"statusCode": 400`} {
		if !strings.Contains(stderr, expected) {
			t.Fatalf("stderr = %s, want %q", stderr, expected)
		}
	}
}

func TestWorkflowDeployProdRequiresYes(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	_, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "prod"})
	if exitCode != 3 {
		t.Fatalf("exitCode = %d, want 3; stderr = %s", exitCode, stderr)
	}
	for _, expected := range []string{"target environment: prod", "target project: Project A (proj-1)", "planned action: deploy", "--yes"} {
		if !strings.Contains(stderr, expected) {
			t.Fatalf("stderr = %s, want %q", stderr, expected)
		}
	}
}

func TestWorkflowDeployPreflightWarnAndSkipDoNotBlockDryRun(t *testing.T) {
	server := newFakeN8NServer()
	server.credentials = nil
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	_, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--dry-run", "--json"})
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2; stderr = %s", exitCode, stderr)
	}
	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--dry-run", "--credential-preflight", "warn", "--json"})
	if exitCode != 0 {
		t.Fatalf("warn exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"mode": "warn"`) || !strings.Contains(stdout, `"severity": "warning"`) {
		t.Fatalf("warn stdout = %s", stdout)
	}
	stdout, stderr, exitCode = runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--dry-run", "--credential-preflight", "skip", "--json"})
	if exitCode != 0 {
		t.Fatalf("skip exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"skipped": true`) {
		t.Fatalf("skip stdout = %s", stdout)
	}
}

func TestWorkflowDeployReportsProjectVerification(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "deploy", "workflow.json", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"status": "project_location_verified"`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestWorkflowDependenciesLocalDoesNotRequireConfigOrAPIKey(t *testing.T) {
	dir := t.TempDir()
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	exitCode := ExecuteWithArgs([]string{"workflow", "dependencies", "workflow.json", "--local", "--json"}, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: dir,
		Getenv:     func(string) string { return "" },
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, errOut.String())
	}
	if !strings.Contains(out.String(), `"mode": "local"`) || !strings.Contains(out.String(), `"type": "credential"`) {
		t.Fatalf("stdout = %s", out.String())
	}
}

func TestWorkflowDriftCIUsesStableExitCode(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{fakeWorkflow("wf-1", "Slack Alert", false)}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	local := readWorkflowFixture(t, filepath.Join("..", "testdata", "workflows", "valid.json"))
	local.Nodes[1].Parameters["text"] = "changed"
	writeWorkflowFile(t, filepath.Join(dir, "workflow.json"), local)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "drift", "workflow.json", "--env", "dev", "--ci", "--json"})
	if exitCode != 22 {
		t.Fatalf("exitCode = %d, want 22; stdout = %s stderr = %s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout, `"status": "drift"`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestWorkflowCleanupDryRunAndDelete(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{
		fakeWorkflow("wf-1", "tmp-one", false),
		fakeWorkflow("wf-2", "keep-two", false),
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "cleanup", "--env", "dev", "--project", "Project A", "--prefix", "tmp-", "--dry-run", "--json"})
	if exitCode != 0 {
		t.Fatalf("dry-run exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"status": "would-delete"`) || server.hasMutationEvent() {
		t.Fatalf("stdout = %s events = %#v", stdout, server.events)
	}
	stdout, stderr, exitCode = runCLI(t, dir, server, []string{"workflow", "cleanup", "--env", "dev", "--project", "Project A", "--prefix", "tmp-", "--json"})
	if exitCode != 0 {
		t.Fatalf("delete exitCode = %d, stdout = %s stderr = %s", exitCode, stdout, stderr)
	}
	if got, want := server.events[len(server.events)-1], "delete:wf-1"; got != want {
		t.Fatalf("last event = %q, want %q; all events = %#v", got, want, server.events)
	}
}

func TestWorkflowRunUnsupportedEndpoint(t *testing.T) {
	server := newFakeN8NServer()
	server.workflows = []*n8n.Workflow{fakeWorkflow("wf-1", "Slack Alert", false)}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	_, stderr, exitCode := runCLI(t, dir, server, []string{"workflow", "run", "Slack Alert", "--env", "dev", "--json"})
	if exitCode != 5 {
		t.Fatalf("exitCode = %d, want 5; stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stderr, `"code": "unsupported_endpoint"`) {
		t.Fatalf("stderr = %s", stderr)
	}
}

func TestExecutionListResolvesWorkflowName(t *testing.T) {
	server := newFakeN8NServer()
	started := time.Unix(100, 0).UTC()
	stopped := time.Unix(120, 0).UTC()
	server.workflows = []*n8n.Workflow{fakeWorkflow("wf-1", "Slack Alert", false)}
	server.executions = []*n8n.Execution{
		{
			ID:         n8n.ID("2001"),
			WorkflowID: n8n.ID("wf-1"),
			Status:     "success",
			Mode:       "manual",
			StartedAt:  &started,
			StoppedAt:  &stopped,
		},
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"execution", "list", "--env", "dev", "--workflow", "Slack Alert", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"workflowName": "Slack Alert"`) {
		t.Fatalf("stdout = %s", stdout)
	}
}

func TestExecutionDiagnoseReportsFailedNodeRun(t *testing.T) {
	server := newFakeN8NServer()
	started := time.Unix(100, 0).UTC()
	stopped := time.Unix(103, 0).UTC()
	server.executions = []*n8n.Execution{
		{
			ID:         n8n.ID("3001"),
			WorkflowID: n8n.ID("wf-1"),
			Status:     "error",
			Mode:       "manual",
			Finished:   true,
			StartedAt:  &started,
			StoppedAt:  &stopped,
			Data: map[string]any{
				"resultData": map[string]any{
					"lastNodeExecuted": "HTTP Request",
					"error": map[string]any{
						"message": "Unauthorized",
					},
					"runData": map[string]any{
						"HTTP Request": []any{
							map[string]any{
								"startTime":     "2026-04-25T08:00:00Z",
								"executionTime": float64(321),
								"data": map[string]any{
									"main": []any{[]any{map[string]any{"json": map[string]any{"id": "item-1"}}}},
								},
								"error": map[string]any{
									"name":        "NodeApiError",
									"message":     "401 Unauthorized",
									"description": "Google credential rejected the request",
									"httpCode":    "401",
									"stack":       "NodeApiError: 401 Unauthorized\n    at node",
								},
							},
						},
					},
				},
			},
		},
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"execution", "diagnose", "3001", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	for _, expected := range []string{`"lastNodeExecuted": "HTTP Request"`, `"nodeName": "HTTP Request"`, `"message": "401 Unauthorized"`, `"items": 1`, "OAuth scopes"} {
		if !strings.Contains(stdout, expected) {
			t.Fatalf("stdout = %s, want %q", stdout, expected)
		}
	}
}

func TestExecutionWaitDiagnosesFailureAndCIExitCode(t *testing.T) {
	server := newFakeN8NServer()
	started := time.Unix(100, 0).UTC()
	stopped := time.Unix(101, 0).UTC()
	server.executions = []*n8n.Execution{
		{
			ID:         n8n.ID("4001"),
			WorkflowID: n8n.ID("wf-1"),
			Status:     "error",
			Mode:       "manual",
			Finished:   true,
			StartedAt:  &started,
			StoppedAt:  &stopped,
			Data: map[string]any{
				"resultData": map[string]any{
					"lastNodeExecuted": "HTTP Request",
					"runData": map[string]any{
						"HTTP Request": []any{
							map[string]any{
								"error": map[string]any{
									"name":     "NodeApiError",
									"message":  "401 Unauthorized",
									"httpCode": "401",
								},
							},
						},
					},
				},
			},
		},
	}
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)

	stdout, stderr, exitCode := runCLI(t, dir, server, []string{"execution", "wait", "4001", "--env", "dev", "--json"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr)
	}
	if !strings.Contains(stdout, `"executionUrl": "`+ts.URL+`/workflow/wf-1/executions/4001"`) || !strings.Contains(stdout, `"nodeName": "HTTP Request"`) {
		t.Fatalf("stdout = %s", stdout)
	}

	stdout, stderr, exitCode = runCLI(t, dir, server, []string{"execution", "wait", "4001", "--env", "dev", "--ci", "--json"})
	if exitCode != 23 {
		t.Fatalf("CI exitCode = %d, want 23; stdout = %s stderr = %s", exitCode, stdout, stderr)
	}
}

func runCLI(t *testing.T, workingDir string, server *fakeN8NServer, args []string) (string, string, int) {
	t.Helper()

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	exitCode := ExecuteWithArgs(args, Dependencies{
		Streams:    Streams{In: bytes.NewBuffer(nil), Out: out, ErrOut: errOut},
		WorkingDir: workingDir,
		Getenv: func(name string) string {
			switch name {
			case "N8N_DEV_API_KEY", "N8N_PROD_API_KEY":
				return "test-key"
			default:
				return ""
			}
		},
		HTTPClient: server.httpClient(),
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	return out.String(), errOut.String(), exitCode
}

func writeConfig(t *testing.T, dir, devURL, prodURL string) {
	t.Helper()

	configBody := fmt.Sprintf(`default_env: dev
environments:
  dev:
    base_url: %s
    api_key_env: N8N_DEV_API_KEY
    default_project: Project A
  prod:
    base_url: %s
    api_key_env: N8N_PROD_API_KEY
    default_project: Project A
workflows:
  path: workflows
  name_strategy: file_or_json_name
safety:
  require_confirm_for_prod: true
  backup_before_update: true
  deploy_inactive_by_default: true
`, devURL, prodURL)

	if err := os.WriteFile(filepath.Join(dir, ".n8nctl.yaml"), []byte(configBody), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFixtureToDir(t *testing.T, fixturePath, destination string) {
	t.Helper()

	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readWorkflowFixture(t *testing.T, path string) n8n.Workflow {
	t.Helper()

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var workflow n8n.Workflow
	if err := json.Unmarshal(body, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow
}

func writeWorkflowFile(t *testing.T, path string, workflow n8n.Workflow) {
	t.Helper()

	body, err := json.MarshalIndent(workflow, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func fakeWorkflow(id, name string, active bool) *n8n.Workflow {
	workflow := readWorkflowFixtureForServer(name)
	workflow.ID = n8n.ID(id)
	workflow.ProjectID = n8n.ID("proj-1")
	workflow.Name = name
	workflow.Active = active
	return workflow
}

func readWorkflowFixtureForServer(name string) *n8n.Workflow {
	workflow := &n8n.Workflow{
		Name: name,
		Nodes: []n8n.Node{
			{Name: "Webhook", Type: "n8n-nodes-base.webhook"},
		},
		Connections: map[string]any{},
		Settings:    map[string]any{"executionOrder": "v1"},
	}
	return workflow
}

type fakeN8NServer struct {
	mu                sync.Mutex
	workflows         []*n8n.Workflow
	executions        []*n8n.Execution
	projects          []*n8n.Project
	credentials       []*n8n.Credential
	events            []string
	lastUpdatePayload map[string]any
	updateErrorStatus int
	updateErrorBody   string
}

func newFakeN8NServer() *fakeN8NServer {
	return &fakeN8NServer{
		workflows:  []*n8n.Workflow{},
		executions: []*n8n.Execution{},
		projects: []*n8n.Project{
			{ID: n8n.ID("proj-1"), Name: "Project A"},
		},
		credentials: []*n8n.Credential{
			{ID: n8n.ID("cred-1"), Name: "Slack API", Type: "slackApi", ProjectID: n8n.ID("proj-1")},
		},
		events: []string{},
	}
}

func (s *fakeN8NServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")

	switch {
	case r.Method == http.MethodGet && path == "/workflows":
		s.handleListWorkflows(w, r)
	case r.Method == http.MethodPost && path == "/workflows":
		s.handleCreateWorkflow(w, r.Body)
	case r.Method == http.MethodGet && path == "/projects":
		s.handleListProjects(w, r)
	case r.Method == http.MethodGet && path == "/credentials":
		s.handleListCredentials(w, r)
	case r.Method == http.MethodPut && strings.HasSuffix(path, "/transfer"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/workflows/"), "/transfer")
		s.handleTransferWorkflow(w, id, r.Body)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/workflows/") && !strings.HasSuffix(path, "/activate") && !strings.HasSuffix(path, "/deactivate"):
		id := strings.TrimPrefix(path, "/workflows/")
		s.handleGetWorkflow(w, id)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/workflows/"):
		id := strings.TrimPrefix(path, "/workflows/")
		s.handleUpdateWorkflow(w, id, r.Body)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/workflows/"):
		id := strings.TrimPrefix(path, "/workflows/")
		s.handleDeleteWorkflow(w, id)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/activate"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/workflows/"), "/activate")
		s.handleActivateWorkflow(w, id)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/deactivate"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/workflows/"), "/deactivate")
		s.handleDeactivateWorkflow(w, id)
	case r.Method == http.MethodGet && path == "/executions":
		s.handleListExecutions(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/executions/"):
		id := strings.TrimPrefix(path, "/executions/")
		s.handleGetExecution(w, id)
	default:
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	}
}

func (s *fakeN8NServer) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	offset := parseCursor(r.URL.Query().Get("cursor"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 250
	}
	filterName := r.URL.Query().Get("name")
	projectID := r.URL.Query().Get("projectId")

	filtered := make([]*n8n.Workflow, 0)
	for _, workflow := range s.workflows {
		if filterName != "" && workflow.Name != filterName {
			continue
		}
		if projectID != "" && workflow.ProjectID.String() != projectID {
			continue
		}
		filtered = append(filtered, workflow)
	}

	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]
	nextCursor := ""
	if end < len(filtered) {
		nextCursor = strconv.Itoa(end)
	}

	response := struct {
		Data       []*n8n.Workflow `json:"data"`
		NextCursor string          `json:"nextCursor,omitempty"`
	}{
		Data:       page,
		NextCursor: nextCursor,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *fakeN8NServer) handleCreateWorkflow(w http.ResponseWriter, body io.Reader) {
	var workflow n8n.Workflow
	_ = json.NewDecoder(body).Decode(&workflow)
	workflow.ID = n8n.ID(fmt.Sprintf("wf-%d", len(s.workflows)+1))
	workflow.Active = false
	s.workflows = append(s.workflows, &workflow)
	s.events = append(s.events, "create:"+workflow.ID.String())
	_ = json.NewEncoder(w).Encode(workflow)
}

func (s *fakeN8NServer) handleTransferWorkflow(w http.ResponseWriter, id string, body io.Reader) {
	var request struct {
		DestinationProjectID string `json:"destinationProjectId"`
	}
	_ = json.NewDecoder(body).Decode(&request)
	for _, workflow := range s.workflows {
		if workflow.ID.String() == id {
			workflow.ProjectID = n8n.ID(request.DestinationProjectID)
			s.events = append(s.events, "transfer:"+id)
			_ = json.NewEncoder(w).Encode(workflow)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleGetWorkflow(w http.ResponseWriter, id string) {
	for _, workflow := range s.workflows {
		if workflow.ID.String() == id {
			_ = json.NewEncoder(w).Encode(workflow)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleUpdateWorkflow(w http.ResponseWriter, id string, body io.Reader) {
	rawBody, _ := io.ReadAll(body)
	s.lastUpdatePayload = map[string]any{}
	_ = json.Unmarshal(rawBody, &s.lastUpdatePayload)
	if s.updateErrorStatus != 0 {
		w.WriteHeader(s.updateErrorStatus)
		_, _ = w.Write([]byte(s.updateErrorBody))
		return
	}
	for _, workflow := range s.workflows {
		if workflow.ID.String() != id {
			continue
		}
		var updated n8n.Workflow
		_ = json.Unmarshal(rawBody, &updated)
		updated.ID = workflow.ID
		updated.Active = workflow.Active
		*workflow = updated
		s.events = append(s.events, "update:"+id)
		_ = json.NewEncoder(w).Encode(workflow)
		return
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleDeleteWorkflow(w http.ResponseWriter, id string) {
	for i, workflow := range s.workflows {
		if workflow.ID.String() == id {
			s.workflows = append(s.workflows[:i], s.workflows[i+1:]...)
			s.events = append(s.events, "delete:"+id)
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleActivateWorkflow(w http.ResponseWriter, id string) {
	for _, workflow := range s.workflows {
		if workflow.ID.String() == id {
			workflow.Active = true
			s.events = append(s.events, "activate:"+id)
			_ = json.NewEncoder(w).Encode(workflow)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleDeactivateWorkflow(w http.ResponseWriter, id string) {
	for _, workflow := range s.workflows {
		if workflow.ID.String() == id {
			workflow.Active = false
			s.events = append(s.events, "deactivate:"+id)
			_ = json.NewEncoder(w).Encode(workflow)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	offset := parseCursor(r.URL.Query().Get("cursor"))
	limit := parseLimit(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 250
	}
	workflowID := r.URL.Query().Get("workflowId")
	status := r.URL.Query().Get("status")

	filtered := make([]*n8n.Execution, 0)
	for _, execution := range s.executions {
		if workflowID != "" && execution.WorkflowID.String() != workflowID {
			continue
		}
		if status != "" && execution.Status != status {
			continue
		}
		filtered = append(filtered, execution)
	}

	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]
	nextCursor := ""
	if end < len(filtered) {
		nextCursor = strconv.Itoa(end)
	}

	response := struct {
		Data       []*n8n.Execution `json:"data"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}{
		Data:       page,
		NextCursor: nextCursor,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *fakeN8NServer) handleGetExecution(w http.ResponseWriter, id string) {
	for _, execution := range s.executions {
		if execution.ID.String() == id {
			_ = json.NewEncoder(w).Encode(execution)
			return
		}
	}
	http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
}

func (s *fakeN8NServer) handleListProjects(w http.ResponseWriter, r *http.Request) {
	response := struct {
		Data       []*n8n.Project `json:"data"`
		NextCursor string         `json:"nextCursor,omitempty"`
	}{
		Data: s.projects,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *fakeN8NServer) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	response := struct {
		Data       []*n8n.Credential `json:"data"`
		NextCursor string            `json:"nextCursor,omitempty"`
	}{
		Data: s.credentials,
	}
	_ = json.NewEncoder(w).Encode(response)
}

func (s *fakeN8NServer) httpClient() *http.Client {
	return &http.Client{}
}

func (s *fakeN8NServer) hasMutationEvent() bool {
	for _, event := range s.events {
		if strings.HasPrefix(event, "create:") || strings.HasPrefix(event, "update:") || strings.HasPrefix(event, "activate:") || strings.HasPrefix(event, "deactivate:") || strings.HasPrefix(event, "transfer:") || strings.HasPrefix(event, "delete:") {
			return true
		}
	}
	return false
}

func parseCursor(value string) int {
	if value == "" {
		return 0
	}
	offset, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return offset
}

func parseLimit(value string) int {
	if value == "" {
		return 0
	}
	limit, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return limit
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
