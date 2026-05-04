package cmd

import (
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alejandro-sg/n8nctl/internal/api"
	"github.com/alejandro-sg/n8nctl/internal/auth"
	"github.com/alejandro-sg/n8nctl/internal/config"
	credentiallint "github.com/alejandro-sg/n8nctl/internal/credential"
	clierrors "github.com/alejandro-sg/n8nctl/internal/errors"
	"github.com/alejandro-sg/n8nctl/internal/output"
	workflowutil "github.com/alejandro-sg/n8nctl/internal/workflow"
	"github.com/alejandro-sg/n8nctl/pkg/n8n"
)

type Streams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

type Dependencies struct {
	Streams    Streams
	WorkingDir string
	Getenv     func(string) string
	HTTPClient *http.Client
	Now        func() time.Time
}

type rootOptions struct {
	JSON    bool
	NoColor bool
	Yes     bool
	DryRun  bool
	CI      bool
}

type app struct {
	deps Dependencies
	opts *rootOptions
}

type environmentContext struct {
	Config          *config.Config
	ConfigPath      string
	EnvironmentName string
	Environment     config.Environment
	Client          *api.Client
}

type resolvedProject struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type projectVerification struct {
	Status            string `json:"status"`
	WorkflowID        string `json:"workflowId,omitempty"`
	ExpectedProjectID string `json:"expectedProjectId,omitempty"`
	ActualProjectID   string `json:"actualProjectId,omitempty"`
	Message           string `json:"message,omitempty"`
}

const (
	credentialPreflightFail = "fail"
	credentialPreflightWarn = "warn"
	credentialPreflightSkip = "skip"
)

func Execute() int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return clierrors.ExitInternal
	}

	return ExecuteWithArgs(os.Args[1:], Dependencies{
		Streams: Streams{
			In:     os.Stdin,
			Out:    os.Stdout,
			ErrOut: os.Stderr,
		},
		WorkingDir: wd,
		Getenv:     os.Getenv,
		Now:        time.Now,
	})
}

func ExecuteWithArgs(args []string, deps Dependencies) int {
	return ExecuteWithContextAndArgs(context.Background(), args, deps)
}

func ExecuteWithContextAndArgs(ctx context.Context, args []string, deps Dependencies) int {
	if ctx == nil {
		ctx = context.Background()
	}
	application := newApp(deps)
	rootCmd := application.newRootCmd()
	rootCmd.SetContext(ctx)
	rootCmd.SetArgs(args)

	if err := rootCmd.Execute(); err != nil {
		application.renderError(err)
		return application.exitCode(err)
	}

	return clierrors.ExitOK
}

func newApp(deps Dependencies) *app {
	if deps.Streams.In == nil {
		deps.Streams.In = os.Stdin
	}
	if deps.Streams.Out == nil {
		deps.Streams.Out = os.Stdout
	}
	if deps.Streams.ErrOut == nil {
		deps.Streams.ErrOut = os.Stderr
	}
	if deps.Getenv == nil {
		deps.Getenv = os.Getenv
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.WorkingDir == "" {
		if wd, err := os.Getwd(); err == nil {
			deps.WorkingDir = wd
		}
	}

	return &app{
		deps: deps,
		opts: &rootOptions{},
	}
}

func (a *app) newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "n8nctl",
		Short:         "Manage n8n Cloud workflows from the terminal",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.PersistentFlags().BoolVar(&a.opts.JSON, "json", false, "output machine-readable JSON")
	cmd.PersistentFlags().BoolVar(&a.opts.NoColor, "no-color", false, "disable color output")
	cmd.PersistentFlags().BoolVar(&a.opts.Yes, "yes", false, "skip safety confirmations")
	cmd.PersistentFlags().BoolVar(&a.opts.DryRun, "dry-run", false, "preview mutating actions without sending API requests")
	cmd.PersistentFlags().BoolVar(&a.opts.CI, "ci", false, "use stable CI-oriented exit codes for validation, drift, and execution failures")

	cmd.AddCommand(
		newInitCmd(a),
		newEnvCmd(a),
		newProjectCmd(a),
		newWorkflowCmd(a),
		newExecutionCmd(a),
		newVersionCmd(a),
		newMCPCmd(a),
	)

	return cmd
}

func (a *app) exitCode(err error) int {
	if err == nil {
		return clierrors.ExitOK
	}
	if !a.opts.CI {
		return clierrors.ExitCode(err)
	}
	cliErr := clierrors.As(err)
	switch cliErr.Code {
	case clierrors.CodeValidationFailed:
		if asString(cliErr.Details["failureType"]) == "credential_preflight" {
			return clierrors.ExitCICredentialPreflight
		}
		return clierrors.ExitCIStructuralValidation
	case clierrors.CodeDriftFound:
		return clierrors.ExitCIDriftFound
	case clierrors.CodeExecutionFailed:
		return clierrors.ExitCIExecutionFailure
	default:
		return cliErr.ExitCode
	}
}

func (a *app) renderError(err error) {
	cliErr := clierrors.As(err)
	if a.opts.JSON {
		_ = output.WriteJSON(a.deps.Streams.ErrOut, map[string]any{
			"status":  "error",
			"code":    cliErr.Code,
			"message": cliErr.Message,
			"details": cliErr.Details,
		})
		return
	}

	fmt.Fprintln(a.deps.Streams.ErrOut, cliErr.Message)
	if cliErr.Code == clierrors.CodeSafetyBlocked {
		renderSafetyDetails(a.deps.Streams.ErrOut, cliErr.Details)
	}
	renderErrorInstructions(a.deps.Streams.ErrOut, cliErr.Details)

	if findings, ok := cliErr.Details["findings"].([]workflowutil.Finding); ok {
		for _, finding := range findings {
			fmt.Fprintf(a.deps.Streams.ErrOut, "%s [%s] %s", strings.ToUpper(finding.Severity), finding.Code, finding.Message)
			if finding.Path != "" {
				fmt.Fprintf(a.deps.Streams.ErrOut, " (%s)", finding.Path)
			}
			if finding.Remediation != "" {
				fmt.Fprintf(a.deps.Streams.ErrOut, "\n  fix: %s", finding.Remediation)
			}
			fmt.Fprintln(a.deps.Streams.ErrOut)
		}
		return
	}

	if rawFindings, ok := cliErr.Details["findings"].([]any); ok {
		for _, item := range rawFindings {
			if findingMap, ok := item.(map[string]any); ok {
				fmt.Fprintf(a.deps.Streams.ErrOut, "%s [%v] %v", strings.ToUpper(asString(findingMap["severity"])), findingMap["code"], findingMap["message"])
				if path := asString(findingMap["path"]); path != "" {
					fmt.Fprintf(a.deps.Streams.ErrOut, " (%s)", path)
				}
				fmt.Fprintln(a.deps.Streams.ErrOut)
			}
		}
	}
}

func renderErrorInstructions(w io.Writer, details map[string]any) {
	if len(details) == 0 {
		return
	}
	if summary := asString(details["summary"]); summary != "" && summary != "<nil>" {
		fmt.Fprintf(w, "%s\n", summary)
	}
	renderStringList(w, "next steps", details["instructions"])
	renderStringList(w, "safe checks", details["safeChecks"])
}

func renderStringList(w io.Writer, title string, value any) {
	switch typed := value.(type) {
	case []string:
		if len(typed) == 0 {
			return
		}
		fmt.Fprintf(w, "%s:\n", title)
		for _, item := range typed {
			fmt.Fprintf(w, "- %s\n", item)
		}
	case []any:
		if len(typed) == 0 {
			return
		}
		fmt.Fprintf(w, "%s:\n", title)
		for _, item := range typed {
			fmt.Fprintf(w, "- %s\n", asString(item))
		}
	}
}

func renderSafetyDetails(w io.Writer, details map[string]any) {
	if len(details) == 0 {
		return
	}
	if environment := asString(details["environment"]); environment != "" && environment != "<nil>" {
		fmt.Fprintf(w, "target environment: %s\n", environment)
	}
	if project, ok := details["project"].(*resolvedProject); ok && project != nil {
		fmt.Fprintf(w, "target project: %s (%s)\n", project.Name, project.ID)
	} else if projectMap, ok := details["project"].(map[string]any); ok {
		name := asString(projectMap["name"])
		id := asString(projectMap["id"])
		if name != "" && name != "<nil>" {
			fmt.Fprintf(w, "target project: %s (%s)\n", name, id)
		}
	}
	if workflowName := asString(details["workflowName"]); workflowName != "" && workflowName != "<nil>" {
		fmt.Fprintf(w, "workflow: %s", workflowName)
		if workflowID := asString(details["workflowId"]); workflowID != "" && workflowID != "<nil>" {
			fmt.Fprintf(w, " (%s)", workflowID)
		}
		fmt.Fprintln(w)
	}
	if action := asString(details["action"]); action != "" && action != "<nil>" {
		fmt.Fprintf(w, "planned action: %s\n", action)
	}
	if checked, ok := details["credentialsChecked"].(int); ok {
		fmt.Fprintf(w, "credentials checked: %d\n", checked)
	}
	if findings, ok := details["credentialFindings"].([]workflowutil.Finding); ok && len(findings) > 0 {
		errors, warnings := 0, 0
		for _, finding := range findings {
			switch finding.Severity {
			case "error":
				errors++
			case "warning":
				warnings++
			}
		}
		fmt.Fprintf(w, "credential findings: %d error(s), %d warning(s)\n", errors, warnings)
	}
	if rerun := asString(details["rerun"]); rerun != "" && rerun != "<nil>" {
		fmt.Fprintf(w, "rerun: %s\n", rerun)
	}
}

func (a *app) printJSON(value any) error {
	return output.WriteJSON(a.deps.Streams.Out, value)
}

func (a *app) printOrText(value any, text string) error {
	if a.opts.JSON {
		return a.printJSON(value)
	}
	_, err := fmt.Fprint(a.deps.Streams.Out, text)
	return err
}

func (a *app) loadConfig() (*config.Config, string, error) {
	return config.LoadFromDir(a.deps.WorkingDir)
}

func (a *app) envContext(envFlag string) (*environmentContext, error) {
	cfg, configPath, err := a.loadConfig()
	if err != nil {
		return nil, err
	}

	envName, env, err := cfg.ResolveEnvironment(envFlag)
	if err != nil {
		return nil, err
	}

	apiKey, err := auth.ResolveAPIKey(a.deps.Getenv, envName, env)
	if err != nil {
		return nil, err
	}

	return &environmentContext{
		Config:          cfg,
		ConfigPath:      configPath,
		EnvironmentName: envName,
		Environment:     env,
		Client:          api.NewClient(env.BaseURL, apiKey, a.deps.HTTPClient),
	}, nil
}

func (a *app) requireValidation(result workflowutil.ValidationResult) error {
	if !result.HasErrors() {
		return nil
	}

	return clierrors.New(clierrors.ExitUsage, clierrors.CodeValidationFailed, "workflow validation failed", map[string]any{
		"failureType":          "structural_validation",
		"workflowName":         result.WorkflowName,
		"file":                 result.File,
		"nodeCount":            result.NodeCount,
		"connectionCount":      result.ConnectionCount,
		"credentialReferences": result.CredentialReferences,
		"findings":             result.Findings,
	})
}

func (a *app) renderWarnings(warnings []workflowutil.Finding) {
	if len(warnings) == 0 || a.opts.JSON {
		return
	}
	for _, finding := range warnings {
		if finding.Severity != "warning" {
			continue
		}
		fmt.Fprintf(a.deps.Streams.ErrOut, "WARNING [%s] %s", finding.Code, finding.Message)
		if finding.Path != "" {
			fmt.Fprintf(a.deps.Streams.ErrOut, " (%s)", finding.Path)
		}
		fmt.Fprintln(a.deps.Streams.ErrOut)
	}
}

func (a *app) resolvePath(input string) string {
	if filepath.IsAbs(input) {
		return input
	}
	return filepath.Join(a.deps.WorkingDir, input)
}

func (a *app) mapAPIError(err error, message string, details map[string]any) error {
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		exitCode := clierrors.ExitAPI
		code := clierrors.CodeAPIFailure
		switch apiErr.StatusCode {
		case http.StatusNotFound:
			exitCode = clierrors.ExitResolution
			code = clierrors.CodeWorkflowNotFound
		case http.StatusUnauthorized, http.StatusForbidden:
			exitCode = clierrors.ExitAPI
			code = clierrors.CodeAPIFailure
		case http.StatusMethodNotAllowed, http.StatusNotImplemented:
			exitCode = clierrors.ExitAPI
			code = clierrors.CodeUnsupportedEndpoint
		}
		withStatus := cloneDetails(details)
		withStatus["statusCode"] = apiErr.StatusCode
		withStatus["apiMessage"] = apiErr.Message
		withStatus["apiBody"] = apiErr.Body
		withStatus["apiMethod"] = apiErr.Method
		withStatus["apiURL"] = apiErr.URL
		switch apiErr.StatusCode {
		case http.StatusMethodNotAllowed, http.StatusNotImplemented:
			withStatus["summary"] = "The target n8n instance does not expose this public API operation."
			withStatus["instructions"] = []string{
				"Check whether the n8n version supports the endpoint used by this command.",
				"Check API key scopes and plan/API availability if the endpoint exists in your version.",
				"Use the command's documented fallback path when available; n8nctl does not call private editor APIs.",
			}
		case http.StatusUnauthorized, http.StatusForbidden:
			withStatus["summary"] = "The n8n API rejected the request; this is usually an API key, scope, permission, or project-access issue."
			withStatus["instructions"] = []string{
				"Verify the API key is loaded from the expected environment variable.",
				"Verify the key has the required workflow, credential, project, or execution scope.",
				"Verify the API-key user has access to the target project and workflow.",
			}
		case http.StatusNotFound:
			if strings.Contains(strings.ToLower(apiErr.Message+" "+apiErr.Body), "endpoint") {
				withStatus["summary"] = "The target n8n instance may not support this public API endpoint."
			}
		}
		return clierrors.Wrap(err, exitCode, code, message, withStatus)
	}

	return clierrors.Wrap(err, clierrors.ExitAPI, clierrors.CodeAPIFailure, message, details)
}

func (a *app) resolveProject(ctx context.Context, envCtx *environmentContext, requested string, required bool) (*resolvedProject, error) {
	nameOrID := strings.TrimSpace(requested)
	if nameOrID == "" {
		nameOrID = strings.TrimSpace(envCtx.Environment.DefaultProject)
	}
	if nameOrID == "" {
		if required {
			return nil, clierrors.New(clierrors.ExitSafety, clierrors.CodeProjectNotFound, "no project selected; pass --project or set environments.<env>.default_project", map[string]any{
				"environment": envCtx.EnvironmentName,
			})
		}
		return nil, nil
	}

	projects, err := envCtx.Client.ListProjects(ctx, api.ListProjectsParams{})
	if err != nil {
		return nil, a.mapAPIError(err, "failed to list projects", map[string]any{
			"environment": envCtx.EnvironmentName,
			"project":     nameOrID,
		})
	}

	for _, project := range projects {
		if project.ID.String() == nameOrID {
			return &resolvedProject{ID: project.ID.String(), Name: project.Name}, nil
		}
	}

	matches := make([]n8n.Project, 0)
	for _, project := range projects {
		if project.Name == nameOrID {
			matches = append(matches, project)
		}
	}
	switch len(matches) {
	case 0:
		return nil, clierrors.New(clierrors.ExitResolution, clierrors.CodeProjectNotFound, fmt.Sprintf("project %q was not found", nameOrID), map[string]any{
			"project": nameOrID,
		})
	case 1:
		return &resolvedProject{ID: matches[0].ID.String(), Name: matches[0].Name}, nil
	default:
		return nil, clierrors.New(clierrors.ExitResolution, clierrors.CodeProjectAmbiguous, fmt.Sprintf("project name %q matched multiple projects", nameOrID), map[string]any{
			"project":    nameOrID,
			"matchCount": len(matches),
		})
	}
}

func (a *app) credentialPreflight(ctx context.Context, envCtx *environmentContext, workflowDoc n8n.Workflow, project *resolvedProject, mode string, fallback string) (credentiallint.Result, error) {
	mode, err := credentialPreflightMode(mode, envCtx.Config.Validation.CredentialPreflight, fallback)
	if err != nil {
		return credentiallint.Result{}, err
	}
	if mode == credentialPreflightSkip {
		return credentiallint.Result{Mode: mode, Skipped: true}, nil
	}
	credentials, err := envCtx.Client.ListCredentials(ctx, api.ListCredentialsParams{})
	if err != nil {
		return credentiallint.Result{}, a.mapAPIError(err, "failed to list credentials for preflight", map[string]any{
			"environment": envCtx.EnvironmentName,
		})
	}
	opts := credentiallint.LintOptions{}
	if project != nil {
		opts.ProjectID = project.ID
		opts.ProjectName = project.Name
	}
	result := credentiallint.Lint(workflowDoc, credentials, opts)
	result.Mode = mode
	if mode == credentialPreflightWarn {
		result.DowngradeErrors()
	}
	return result, nil
}

func (a *app) requireCredentialPreflight(result credentiallint.Result, workflowName string) error {
	if !result.HasErrors() {
		if a.opts.CI && len(result.Findings) > 0 {
			return clierrors.New(clierrors.ExitUsage, clierrors.CodeValidationFailed, "workflow credential preflight produced warnings", map[string]any{
				"failureType":  "credential_preflight",
				"workflowName": workflowName,
				"checked":      result.Checked,
				"findings":     result.Findings,
				"references":   result.References,
			})
		}
		return nil
	}
	return clierrors.New(clierrors.ExitUsage, clierrors.CodeValidationFailed, "workflow credential preflight failed", map[string]any{
		"failureType":  "credential_preflight",
		"workflowName": workflowName,
		"checked":      result.Checked,
		"findings":     result.Findings,
		"references":   result.References,
	})
}

func credentialPreflightMode(flagValue, configValue, fallback string) (string, error) {
	for _, value := range []string{flagValue, configValue, fallback} {
		mode := strings.TrimSpace(value)
		if mode == "" {
			continue
		}
		switch mode {
		case credentialPreflightFail, credentialPreflightWarn, credentialPreflightSkip:
			return mode, nil
		default:
			return "", clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "--credential-preflight must be fail, warn, or skip", map[string]any{"value": mode})
		}
	}
	return credentialPreflightFail, nil
}

func (a *app) requireProdConfirmation(envCtx *environmentContext, project *resolvedProject, workflowName string, workflowID string, action string, credentialResult *credentiallint.Result) error {
	if !envCtx.Config.IsProductionEnv(envCtx.EnvironmentName) || !envCtx.Config.Safety.RequireConfirmForProd || a.opts.Yes || a.opts.DryRun {
		return nil
	}
	details := map[string]any{
		"environment":  envCtx.EnvironmentName,
		"workflowName": workflowName,
		"workflowId":   workflowID,
		"action":       action,
		"rerun":        "rerun the same command with --yes after verifying the target env, project, workflow, and credential preflight",
	}
	if project != nil {
		details["project"] = project
	}
	if credentialResult != nil {
		details["credentialsChecked"] = credentialResult.Checked
		details["credentialFindings"] = credentialResult.Findings
	}
	return clierrors.New(clierrors.ExitSafety, clierrors.CodeSafetyBlocked, fmt.Sprintf("refusing to %s in production without --yes", action), details)
}

func (a *app) verifyWorkflowProject(ctx context.Context, envCtx *environmentContext, workflowID string, expected *resolvedProject) (projectVerification, *n8n.Workflow, error) {
	verification := projectVerification{
		Status:     "project_location_unverified",
		WorkflowID: workflowID,
	}
	if expected != nil {
		verification.ExpectedProjectID = expected.ID
	}
	workflow, err := envCtx.Client.GetWorkflow(ctx, workflowID, false)
	if err != nil {
		verification.Message = "unable to refetch workflow after mutation"
		return verification, nil, a.mapAPIError(err, "failed to verify workflow project placement", map[string]any{"workflowId": workflowID})
	}
	verification.ActualProjectID = workflow.ProjectID.String()
	if expected == nil {
		verification.Message = "no target project was selected"
		return verification, workflow, nil
	}
	if workflow.ProjectID.String() == expected.ID {
		verification.Status = "project_location_verified"
		verification.Message = "workflow is in the target project"
		return verification, workflow, nil
	}
	if workflow.ProjectID.String() != "" && workflow.ProjectID.String() != expected.ID {
		verification.Status = "project_location_mismatch"
		verification.Message = "workflow is not in the target project"
		return verification, workflow, clierrors.New(clierrors.ExitSafety, clierrors.CodeProjectMismatch, "workflow project placement verification failed", map[string]any{
			"workflowId":        workflowID,
			"expectedProjectId": expected.ID,
			"actualProjectId":   workflow.ProjectID.String(),
		})
	}
	workflows, err := envCtx.Client.ListWorkflows(ctx, api.ListWorkflowsParams{Name: workflow.Name, ProjectID: expected.ID, Limit: 250})
	if err != nil {
		verification.Message = "workflow was refetched but project-filtered verification failed"
		return verification, workflow, nil
	}
	for _, candidate := range workflows {
		if candidate.ID.String() == workflowID {
			verification.Status = "project_location_verified"
			verification.Message = "workflow was found in the project-filtered workflow list"
			return verification, workflow, nil
		}
	}
	verification.Message = "workflow API response did not include project metadata and project-filtered lookup did not confirm placement"
	return verification, workflow, nil
}

func (a *app) resolveWorkflow(ctx context.Context, client *api.Client, identifier string) (*n8n.Workflow, error) {
	return a.resolveWorkflowInProject(ctx, client, identifier, "")
}

func (a *app) resolveWorkflowInProject(ctx context.Context, client *api.Client, identifier string, projectID string) (*n8n.Workflow, error) {
	if strings.TrimSpace(identifier) == "" {
		return nil, clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow identifier is required", nil)
	}

	workflow, found, err := a.findWorkflowInProject(ctx, client, identifier, "", projectID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowNotFound, fmt.Sprintf("workflow %q was not found", identifier), nil)
	}
	return workflow, nil
}

func (a *app) findWorkflow(ctx context.Context, client *api.Client, identifier string, fallbackName string) (*n8n.Workflow, bool, error) {
	return a.findWorkflowInProject(ctx, client, identifier, fallbackName, "")
}

func (a *app) findWorkflowInProject(ctx context.Context, client *api.Client, identifier string, fallbackName string, projectID string) (*n8n.Workflow, bool, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier != "" {
		workflow, err := client.GetWorkflow(ctx, identifier, false)
		if err == nil {
			if projectID != "" && workflow.ProjectID.String() != "" && workflow.ProjectID.String() != projectID {
				return nil, false, nil
			}
			return workflow, true, nil
		}

		var apiErr *api.APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
			return nil, false, a.mapAPIError(err, "failed to fetch workflow by id", map[string]any{
				"identifier": identifier,
			})
		}

		fallbackName = identifier
	}

	name := strings.TrimSpace(fallbackName)
	if name == "" {
		return nil, false, nil
	}

	workflows, err := client.ListWorkflows(ctx, api.ListWorkflowsParams{Name: name, ProjectID: projectID})
	if err != nil {
		return nil, false, a.mapAPIError(err, "failed to search workflows", map[string]any{
			"name": name,
		})
	}

	matches := make([]n8n.Workflow, 0)
	for _, workflow := range workflows {
		if workflow.Name == name {
			matches = append(matches, workflow)
		}
	}

	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return &matches[0], true, nil
	default:
		return nil, false, clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowAmbiguous, fmt.Sprintf("workflow name %q matched multiple remote workflows", name), map[string]any{
			"name":       name,
			"matchCount": len(matches),
		})
	}
}

func (a *app) backupWorkflow(envCtx *environmentContext, workflow *n8n.Workflow) (string, error) {
	return a.backupWorkflowWithReason(envCtx, workflow, "")
}

func (a *app) backupWorkflowWithReason(envCtx *environmentContext, workflow *n8n.Workflow, reason string) (string, error) {
	return a.backupWorkflowWithOptions(envCtx, workflow, reason, "", "")
}

func (a *app) backupWorkflowWithOptions(envCtx *environmentContext, workflow *n8n.Workflow, reason string, backupFile string, backupDirOverride string) (string, error) {
	backupDir := strings.TrimSpace(backupDirOverride)
	if backupDir == "" {
		backupDir = envCtx.Config.BackupDir(envCtx.ConfigPath, envCtx.EnvironmentName)
	}
	backupPath := strings.TrimSpace(backupFile)
	if backupPath != "" && !filepath.IsAbs(backupPath) {
		backupPath = filepath.Join(a.deps.WorkingDir, backupPath)
	}
	if backupPath != "" {
		backupDir = filepath.Dir(backupPath)
	}
	if !filepath.IsAbs(backupDir) {
		backupDir = filepath.Join(a.deps.WorkingDir, backupDir)
	}
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to create backup directory", map[string]any{
			"dir": backupDir,
		})
	}

	if backupPath == "" {
		timestamp := a.deps.Now().UTC().Format("20060102T150405Z")
		parts := []string{timestamp, workflow.ID.String()}
		if label := backupLabel(reason); label != "" {
			parts = append(parts, label)
		}
		if sha := gitShortSHA(envCtx.Config.ConfigDir(envCtx.ConfigPath)); sha != "" {
			parts = append(parts, "git-"+sha)
		}
		fileName := strings.Join(parts, "-") + ".json"
		backupPath = filepath.Join(backupDir, fileName)
	}
	payload, err := stdjson.MarshalIndent(workflow, "", "  ")
	if err != nil {
		return "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to encode workflow backup", nil)
	}

	file, err := os.OpenFile(backupPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to write workflow backup", map[string]any{
			"path": backupPath,
		})
	}
	defer file.Close()
	if _, err := file.Write(payload); err != nil {
		return "", clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to write workflow backup", map[string]any{
			"path": backupPath,
		})
	}

	return backupPath, nil
}

func backupLabel(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range reason {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	return strings.Trim(b.String(), "-_")
}

func gitShortSHA(dir string) string {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--short=12", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func sortWorkflows(workflows []n8n.Workflow) {
	sort.Slice(workflows, func(i, j int) bool {
		if workflows[i].Name == workflows[j].Name {
			return workflows[i].ID.String() < workflows[j].ID.String()
		}
		return workflows[i].Name < workflows[j].Name
	})
}

func sortExecutions(executions []n8n.Execution) {
	sort.Slice(executions, func(i, j int) bool {
		left := time.Time{}
		right := time.Time{}
		if executions[i].StartedAt != nil {
			left = *executions[i].StartedAt
		}
		if executions[j].StartedAt != nil {
			right = *executions[j].StartedAt
		}
		if !left.Equal(right) {
			return left.After(right)
		}
		return executions[i].ID.String() > executions[j].ID.String()
	})
}

func formatTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func cloneDetails(details map[string]any) map[string]any {
	if len(details) == 0 {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(details))
	for key, value := range details {
		cloned[key] = value
	}
	return cloned
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}
