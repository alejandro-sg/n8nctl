package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"
)

func TestToolRegistrationAndSchemaGeneration(t *testing.T) {
	dir := t.TempDir()
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		return CLIResult{Stdout: `{"status":"ok"}`, ExitCode: 0}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	defer serverIn.Close()
	defer clientOut.Close()
	defer clientIn.Close()
	defer serverOut.Close()

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(ctx, serverIn, serverOut)
	}()

	client := mcp.NewClient(stdio.NewStdioServerTransportWithIO(clientIn, clientOut))
	initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer initCancel()
	if _, err := client.Initialize(initCtx); err != nil {
		t.Fatalf("Initialize() error = %v", err)
	}
	tools, err := client.ListTools(initCtx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if got, want := len(tools.Tools), len(ToolNames()); got != want {
		t.Fatalf("len(tools) = %d, want %d", got, want)
	}
	foundDeploy := false
	foundProjectList := false
	for _, tool := range tools.Tools {
		if tool.Name == "workflow_deploy" {
			foundDeploy = tool.InputSchema != nil
		}
		if tool.Name == "project_list" {
			foundProjectList = tool.InputSchema != nil
		}
	}
	if !foundDeploy {
		t.Fatalf("workflow_deploy tool with input schema not found: %#v", tools.Tools)
	}
	if !foundProjectList {
		t.Fatalf("project_list tool with input schema not found: %#v", tools.Tools)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not stop after context cancellation")
	}
}

func TestServeExitsWhenStdinCloses(t *testing.T) {
	dir := t.TempDir()
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		return CLIResult{Stdout: `{"status":"ok"}`, ExitCode: 0}
	})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"test"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		"",
	}, "\n")
	var out bytes.Buffer

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(context.Background(), strings.NewReader(input), &out)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Serve() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not exit after stdin closed")
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("response lines = %d, want 2; output=%q", len(lines), out.String())
	}
	responses := make(map[int]string)
	for _, line := range lines {
		var response struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("response %q did not decode: %v", line, err)
		}
		responses[response.ID] = line
	}
	if !strings.Contains(responses[1], `"serverInfo"`) {
		t.Fatalf("initialize response = %q", responses[1])
	}
	if !strings.Contains(responses[2], `"tools"`) {
		t.Fatalf("tools/list response = %q", responses[2])
	}
}

func TestMutationDefaultsToDryRunAndReturnsConfirmation(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	var gotArgs []string
	server := newTestServer(t, dir, func(_ context.Context, args []string) CLIResult {
		gotArgs = append([]string(nil), args...)
		return CLIResult{Stdout: `{"status":"dry-run","workflowName":"Slack Alert"}`, ExitCode: 0}
	})

	result := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:  "dev",
		File: "workflow.json",
	})
	if result.Status != "ok" {
		t.Fatalf("result = %#v", result)
	}
	if !contains(gotArgs, "--dry-run") {
		t.Fatalf("args = %#v, want --dry-run", gotArgs)
	}
	if result.ConfirmationPhrase == "" {
		t.Fatalf("confirmation phrase empty in result %#v", result)
	}
	if result.NextCall == nil {
		t.Fatalf("next call empty in result %#v", result)
	}
	if got, want := result.NextCall.Tool, "workflow_deploy"; got != want {
		t.Fatalf("next call tool = %q, want %q", got, want)
	}
	nextArgs := result.NextCall.Arguments
	if got, want := nextArgs["env"], "dev"; got != want {
		t.Fatalf("next env = %#v, want %#v", got, want)
	}
	if got, want := nextArgs["file"], "workflow.json"; got != want {
		t.Fatalf("next file = %#v, want %#v", got, want)
	}
	if got, want := nextArgs["dry_run"], false; got != want {
		t.Fatalf("next dry_run = %#v, want %#v", got, want)
	}
	if got, want := nextArgs["confirm_mutation"], true; got != want {
		t.Fatalf("next confirm_mutation = %#v, want %#v", got, want)
	}
	if got, want := nextArgs["confirmation_phrase"], result.ConfirmationPhrase; got != want {
		t.Fatalf("next confirmation_phrase = %#v, want %#v", got, want)
	}
}

func TestNonDryRunMutationRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	called := false
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		called = true
		return CLIResult{Stdout: `{"status":"updated"}`, ExitCode: 0}
	})
	dryRun := false
	result := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:            "dev",
		File:           "workflow.json",
		MutationFields: MutationFields{DryRun: &dryRun},
	})
	if result.Status != "error" {
		t.Fatalf("status = %s, want error", result.Status)
	}
	if called {
		t.Fatal("runner called for unconfirmed mutation")
	}
	if result.RetryCall == nil {
		t.Fatalf("retry call empty in result %#v", result)
	}
	if got, want := result.RetryCall.Tool, "workflow_deploy"; got != want {
		t.Fatalf("retry call tool = %q, want %q", got, want)
	}
	if got, want := result.RetryCall.Arguments["confirm_mutation"], true; got != want {
		t.Fatalf("retry confirm_mutation = %#v, want %#v", got, want)
	}
	if got, want := result.RetryCall.Arguments["dry_run"], false; got != want {
		t.Fatalf("retry dry_run = %#v, want %#v", got, want)
	}
}

func TestProductionMutationRequiresPriorDryRunPhrase(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	writeConfig(t, dir)
	var calls [][]string
	server := newTestServer(t, dir, func(_ context.Context, args []string) CLIResult {
		calls = append(calls, append([]string(nil), args...))
		if contains(args, "--dry-run") {
			return CLIResult{Stdout: `{"status":"dry-run","workflowName":"Slack Alert"}`, ExitCode: 0}
		}
		return CLIResult{Stdout: `{"status":"updated","workflowName":"Slack Alert"}`, ExitCode: 0}
	})

	dry := true
	dryResult := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:            "prod",
		File:           "workflow.json",
		MutationFields: MutationFields{DryRun: &dry},
	})
	if dryResult.ConfirmationPhrase == "" {
		t.Fatalf("dry-run result = %#v, want confirmation phrase", dryResult)
	}
	if dryResult.NextCall == nil {
		t.Fatalf("dry-run result = %#v, want next call", dryResult)
	}

	real := false
	realResult := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:  "prod",
		File: "workflow.json",
		MutationFields: MutationFields{
			DryRun:             &real,
			ConfirmMutation:    true,
			ConfirmationPhrase: dryResult.ConfirmationPhrase,
		},
	})
	if realResult.Status != "ok" {
		payload, _ := json.MarshalIndent(realResult, "", "  ")
		t.Fatalf("real result = %s", payload)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v, want dry-run and real call", calls)
	}
	if !contains(calls[1], "--yes") {
		t.Fatalf("real call args = %#v, want --yes for production", calls[1])
	}
}

func TestProductionDeployConfirmationMatchesCredentialPreflightWarn(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	writeConfig(t, dir)
	var calls [][]string
	server := newTestServer(t, dir, func(_ context.Context, args []string) CLIResult {
		calls = append(calls, append([]string(nil), args...))
		if contains(args, "--dry-run") {
			return CLIResult{Stdout: `{"status":"dry-run","workflowName":"Slack Alert"}`, ExitCode: 0}
		}
		return CLIResult{Stdout: `{"status":"updated","workflowName":"Slack Alert"}`, ExitCode: 0}
	})

	dry := true
	dryResult := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:                 "prod",
		File:                "workflow.json",
		CredentialPreflight: "warn",
		MutationFields:      MutationFields{DryRun: &dry, ConfirmMutation: true},
	})
	if dryResult.ConfirmationPhrase == "" {
		t.Fatalf("dry-run result = %#v, want confirmation phrase", dryResult)
	}
	if dryResult.NextCall == nil {
		t.Fatalf("dry-run result = %#v, want next call", dryResult)
	}
	if got, want := dryResult.NextCall.Arguments["credential_preflight"], "warn"; got != want {
		t.Fatalf("next call credential_preflight = %#v, want %#v", got, want)
	}

	real := false
	realResult := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:                 "prod",
		File:                "workflow.json",
		CredentialPreflight: "warn",
		MutationFields: MutationFields{
			DryRun:             &real,
			ConfirmMutation:    true,
			ConfirmationPhrase: dryResult.ConfirmationPhrase,
		},
	})
	if realResult.Status != "ok" {
		payload, _ := json.MarshalIndent(realResult, "", "  ")
		t.Fatalf("real result = %s", payload)
	}
	if got, want := flagValue(calls[1], "--credential-preflight"), "warn"; got != want {
		t.Fatalf("real call credential preflight = %q, want %q; args = %#v", got, want, calls[1])
	}
}

func TestProductionCreateConfirmationMatchesCredentialPreflightWarn(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	writeConfig(t, dir)
	server := newTestServer(t, dir, func(_ context.Context, args []string) CLIResult {
		if contains(args, "--dry-run") {
			return CLIResult{Stdout: `{"status":"dry-run","workflowName":"Slack Alert"}`, ExitCode: 0}
		}
		return CLIResult{Stdout: `{"status":"created","workflowName":"Slack Alert"}`, ExitCode: 0}
	})

	dry := true
	dryResult := server.handleWorkflowCreate(context.Background(), WorkflowCreateArgs{
		Env:                 "prod",
		File:                "workflow.json",
		CredentialPreflight: "warn",
		MutationFields:      MutationFields{DryRun: &dry},
	})
	if dryResult.ConfirmationPhrase == "" {
		t.Fatalf("dry-run result = %#v, want confirmation phrase", dryResult)
	}

	real := false
	realResult := server.handleWorkflowCreate(context.Background(), WorkflowCreateArgs{
		Env:                 "prod",
		File:                "workflow.json",
		CredentialPreflight: "warn",
		MutationFields: MutationFields{
			DryRun:             &real,
			ConfirmMutation:    true,
			ConfirmationPhrase: dryResult.ConfirmationPhrase,
		},
	})
	if realResult.Status != "ok" {
		payload, _ := json.MarshalIndent(realResult, "", "  ")
		t.Fatalf("real result = %s", payload)
	}
}

func TestNonDryRunCredentialPreflightSkipBlocked(t *testing.T) {
	dir := t.TempDir()
	writeWorkflow(t, dir)
	called := false
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		called = true
		return CLIResult{}
	})
	real := false
	result := server.handleWorkflowDeploy(context.Background(), WorkflowDeployArgs{
		Env:                 "dev",
		File:                "workflow.json",
		CredentialPreflight: "skip",
		MutationFields: MutationFields{
			DryRun:          &real,
			ConfirmMutation: true,
		},
	})
	if result.Status != "error" || !strings.Contains(toJSON(result.Error), "skip") {
		t.Fatalf("result = %#v, want skip block", result)
	}
	if called {
		t.Fatal("runner called for blocked credential_preflight=skip")
	}
}

func TestCleanupByPrefixNonDryRunBlocked(t *testing.T) {
	dir := t.TempDir()
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		t.Fatal("runner should not be called")
		return CLIResult{}
	})
	dry := false
	result := server.handleWorkflowCleanup(context.Background(), WorkflowCleanupArgs{
		Env:     "dev",
		Project: "Project A",
		Prefix:  "tmp-",
		MutationFields: MutationFields{
			DryRun:          &dry,
			ConfirmMutation: true,
		},
	})
	if result.Status != "error" || !strings.Contains(toJSON(result.Error), "prefix") {
		t.Fatalf("result = %#v, want prefix cleanup block", result)
	}
}

func TestRemoteRequiredIDsAreValidatedBeforeRunner(t *testing.T) {
	dir := t.TempDir()
	called := false
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult {
		called = true
		return CLIResult{}
	})
	result := server.handleExecutionGet(context.Background(), ExecutionIDArgs{Env: "dev"})
	if result.Status != "error" || called {
		t.Fatalf("result = %#v called=%t, want local validation error before runner", result, called)
	}
}

func TestProjectListRequiresEnvAndRoutesCompactCLI(t *testing.T) {
	dir := t.TempDir()
	var gotArgs []string
	server := newTestServer(t, dir, func(_ context.Context, args []string) CLIResult {
		gotArgs = append([]string(nil), args...)
		return CLIResult{Stdout: `{"status":"ok","projects":[]}`, ExitCode: 0}
	})

	missingEnv := server.handleProjectList(context.Background(), ProjectListArgs{})
	if missingEnv.Status != "error" {
		t.Fatalf("missing env result = %#v, want error", missingEnv)
	}

	result := server.handleProjectList(context.Background(), ProjectListArgs{
		Env:                "dev",
		Limit:              10,
		SkipWorkflowCounts: true,
	})
	if result.Status != "ok" {
		t.Fatalf("result = %#v, want ok", result)
	}
	wantArgs := []string{"--json", "--no-color", "project", "list", "--env", "dev", "--limit", "10", "--skip-workflow-counts"}
	if !equalStringSlices(gotArgs, wantArgs) {
		t.Fatalf("args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestWorkspacePathRejectsTraversalAndSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	server := newTestServer(t, dir, func(context.Context, []string) CLIResult { return CLIResult{} })
	if _, err := server.workspacePath("../outside.json", true); err == nil {
		t.Fatal("workspacePath traversal error = nil, want error")
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "workflow.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "workflow.json"), filepath.Join(dir, "linked.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := server.workspacePath("linked.json", true); err == nil {
		t.Fatal("workspacePath symlink escape error = nil, want error")
	}
}

func TestSanitizeRedactsSecretsAndRawData(t *testing.T) {
	input := map[string]any{
		"apiKey": "secret-value",
		"workflow": map[string]any{
			"pinData": map[string]any{"node": "raw"},
		},
		"execution": map[string]any{
			"data": map[string]any{"resultData": "raw"},
		},
	}
	got := Sanitize(input).(map[string]any)
	if got["apiKey"] != "<redacted>" {
		t.Fatalf("apiKey = %#v, want redacted", got["apiKey"])
	}
	if !strings.Contains(toJSON(got), "mcp_response_sanitization") {
		t.Fatalf("sanitized payload = %s, want pinData omission", toJSON(got))
	}
	if strings.Contains(toJSON(got), "resultData") {
		t.Fatalf("sanitized payload = %s, want execution data omitted", toJSON(got))
	}
}

func newTestServer(t *testing.T, dir string, runner CLIRunner) *Server {
	t.Helper()
	server, err := New(Config{
		WorkingDir:    dir,
		RunCLI:        runner,
		ToolTimeout:   time.Second,
		Now:           func() time.Time { return time.Unix(0, 0).UTC() },
		ServerVersion: "test",
		EOFGrace:      10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func writeWorkflow(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "workflow.json"), []byte(`{"name":"Slack Alert","nodes":[],"connections":{},"settings":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeConfig(t *testing.T, dir string) {
	t.Helper()
	body := `default_env: dev
environments:
  dev:
    base_url: https://dev.example.com
    api_key_env: N8N_DEV_API_KEY
  prod:
    base_url: https://prod.example.com
    api_key_env: N8N_PROD_API_KEY
safety:
  require_confirm_for_prod: true
`
	if err := os.WriteFile(filepath.Join(dir, ".n8nctl.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func equalStringSlices(left []string, right []string) bool {
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

func flagValue(args []string, name string) string {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == name {
			return args[i+1]
		}
	}
	return ""
}

func toJSON(value any) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}
