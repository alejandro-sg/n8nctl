package mcpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	mcp "github.com/metoro-io/mcp-golang"
	"github.com/metoro-io/mcp-golang/transport"
	"github.com/metoro-io/mcp-golang/transport/stdio"

	"github.com/alejandro-sg/n8nctl/internal/config"
)

type CLIResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type CLIRunner func(context.Context, []string) CLIResult

type Config struct {
	WorkingDir    string
	RunCLI        CLIRunner
	ToolTimeout   time.Duration
	Now           func() time.Time
	ServerVersion string
	EOFGrace      time.Duration
}

type Server struct {
	workingDir    string
	runCLI        CLIRunner
	toolTimeout   time.Duration
	now           func() time.Time
	serverVersion string
	eofGrace      time.Duration

	mutationMu     sync.Mutex
	confirmationMu sync.Mutex
	confirmations  map[string]time.Time
}

type MCPCall struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type ToolResult struct {
	Status                string   `json:"status"`
	Tool                  string   `json:"tool"`
	ExitCode              int      `json:"exitCode,omitempty"`
	Output                any      `json:"output,omitempty"`
	Error                 any      `json:"error,omitempty"`
	ConfirmationPhrase    string   `json:"confirmationPhrase,omitempty"`
	ConfirmationExpiresAt string   `json:"confirmationExpiresAt,omitempty"`
	NextCall              *MCPCall `json:"nextCall,omitempty"`
	RetryCall             *MCPCall `json:"retryCall,omitempty"`
	AuditError            string   `json:"auditError,omitempty"`
}

type invocation struct {
	ToolName           string
	CLIArgs            []string
	NextCallArguments  map[string]any
	Remote             bool
	Env                string
	Mutating           bool
	DryRun             bool
	ConfirmMutation    bool
	ConfirmationPhrase string
	Audit              map[string]any
}

type EmptyArgs struct{}

type EnvDoctorArgs struct {
	Env string `json:"env,omitempty" jsonschema:"description=Configured environment name to inspect"`
	All bool   `json:"all,omitempty" jsonschema:"description=Inspect all configured environments"`
}

type ProjectListArgs struct {
	Env                string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Limit              int    `json:"limit,omitempty" jsonschema:"description=Maximum projects to return; 0 means all"`
	SkipWorkflowCounts bool   `json:"skip_workflow_counts,omitempty" jsonschema:"description=Skip per-project workflow count queries for faster project inventory"`
}

type WorkflowListArgs struct {
	Env     string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Project string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	Limit   int    `json:"limit,omitempty" jsonschema:"description=Maximum workflows to return"`
}

type WorkflowGetArgs struct {
	Env        string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Project    string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	Identifier string `json:"identifier" jsonschema:"required,description=Workflow id or exact name"`
}

type WorkflowValidateArgs struct {
	File        string `json:"file" jsonschema:"required,description=Workspace-local workflow JSON file"`
	Env         string `json:"env,omitempty" jsonschema:"description=Optional environment for environment-aware validation"`
	Project     string `json:"project,omitempty" jsonschema:"description=Project name or id for context-aware messages"`
	AllowActive bool   `json:"allow_active,omitempty" jsonschema:"description=Permit active=true in local workflow JSON"`
}

type WorkflowFileCompareArgs struct {
	Env         string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	File        string `json:"file" jsonschema:"required,description=Workspace-local workflow JSON file"`
	ID          string `json:"id,omitempty" jsonschema:"description=Explicit remote workflow id"`
	Project     string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	AllowActive bool   `json:"allow_active,omitempty" jsonschema:"description=Permit active=true in local workflow JSON"`
}

type WorkflowIssuesArgs struct {
	Env                 string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID                  string `json:"id" jsonschema:"required,description=Remote workflow id"`
	Project             string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	CredentialPreflight string `json:"credential_preflight,omitempty" jsonschema:"description=Credential preflight mode: fail, warn, or skip"`
}

type WorkflowDependenciesArgs struct {
	Env  string `json:"env,omitempty" jsonschema:"description=Target n8n environment name for remote dependency checks"`
	File string `json:"file,omitempty" jsonschema:"description=Workspace-local workflow JSON file for local dependency checks"`
	ID   string `json:"id,omitempty" jsonschema:"description=Remote workflow id for remote dependency checks"`
}

type ExecutionListArgs struct {
	Env      string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Project  string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	Workflow string `json:"workflow,omitempty" jsonschema:"description=Workflow name or id to filter by"`
	Status   string `json:"status,omitempty" jsonschema:"description=Execution status to filter by"`
	Limit    int    `json:"limit,omitempty" jsonschema:"description=Maximum executions to return"`
}

type ExecutionIDArgs struct {
	Env string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID  string `json:"id" jsonschema:"required,description=Execution id"`
}

type ExecutionWaitArgs struct {
	Env               string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID                string `json:"id" jsonschema:"required,description=Execution id"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty" jsonschema:"description=Maximum seconds to wait"`
	IntervalSeconds   int    `json:"interval_seconds,omitempty" jsonschema:"description=Polling interval seconds"`
	DiagnoseOnFailure string `json:"diagnose_on_failure,omitempty" jsonschema:"description=auto, always, or never"`
}

type ExecutionDiagnoseArgs struct {
	Env   string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID    string `json:"id" jsonschema:"required,description=Execution id"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=Maximum node-run log rows to return"`
}

type MutationFields struct {
	DryRun             *bool  `json:"dry_run,omitempty" jsonschema:"description=Preview without mutation; defaults to true"`
	ConfirmMutation    bool   `json:"confirm_mutation,omitempty" jsonschema:"description=Required true for non-dry-run mutations"`
	ConfirmationPhrase string `json:"confirmation_phrase,omitempty" jsonschema:"description=Phrase returned by a matching prior dry run; required for production mutations"`
}

type WorkflowDeployArgs struct {
	Env                 string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	File                string `json:"file" jsonschema:"required,description=Workspace-local workflow JSON file"`
	ID                  string `json:"id,omitempty" jsonschema:"description=Explicit remote workflow id to update"`
	Project             string `json:"project,omitempty" jsonschema:"description=Target project name or id"`
	Reason              string `json:"reason,omitempty" jsonschema:"description=Backup/deploy reason label"`
	Activate            bool   `json:"activate,omitempty" jsonschema:"description=Activate after deploy"`
	AllowActive         bool   `json:"allow_active,omitempty" jsonschema:"description=Permit active=true in local workflow JSON"`
	CredentialPreflight string `json:"credential_preflight,omitempty" jsonschema:"description=Credential preflight mode: fail or warn for real mutations; skip is only allowed for dry runs"`
	BackupFile          string `json:"backup_file,omitempty" jsonschema:"description=Workspace-local backup file path"`
	BackupDir           string `json:"backup_dir,omitempty" jsonschema:"description=Workspace-local backup directory"`
	MutationFields
}

type WorkflowCreateArgs struct {
	Env                 string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	File                string `json:"file" jsonschema:"required,description=Workspace-local workflow JSON file"`
	Project             string `json:"project,omitempty" jsonschema:"description=Target project name or id"`
	Activate            bool   `json:"activate,omitempty" jsonschema:"description=Activate after create"`
	AllowActive         bool   `json:"allow_active,omitempty" jsonschema:"description=Permit active=true in local workflow JSON"`
	CredentialPreflight string `json:"credential_preflight,omitempty" jsonschema:"description=Credential preflight mode: fail or warn for real mutations; skip is only allowed for dry runs"`
	MutationFields
}

type WorkflowMoveArgs struct {
	Env              string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Identifier       string `json:"identifier" jsonschema:"required,description=Workflow id or exact name"`
	Project          string `json:"project" jsonschema:"required,description=Target project name or id"`
	ShareCredentials bool   `json:"share_credentials,omitempty" jsonschema:"description=Share associated credentials with destination project"`
	BackupFile       string `json:"backup_file,omitempty" jsonschema:"description=Workspace-local backup file path"`
	BackupDir        string `json:"backup_dir,omitempty" jsonschema:"description=Workspace-local backup directory"`
	MutationFields
}

type WorkflowCloneArgs struct {
	Env        string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Identifier string `json:"identifier" jsonschema:"required,description=Workflow id or exact name"`
	Project    string `json:"project,omitempty" jsonschema:"description=Target project name or id"`
	Name       string `json:"name,omitempty" jsonschema:"description=Name for the cloned workflow"`
	Activate   bool   `json:"activate,omitempty" jsonschema:"description=Activate the cloned workflow"`
	MutationFields
}

type WorkflowRunArgs struct {
	Env               string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Identifier        string `json:"identifier" jsonschema:"required,description=Workflow id or exact name"`
	Project           string `json:"project,omitempty" jsonschema:"description=Project name or id"`
	Wait              bool   `json:"wait,omitempty" jsonschema:"description=Wait for execution completion"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty" jsonschema:"description=Maximum seconds to wait"`
	DiagnoseOnFailure string `json:"diagnose_on_failure,omitempty" jsonschema:"description=auto, always, or never"`
	StartNode         string `json:"start_node,omitempty" jsonschema:"description=Node name to start from when supported"`
	DestinationNode   string `json:"destination_node,omitempty" jsonschema:"description=Destination node name when supported"`
	Input             string `json:"input,omitempty" jsonschema:"description=Workspace-local JSON input file"`
	MutationFields
}

type WorkflowToggleArgs struct {
	Env        string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Identifier string `json:"identifier" jsonschema:"required,description=Workflow id or exact name"`
	MutationFields
}

type WorkflowCleanupArgs struct {
	Env        string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	Project    string `json:"project" jsonschema:"required,description=Target project name or id"`
	ID         string `json:"id,omitempty" jsonschema:"description=Workflow id to delete"`
	Prefix     string `json:"prefix,omitempty" jsonschema:"description=Workflow name prefix; real cleanup by prefix is blocked in MCP"`
	BackupFile string `json:"backup_file,omitempty" jsonschema:"description=Workspace-local backup file path"`
	BackupDir  string `json:"backup_dir,omitempty" jsonschema:"description=Workspace-local backup directory"`
	MutationFields
}

type WorkflowRebindCredentialArgs struct {
	Env                 string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID                  string `json:"id" jsonschema:"required,description=Remote workflow id"`
	Node                string `json:"node,omitempty" jsonschema:"description=Node name to rebind"`
	Credential          string `json:"credential,omitempty" jsonschema:"description=Credential name or id"`
	AllGoogleDrive      string `json:"all_google_drive,omitempty" jsonschema:"description=Rebind all Google Drive nodes to this credential name or id"`
	CredentialPreflight string `json:"credential_preflight,omitempty" jsonschema:"description=Credential preflight mode: fail or warn for real mutations; skip is only allowed for dry runs"`
	BackupFile          string `json:"backup_file,omitempty" jsonschema:"description=Workspace-local backup file path"`
	BackupDir           string `json:"backup_dir,omitempty" jsonschema:"description=Workspace-local backup directory"`
	MutationFields
}

type ExecutionRetryArgs struct {
	Env               string `json:"env" jsonschema:"required,description=Target n8n environment name"`
	ID                string `json:"id" jsonschema:"required,description=Execution id"`
	LoadWorkflow      bool   `json:"load_workflow,omitempty" jsonschema:"description=Load latest workflow definition when retrying"`
	Wait              bool   `json:"wait,omitempty" jsonschema:"description=Wait for retried execution completion"`
	DiagnoseOnFailure string `json:"diagnose_on_failure,omitempty" jsonschema:"description=auto, always, or never"`
	MutationFields
}

var toolNames = []string{
	"version",
	"env_list",
	"env_doctor",
	"project_list",
	"workflow_list",
	"workflow_get",
	"workflow_validate",
	"workflow_diff",
	"workflow_drift",
	"workflow_issues",
	"workflow_dependencies",
	"workflow_doctor",
	"workflow_deploy",
	"workflow_create",
	"workflow_move",
	"workflow_clone",
	"workflow_run",
	"workflow_activate",
	"workflow_deactivate",
	"workflow_cleanup",
	"workflow_rebind_credential",
	"execution_list",
	"execution_get",
	"execution_wait",
	"execution_failures",
	"execution_diagnose",
	"execution_retry",
}

func New(cfg Config) (*Server, error) {
	workingDir := strings.TrimSpace(cfg.WorkingDir)
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	absDir, err := filepath.Abs(workingDir)
	if err != nil {
		return nil, err
	}
	resolvedDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		return nil, err
	}
	if cfg.RunCLI == nil {
		return nil, fmt.Errorf("mcp server requires a CLI runner")
	}
	if cfg.ToolTimeout <= 0 {
		cfg.ToolTimeout = 2 * time.Minute
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.ServerVersion == "" {
		cfg.ServerVersion = "dev"
	}
	server := &Server{
		workingDir:    resolvedDir,
		runCLI:        cfg.RunCLI,
		toolTimeout:   cfg.ToolTimeout,
		now:           cfg.Now,
		serverVersion: cfg.ServerVersion,
		eofGrace:      cfg.EOFGrace,
		confirmations: make(map[string]time.Time),
	}
	if _, err := server.buildMCPServer(stdio.NewStdioServerTransportWithIO(strings.NewReader(""), io.Discard)); err != nil {
		return nil, err
	}
	return server, nil
}

func ToolNames() []string {
	names := append([]string(nil), toolNames...)
	sort.Strings(names)
	return names
}

func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if in == nil {
		in = os.Stdin
	}
	if out == nil {
		out = os.Stdout
	}
	eofCh := make(chan struct{})
	tr := stdio.NewStdioServerTransportWithIO(&eofSignalReader{reader: in, done: eofCh}, out)
	server, err := s.buildMCPServer(tr)
	if err != nil {
		return err
	}
	if err := server.Serve(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		_ = tr.Close()
		if ctx.Err() == context.Canceled {
			return nil
		}
		return ctx.Err()
	case <-eofCh:
		grace := s.eofGrace
		if grace == 0 {
			grace = 100 * time.Millisecond
		}
		timer := time.NewTimer(grace)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
		_ = tr.Close()
		return nil
	}
}

type eofSignalReader struct {
	reader io.Reader
	done   chan<- struct{}
	once   sync.Once
}

func (r *eofSignalReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 && err == io.EOF {
		return n, nil
	}
	if err == io.EOF {
		r.once.Do(func() {
			close(r.done)
		})
	}
	return n, err
}

func (s *Server) buildMCPServer(tr transport.Transport) (*mcp.Server, error) {
	server := mcp.NewServer(
		tr,
		mcp.WithName("n8nctl"),
		mcp.WithVersion(s.serverVersion),
		mcp.WithInstructions("Use these local tools to validate, inspect, dry-run, and guardedly operate n8n workflows through the existing n8nctl CLI safety model."),
	)
	if err := registerTool(server, "version", "Print n8nctl build and release metadata.", s.handleVersion); err != nil {
		return nil, err
	}
	if err := registerTool(server, "env_list", "List configured n8n environments.", s.handleEnvList); err != nil {
		return nil, err
	}
	if err := registerTool(server, "env_doctor", "Check configured environment variables without printing secret values.", s.handleEnvDoctor); err != nil {
		return nil, err
	}
	if err := registerTool(server, "project_list", "List n8n projects with compact id, name, role/type, and workflow count metadata.", s.handleProjectList); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_list", "List workflows in an explicit n8n environment.", s.handleWorkflowList); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_get", "Fetch and sanitize a workflow by id or exact name.", s.handleWorkflowGet); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_validate", "Validate a workspace-local workflow JSON file.", s.handleWorkflowValidate); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_diff", "Diff a local workflow file against a remote workflow.", s.handleWorkflowDiff); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_drift", "Compare local workflow JSON with remote placement and activation state.", s.handleWorkflowDrift); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_issues", "Print setup blockers for a remote workflow.", s.handleWorkflowIssues); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_dependencies", "Report local or remote workflow dependencies.", s.handleWorkflowDependencies); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_doctor", "Run remote workflow issues, credential preflight, and dependency checks.", s.handleWorkflowDoctor); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_deploy", "Dry-run or guardedly create/update a workflow.", s.handleWorkflowDeploy); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_create", "Dry-run or guardedly create a workflow.", s.handleWorkflowCreate); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_move", "Dry-run or guardedly move a workflow to another project.", s.handleWorkflowMove); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_clone", "Dry-run or guardedly clone a workflow.", s.handleWorkflowClone); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_run", "Dry-run or guardedly run a workflow when the public API supports it.", s.handleWorkflowRun); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_activate", "Dry-run or guardedly activate a workflow.", s.handleWorkflowActivate); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_deactivate", "Dry-run or guardedly deactivate a workflow.", s.handleWorkflowDeactivate); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_cleanup", "Dry-run or guardedly delete explicitly selected workflows.", s.handleWorkflowCleanup); err != nil {
		return nil, err
	}
	if err := registerTool(server, "workflow_rebind_credential", "Dry-run or guardedly rebind workflow credential references.", s.handleWorkflowRebindCredential); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_list", "List workflow executions.", s.handleExecutionList); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_get", "Fetch one execution without raw execution data.", s.handleExecutionGet); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_wait", "Wait for an execution to finish.", s.handleExecutionWait); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_failures", "Print failed nodes and compact error context.", s.handleExecutionFailures); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_diagnose", "Summarize execution status, failures, and node-run logs.", s.handleExecutionDiagnose); err != nil {
		return nil, err
	}
	if err := registerTool(server, "execution_retry", "Dry-run or guardedly retry an execution when supported.", s.handleExecutionRetry); err != nil {
		return nil, err
	}
	return server, nil
}

func registerTool[T any](server *mcp.Server, name string, description string, handler func(context.Context, T) ToolResult) error {
	return server.RegisterTool(name, description, func(ctx context.Context, args T) (*mcp.ToolResponse, error) {
		result := handler(ctx, args)
		payload, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, err
		}
		return mcp.NewToolResponse(mcp.NewTextContent(string(payload))), nil
	})
}

func (s *Server) invoke(ctx context.Context, inv invocation) ToolResult {
	if inv.ToolName == "" {
		inv.ToolName = "unknown"
	}
	if inv.Remote && strings.TrimSpace(inv.Env) == "" {
		return s.securityResult(inv, "remote MCP tools require an explicit env")
	}
	if inv.Mutating && !inv.DryRun && !inv.ConfirmMutation {
		return s.securityResult(inv, "non-dry-run MCP mutations require confirm_mutation=true")
	}
	if inv.Mutating && !inv.DryRun {
		prod, err := s.isProductionEnv(inv.Env)
		if err != nil {
			return s.securityResult(inv, err.Error())
		}
		if prod {
			fingerprint := confirmationFingerprint(inv.ToolName, inv.CLIArgs)
			if !s.confirmationValid(fingerprint, inv.ConfirmationPhrase) {
				return s.securityResult(inv, "production MCP mutations require a matching confirmation_phrase returned by a prior dry run")
			}
			inv.CLIArgs = append(inv.CLIArgs, "--yes")
		}
	}

	if inv.Mutating && inv.DryRun {
		inv.CLIArgs = append(inv.CLIArgs, "--dry-run")
	}
	fullArgs := append([]string{"--json", "--no-color"}, inv.CLIArgs...)
	run := func() CLIResult {
		runCtx, cancel := context.WithTimeout(ctx, s.toolTimeout)
		defer cancel()
		return s.runCLI(runCtx, fullArgs)
	}
	var cliResult CLIResult
	if inv.Mutating && !inv.DryRun {
		s.mutationMu.Lock()
		cliResult = run()
		s.mutationMu.Unlock()
	} else {
		cliResult = run()
	}

	result := s.toolResultFromCLI(inv, cliResult)
	if inv.Mutating && inv.DryRun && cliResult.ExitCode == 0 {
		fingerprint := confirmationFingerprint(inv.ToolName, inv.CLIArgs[:len(inv.CLIArgs)-1])
		phrase, expiresAt := s.storeConfirmation(fingerprint)
		result.ConfirmationPhrase = phrase
		result.ConfirmationExpiresAt = expiresAt.UTC().Format(time.RFC3339)
		result.NextCall = mutationNextCall(inv, phrase)
	}
	if err := s.writeAudit(inv, result); err != nil {
		result.AuditError = err.Error()
	}
	return result
}

func (s *Server) securityResult(inv invocation, message string) ToolResult {
	result := ToolResult{
		Status: "error",
		Tool:   inv.ToolName,
		Error: map[string]any{
			"code":    "mcp_security_blocked",
			"message": message,
		},
	}
	if inv.Mutating && !inv.DryRun && !inv.ConfirmMutation {
		result.RetryCall = mutationRetryCall(inv)
	}
	_ = s.writeAudit(inv, result)
	return result
}

func (s *Server) toolResultFromCLI(inv invocation, cliResult CLIResult) ToolResult {
	result := ToolResult{
		Status:   "ok",
		Tool:     inv.ToolName,
		ExitCode: cliResult.ExitCode,
	}
	if cliResult.ExitCode != 0 {
		result.Status = "error"
	}
	payload, payloadOK := parseJSONText(cliResult.Stdout)
	if !payloadOK {
		payload, payloadOK = parseJSONText(cliResult.Stderr)
	}
	if payloadOK {
		payload = Sanitize(payload)
		if result.Status == "error" {
			result.Error = payload
		} else {
			result.Output = payload
		}
		return result
	}

	text := strings.TrimSpace(cliResult.Stdout)
	if text == "" {
		text = strings.TrimSpace(cliResult.Stderr)
	}
	text = truncateString(text)
	if result.Status == "error" {
		result.Error = map[string]any{"message": text}
	} else {
		result.Output = map[string]any{"text": text}
	}
	return result
}

func parseJSONText(text string) (any, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, false
	}
	var payload any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func (s *Server) isProductionEnv(env string) (bool, error) {
	cfg, _, err := config.LoadFromDir(s.workingDir)
	if err != nil {
		return false, err
	}
	envName, _, err := cfg.ResolveEnvironment(env)
	if err != nil {
		return false, err
	}
	return cfg.IsProductionEnv(envName), nil
}

func (s *Server) storeConfirmation(fingerprint string) (string, time.Time) {
	phrase := confirmationPhrase(fingerprint)
	expiresAt := s.now().Add(15 * time.Minute)
	s.confirmationMu.Lock()
	s.confirmations[fingerprint] = expiresAt
	s.confirmationMu.Unlock()
	return phrase, expiresAt
}

func (s *Server) confirmationValid(fingerprint string, phrase string) bool {
	if strings.TrimSpace(phrase) != confirmationPhrase(fingerprint) {
		return false
	}
	s.confirmationMu.Lock()
	defer s.confirmationMu.Unlock()
	expiresAt, ok := s.confirmations[fingerprint]
	return ok && s.now().Before(expiresAt)
}

func confirmationFingerprint(tool string, args []string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(tool))
	_, _ = hash.Write([]byte{0})
	for _, arg := range args {
		if arg == "--dry-run" || arg == "--yes" || arg == "--json" || arg == "--no-color" {
			continue
		}
		_, _ = hash.Write([]byte(arg))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func confirmationPhrase(fingerprint string) string {
	if len(fingerprint) > 16 {
		fingerprint = fingerprint[:16]
	}
	return "n8nctl-mcp-confirm:" + fingerprint
}

func mutationNextCall(inv invocation, phrase string) *MCPCall {
	if len(inv.NextCallArguments) == 0 {
		return nil
	}
	args := copyCallArguments(inv.NextCallArguments)
	args["dry_run"] = false
	args["confirm_mutation"] = true
	args["confirmation_phrase"] = phrase
	return &MCPCall{Tool: inv.ToolName, Arguments: args}
}

func mutationRetryCall(inv invocation) *MCPCall {
	if len(inv.NextCallArguments) == 0 {
		return nil
	}
	args := copyCallArguments(inv.NextCallArguments)
	args["dry_run"] = false
	args["confirm_mutation"] = true
	if strings.TrimSpace(inv.ConfirmationPhrase) != "" {
		args["confirmation_phrase"] = inv.ConfirmationPhrase
	}
	return &MCPCall{Tool: inv.ToolName, Arguments: args}
}

func copyCallArguments(input map[string]any) map[string]any {
	out := make(map[string]any, len(input)+3)
	for key, value := range input {
		out[key] = value
	}
	return out
}

func compactCallArguments(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				out[key] = typed
			}
		case bool:
			if typed {
				out[key] = typed
			}
		case int:
			if typed != 0 {
				out[key] = typed
			}
		case nil:
		default:
			out[key] = typed
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *Server) workspacePath(input string, mustExist bool) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", nil
	}
	var candidate string
	if filepath.IsAbs(input) {
		candidate = input
	} else {
		candidate = filepath.Join(s.workingDir, input)
	}
	candidate = filepath.Clean(candidate)
	if !pathWithinRoot(s.workingDir, candidate) {
		return "", fmt.Errorf("path %q escapes workspace", input)
	}
	if mustExist {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return "", err
		}
		if !pathWithinRoot(s.workingDir, resolved) {
			return "", fmt.Errorf("path %q resolves outside workspace", input)
		}
		return resolved, nil
	}
	ancestor := filepath.Dir(candidate)
	for {
		info, err := os.Stat(ancestor)
		if err == nil && info.IsDir() {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", err
			}
			if !pathWithinRoot(s.workingDir, resolved) {
				return "", fmt.Errorf("path %q parent resolves outside workspace", input)
			}
			return candidate, nil
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", fmt.Errorf("path %q has no existing parent directory", input)
		}
		ancestor = parent
	}
}

func pathWithinRoot(root string, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func (s *Server) writeAudit(inv invocation, result ToolResult) error {
	auditDir := filepath.Join(s.workingDir, ".n8nctl", "audit")
	if err := os.MkdirAll(auditDir, 0o700); err != nil {
		return err
	}
	auditPath := filepath.Join(auditDir, "mcp.jsonl")
	record := map[string]any{
		"timestamp": s.now().UTC().Format(time.RFC3339),
		"tool":      inv.ToolName,
		"env":       inv.Env,
		"mutating":  inv.Mutating,
		"dryRun":    inv.DryRun,
		"status":    result.Status,
		"exitCode":  result.ExitCode,
	}
	for key, value := range inv.Audit {
		record[key] = value
	}
	record = Sanitize(record).(map[string]any)
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(auditPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Write(append(payload, '\n')); err != nil {
		return err
	}
	return nil
}

var sensitiveKeyPattern = regexp.MustCompile(`(?i)(password|passphrase|token|secret|api[_-]?key|authorization|client[_-]?secret|private[_-]?key|access[_-]?key)`)

func Sanitize(value any) any {
	return sanitizeAny("", normalizeJSON(value))
}

func normalizeJSON(value any) any {
	payload, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var normalized any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return value
	}
	return normalized
}

func sanitizeAny(path string, value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			lowerKey := strings.ToLower(key)
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			switch {
			case sensitiveKeyPattern.MatchString(key):
				out[key] = "<redacted>"
			case lowerKey == "apibody":
				out[key] = "<omitted>"
			case lowerKey == "pindata" || lowerKey == "staticdata":
				out[key] = map[string]any{"omitted": true, "reason": "mcp_response_sanitization"}
			case lowerKey == "data" && strings.Contains(strings.ToLower(path), "execution"):
				out[key] = map[string]any{"omitted": true, "reason": "use execution_diagnose for summarized execution data"}
			default:
				out[key] = sanitizeAny(childPath, child)
			}
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = sanitizeAny(path, child)
		}
		return out
	case string:
		return truncateString(typed)
	default:
		return value
	}
}

func truncateString(value string) string {
	const max = 8000
	if len(value) <= max {
		return value
	}
	return value[:max] + "...<truncated>"
}

func dryRunValue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func addFlag(args *[]string, name string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	*args = append(*args, name, value)
}

func addBoolFlag(args *[]string, name string, value bool) {
	if value {
		*args = append(*args, name)
	}
}

func addIntFlag(args *[]string, name string, value int) {
	if value > 0 {
		*args = append(*args, name, fmt.Sprintf("%d", value))
	}
}

func addDurationFlag(args *[]string, name string, seconds int) {
	if seconds > 0 {
		*args = append(*args, name, fmt.Sprintf("%ds", seconds))
	}
}

func requireValue(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", name)
	}
	return nil
}

func (s *Server) addWorkspaceInput(args *[]string, value string) error {
	resolved, err := s.workspacePath(value, true)
	if err != nil {
		return err
	}
	*args = append(*args, resolved)
	return nil
}

func (s *Server) addOptionalWorkspaceFlag(args *[]string, flag string, value string, mustExist bool) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	resolved, err := s.workspacePath(value, mustExist)
	if err != nil {
		return err
	}
	addFlag(args, flag, resolved)
	return nil
}

func addCredentialPreflight(args *[]string, value string, dryRun bool) error {
	value = strings.TrimSpace(value)
	if !dryRun {
		switch value {
		case "":
			value = "fail"
		case "fail", "warn":
		case "skip":
			return fmt.Errorf("credential_preflight=skip is not allowed for non-dry-run MCP mutations")
		default:
			return fmt.Errorf("credential_preflight must be fail, warn, or skip")
		}
		addFlag(args, "--credential-preflight", value)
		return nil
	}
	switch value {
	case "", "fail", "warn", "skip":
		if value == "" {
			value = "fail"
		}
		addFlag(args, "--credential-preflight", value)
		return nil
	default:
		return fmt.Errorf("credential_preflight must be fail, warn, or skip")
	}
}
