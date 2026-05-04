package cmd

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/LogicMonitor-IT/n8nctl/internal/mcpserver"
)

func TestMCPWorkflowDeployDryRunDoesNotMutate(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	client, stop := startMCPClient(t, dir, server)
	defer stop()

	response, err := client.CallTool(context.Background(), "workflow_deploy", map[string]any{
		"env":  "dev",
		"file": "workflow.json",
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	var result mcpserver.ToolResult
	decodeMCPResult(t, response, &result)
	if result.Status != "ok" || result.ConfirmationPhrase == "" {
		t.Fatalf("result = %#v, want ok dry-run with confirmation", result)
	}
	if server.hasMutationEvent() {
		t.Fatalf("mutation events = %#v, want none", server.events)
	}
}

func TestMCPConfirmedDevDeployMutates(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	client, stop := startMCPClient(t, dir, server)
	defer stop()

	dry := false
	response, err := client.CallTool(context.Background(), "workflow_deploy", map[string]any{
		"env":              "dev",
		"file":             "workflow.json",
		"dry_run":          dry,
		"confirm_mutation": true,
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	var result mcpserver.ToolResult
	decodeMCPResult(t, response, &result)
	if result.Status != "ok" {
		t.Fatalf("result = %#v, want ok", result)
	}
	if !server.hasMutationEvent() {
		t.Fatalf("mutation events = %#v, want create/update event", server.events)
	}
	auditPath := filepath.Join(dir, ".n8nctl", "audit", "mcp.jsonl")
	info, err := os.Stat(auditPath)
	if err != nil {
		t.Fatalf("audit file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("audit mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestMCPProdDeployBlocksWithoutDryRunPhrase(t *testing.T) {
	server := newFakeN8NServer()
	ts := httptest.NewServer(server)
	defer ts.Close()

	dir := t.TempDir()
	writeConfig(t, dir, ts.URL, ts.URL)
	copyFixtureToDir(t, filepath.Join("..", "testdata", "workflows", "valid.json"), filepath.Join(dir, "workflow.json"))

	client, stop := startMCPClient(t, dir, server)
	defer stop()

	dry := false
	response, err := client.CallTool(context.Background(), "workflow_deploy", map[string]any{
		"env":              "prod",
		"file":             "workflow.json",
		"dry_run":          dry,
		"confirm_mutation": true,
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	var result mcpserver.ToolResult
	decodeMCPResult(t, response, &result)
	if result.Status != "error" {
		t.Fatalf("result = %#v, want production confirmation error", result)
	}
	if server.hasMutationEvent() {
		t.Fatalf("mutation events = %#v, want none", server.events)
	}
}

func startMCPClient(t *testing.T, dir string, fake *fakeN8NServer) (*mcp.Client, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	mcpServer, err := mcpserver.New(mcpserver.Config{
		WorkingDir: dir,
		RunCLI: func(ctx context.Context, args []string) mcpserver.CLIResult {
			stdout, stderr, exitCode := runCLI(t, dir, fake, args)
			return mcpserver.CLIResult{Stdout: stdout, Stderr: stderr, ExitCode: exitCode}
		},
		ToolTimeout:   2 * time.Second,
		Now:           func() time.Time { return time.Unix(0, 0).UTC() },
		ServerVersion: "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- mcpServer.Serve(ctx, serverIn, serverOut)
	}()

	client := mcp.NewClient(stdio.NewStdioServerTransportWithIO(clientIn, clientOut))
	initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer initCancel()
	if _, err := client.Initialize(initCtx); err != nil {
		cancel()
		t.Fatalf("Initialize() error = %v", err)
	}

	stop := func() {
		cancel()
		_ = clientIn.Close()
		_ = clientOut.Close()
		_ = serverIn.Close()
		_ = serverOut.Close()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("Serve() error = %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("MCP server did not stop")
		}
	}
	return client, stop
}

func decodeMCPResult(t *testing.T, response *mcp.ToolResponse, out any) {
	t.Helper()
	if response == nil || len(response.Content) == 0 || response.Content[0].TextContent == nil {
		t.Fatalf("response has no text content: %#v", response)
	}
	if err := json.Unmarshal([]byte(response.Content[0].TextContent.Text), out); err != nil {
		t.Fatalf("json.Unmarshal(%s) error = %v", response.Content[0].TextContent.Text, err)
	}
}
