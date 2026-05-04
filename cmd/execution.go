package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/alejandro-sg/n8nctl/internal/api"
	clierrors "github.com/alejandro-sg/n8nctl/internal/errors"
	"github.com/alejandro-sg/n8nctl/internal/output"
	"github.com/alejandro-sg/n8nctl/pkg/n8n"
)

func newExecutionCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execution",
		Short: "Inspect n8n workflow executions",
	}
	cmd.AddCommand(
		newExecutionListCmd(a),
		newExecutionGetCmd(a),
		newExecutionWaitCmd(a),
		newExecutionFailuresCmd(a),
		newExecutionDiagnoseCmd(a),
		newExecutionRetryCmd(a),
		newExecutionRerunItemCmd(a),
	)
	return cmd
}

func newExecutionListCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var workflowIdentifier string
	var status string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflow executions",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, false)
			if err != nil {
				return err
			}
			projectID := ""
			if project != nil {
				projectID = project.ID
			}

			workflowID := ""
			workflowName := ""
			if workflowIdentifier != "" {
				workflow, err := a.resolveWorkflowInProject(cmd.Context(), envCtx.Client, workflowIdentifier, projectID)
				if err != nil {
					return err
				}
				workflowID = workflow.ID.String()
				workflowName = workflow.Name
			}

			executions, err := envCtx.Client.ListExecutions(cmd.Context(), api.ListExecutionsParams{
				Status:     status,
				WorkflowID: workflowID,
				ProjectID:  projectID,
				Limit:      limit,
			})
			if err != nil {
				return a.mapAPIError(err, "failed to list executions", map[string]any{
					"environment": envCtx.EnvironmentName,
				})
			}
			sortExecutions(executions)

			workflowNames := make(map[string]string, len(executions))
			fetched := make(map[string]bool, len(executions))
			if workflowID != "" {
				workflowNames[workflowID] = workflowName
				fetched[workflowID] = true
			}
			for _, execution := range executions {
				id := execution.WorkflowID.String()
				if id == "" || fetched[id] {
					continue
				}
				fetched[id] = true
				workflow, err := envCtx.Client.GetWorkflow(cmd.Context(), id, true)
				if err == nil {
					workflowNames[id] = workflow.Name
				}
			}

			type executionRow struct {
				ID           string `json:"id"`
				WorkflowID   string `json:"workflowId"`
				WorkflowName string `json:"workflowName,omitempty"`
				Status       string `json:"status"`
				Mode         string `json:"mode"`
				StartedAt    string `json:"startedAt,omitempty"`
				StoppedAt    string `json:"stoppedAt,omitempty"`
			}

			rows := make([]executionRow, 0, len(executions))
			for _, execution := range executions {
				rows = append(rows, executionRow{
					ID:           execution.ID.String(),
					WorkflowID:   execution.WorkflowID.String(),
					WorkflowName: workflowNames[execution.WorkflowID.String()],
					Status:       execution.Status,
					Mode:         execution.Mode,
					StartedAt:    formatTime(execution.StartedAt),
					StoppedAt:    formatTime(execution.StoppedAt),
				})
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"project":     project,
					"executions":  rows,
				})
			}

			tableRows := make([][]string, 0, len(rows))
			for _, row := range rows {
				tableRows = append(tableRows, []string{
					row.ID,
					row.WorkflowID,
					row.WorkflowName,
					row.Status,
					row.Mode,
					row.StartedAt,
					row.StoppedAt,
				})
			}

			return output.WriteTable(a.deps.Streams.Out, []string{"ID", "WORKFLOW_ID", "WORKFLOW_NAME", "STATUS", "MODE", "STARTED_AT", "STOPPED_AT"}, tableRows)
		},
		Args: cobra.NoArgs,
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().StringVar(&workflowIdentifier, "workflow", "", "workflow name or id to filter by")
	cmd.Flags().StringVar(&status, "status", "", "execution status to filter by")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum executions to show")
	return cmd
}

func newExecutionGetCmd(a *app) *cobra.Command {
	var envName string
	var includeData bool

	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Fetch one execution",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			execution, err := envCtx.Client.GetExecution(cmd.Context(), args[0], includeData)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch execution", map[string]any{"executionId": args[0]})
			}
			return a.printOrText(map[string]any{
				"status":      "ok",
				"environment": envCtx.EnvironmentName,
				"execution":   execution,
			}, n8n.MustPrettyJSON(execution)+"\n")
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().BoolVar(&includeData, "include-data", false, "include execution run data")
	return cmd
}

func newExecutionWaitCmd(a *app) *cobra.Command {
	var envName string
	var timeout time.Duration
	var interval time.Duration
	var diagnoseOnFailure string

	cmd := &cobra.Command{
		Use:   "wait <id>",
		Short: "Wait for an execution to finish",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			execution, err := a.waitExecution(cmd.Context(), envCtx, args[0], timeout, interval)
			if err != nil {
				return err
			}
			payload := map[string]any{
				"status":      execution.Status,
				"environment": envCtx.EnvironmentName,
				"execution":   execution,
			}
			text := fmt.Sprintf("Execution %s finished with status=%s\n", execution.ID.String(), execution.Status)
			if shouldDiagnoseExecution(diagnoseOnFailure, *execution) {
				diagnosis, err := a.executionDiagnosis(cmd.Context(), envCtx, execution.ID.String(), 25)
				if err != nil {
					return err
				}
				payload["diagnosis"] = diagnosis
				text += renderDiagnosisText(diagnosis)
			}
			if err := a.printOrText(payload, text); err != nil {
				return err
			}
			if a.opts.CI && executionFailed(*execution) {
				return clierrors.New(clierrors.ExitAPI, clierrors.CodeExecutionFailed, "execution finished unsuccessfully", map[string]any{"executionId": execution.ID.String(), "status": execution.Status})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait")
	cmd.Flags().DurationVar(&interval, "interval", 2*time.Second, "poll interval")
	cmd.Flags().StringVar(&diagnoseOnFailure, "diagnose-on-failure", "auto", "diagnose failed executions: auto, always, or never")
	return cmd
}

func newExecutionFailuresCmd(a *app) *cobra.Command {
	var envName string

	cmd := &cobra.Command{
		Use:   "failures <id>",
		Short: "Print failed nodes and compact error context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			execution, err := envCtx.Client.GetExecution(cmd.Context(), args[0], true)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch execution failures", map[string]any{"executionId": args[0]})
			}
			diagnosis := diagnoseExecution(*execution, 0)
			addExecutionURL(&diagnosis, envCtx, *execution)
			failures := diagnosis.Failures
			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"summary":     diagnosis.Summary,
					"failures":    failures,
					"hints":       diagnosis.Hints,
				})
			}
			if len(failures) == 0 {
				_, err = fmt.Fprintf(a.deps.Streams.Out, "No execution failures found for %s; status=%s lastNode=%s\n", execution.ID.String(), execution.Status, diagnosis.Summary.LastNodeExecuted)
				return err
			}
			rows := make([][]string, 0, len(failures))
			for _, failure := range failures {
				rows = append(rows, []string{failure.NodeName, fmt.Sprintf("%d", failure.RunIndex), failure.ItemIndex, failure.Message, failure.Hint, failure.Path})
			}
			if err := output.WriteTable(a.deps.Streams.Out, []string{"NODE", "RUN", "ITEM", "MESSAGE", "HINT", "PATH"}, rows); err != nil {
				return err
			}
			if len(diagnosis.Hints) > 0 {
				fmt.Fprintln(a.deps.Streams.Out, "\nTroubleshooting hints:")
				for _, hint := range diagnosis.Hints {
					fmt.Fprintf(a.deps.Streams.Out, "- %s\n", hint)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	return cmd
}

func newExecutionDiagnoseCmd(a *app) *cobra.Command {
	var envName string
	var limit int

	cmd := &cobra.Command{
		Use:   "diagnose <id>",
		Short: "Summarize execution status, failed nodes, and node-run logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			execution, err := envCtx.Client.GetExecution(cmd.Context(), args[0], true)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch execution diagnosis", map[string]any{"executionId": args[0]})
			}
			diagnosis := diagnoseExecution(*execution, limit)
			addExecutionURL(&diagnosis, envCtx, *execution)
			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"diagnosis":   diagnosis,
				})
			}

			s := diagnosis.Summary
			fmt.Fprintf(a.deps.Streams.Out, "Execution %s status=%s workflow=%s mode=%s duration=%s\n", s.ID, s.Status, s.WorkflowID, s.Mode, s.Duration)
			if s.LastNodeExecuted != "" {
				fmt.Fprintf(a.deps.Streams.Out, "Last node executed: %s\n", s.LastNodeExecuted)
			}
			if s.TopLevelError != "" {
				fmt.Fprintf(a.deps.Streams.Out, "Top-level error: %s\n", s.TopLevelError)
			}

			if len(diagnosis.Failures) > 0 {
				fmt.Fprintln(a.deps.Streams.Out, "\nFailures:")
				rows := make([][]string, 0, len(diagnosis.Failures))
				for _, failure := range diagnosis.Failures {
					rows = append(rows, []string{failure.NodeName, fmt.Sprintf("%d", failure.RunIndex), failure.ItemIndex, failure.ErrorName, failure.Message, failure.Hint})
				}
				if err := output.WriteTable(a.deps.Streams.Out, []string{"NODE", "RUN", "ITEM", "ERROR", "MESSAGE", "HINT"}, rows); err != nil {
					return err
				}
			} else {
				fmt.Fprintln(a.deps.Streams.Out, "\nFailures: none found in execution data")
			}

			if len(diagnosis.Runs) > 0 {
				fmt.Fprintln(a.deps.Streams.Out, "\nNode run log:")
				rows := make([][]string, 0, len(diagnosis.Runs))
				for _, run := range diagnosis.Runs {
					rows = append(rows, []string{run.NodeName, fmt.Sprintf("%d", run.RunIndex), run.Status, run.StartTime, fmt.Sprintf("%d", run.ExecutionTimeMS), fmt.Sprintf("%d", run.Items), run.Message})
				}
				if err := output.WriteTable(a.deps.Streams.Out, []string{"NODE", "RUN", "STATUS", "START", "MS", "ITEMS", "MESSAGE"}, rows); err != nil {
					return err
				}
			}

			if len(diagnosis.Hints) > 0 {
				fmt.Fprintln(a.deps.Streams.Out, "\nTroubleshooting hints:")
				for _, hint := range diagnosis.Hints {
					fmt.Fprintf(a.deps.Streams.Out, "- %s\n", hint)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().IntVar(&limit, "limit", 25, "maximum node-run log rows to show; 0 shows all")
	return cmd
}

func newExecutionRetryCmd(a *app) *cobra.Command {
	var envName string
	var loadWorkflow bool
	var wait bool
	var diagnoseOnFailure string

	cmd := &cobra.Command{
		Use:   "retry <id>",
		Short: "Retry an execution when supported by the public API",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":      "dry-run",
					"environment": envCtx.EnvironmentName,
					"executionId": args[0],
					"actions":     []string{"retry"},
				}, fmt.Sprintf("Dry run retry execution %s\n", args[0]))
			}
			execution, err := envCtx.Client.RetryExecution(cmd.Context(), args[0], loadWorkflow)
			if err != nil {
				return a.mapAPIError(err, "failed to retry execution", map[string]any{"executionId": args[0]})
			}
			if wait {
				execution, err = a.waitExecution(cmd.Context(), envCtx, execution.ID.String(), 5*time.Minute, 2*time.Second)
				if err != nil {
					return err
				}
			}
			payload := map[string]any{
				"status":      execution.Status,
				"environment": envCtx.EnvironmentName,
				"execution":   execution,
			}
			text := fmt.Sprintf("Retried execution %s; status=%s\n", execution.ID.String(), execution.Status)
			if wait && shouldDiagnoseExecution(diagnoseOnFailure, *execution) {
				diagnosis, err := a.executionDiagnosis(cmd.Context(), envCtx, execution.ID.String(), 25)
				if err != nil {
					return err
				}
				payload["diagnosis"] = diagnosis
				text += renderDiagnosisText(diagnosis)
			}
			if err := a.printOrText(payload, text); err != nil {
				return err
			}
			if wait && a.opts.CI && executionFailed(*execution) {
				return clierrors.New(clierrors.ExitAPI, clierrors.CodeExecutionFailed, "execution retry finished unsuccessfully", map[string]any{"executionId": execution.ID.String(), "status": execution.Status})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().BoolVar(&loadWorkflow, "load-workflow", false, "load latest workflow definition when retrying")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for retried execution completion")
	cmd.Flags().StringVar(&diagnoseOnFailure, "diagnose-on-failure", "auto", "diagnose failed executions after --wait: auto, always, or never")
	return cmd
}

func newExecutionRerunItemCmd(a *app) *cobra.Command {
	var envName string
	var nodeName string
	var itemIndex int
	var loop bool
	var max int

	cmd := &cobra.Command{
		Use:   "rerun-item <id>",
		Short: "Explain public-API support for rerunning one execution item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			return clierrors.New(clierrors.ExitAPI, clierrors.CodeUnsupportedEndpoint, "rerun one item is not exposed as a stable n8n public API endpoint on this target", map[string]any{
				"environment": envCtx.EnvironmentName,
				"executionId": args[0],
				"node":        nodeName,
				"item":        itemIndex,
				"loop":        loop,
				"max":         max,
				"guidance":    "use execution retry when possible, or expose a webhook/manual trigger path for item-level replay",
			})
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&nodeName, "node", "", "failed node name")
	cmd.Flags().IntVar(&itemIndex, "item", 0, "item index to rerun")
	cmd.Flags().BoolVar(&loop, "loop", false, "continue rerunning while failures remain")
	cmd.Flags().IntVar(&max, "max", 10, "maximum rerun attempts when --loop is set")
	return cmd
}

func (a *app) waitExecution(ctx context.Context, envCtx *environmentContext, executionID string, timeout time.Duration, interval time.Duration) (*n8n.Execution, error) {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		execution, err := envCtx.Client.GetExecution(ctx, executionID, false)
		if err != nil {
			return nil, a.mapAPIError(err, "failed to fetch execution while waiting", map[string]any{"executionId": executionID})
		}
		if executionTerminal(*execution) {
			return execution, nil
		}
		if time.Now().After(deadline) {
			return nil, clierrors.New(clierrors.ExitAPI, clierrors.CodeAPIFailure, "execution wait timed out", map[string]any{
				"executionId": executionID,
				"timeout":     timeout.String(),
			})
		}
		select {
		case <-ctx.Done():
			return nil, clierrors.Wrap(ctx.Err(), clierrors.ExitAPI, clierrors.CodeAPIFailure, "execution wait was cancelled", map[string]any{"executionId": executionID})
		case <-time.After(interval):
		}
	}
}

func executionTerminal(execution n8n.Execution) bool {
	if execution.Finished {
		return true
	}
	switch strings.ToLower(execution.Status) {
	case "success", "error", "failed", "crashed", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

type executionDiagnosis struct {
	Summary  executionSummary   `json:"summary"`
	Failures []executionFailure `json:"failures,omitempty"`
	Runs     []executionNodeRun `json:"runs,omitempty"`
	Hints    []string           `json:"hints,omitempty"`
}

type executionSummary struct {
	ID               string `json:"id"`
	WorkflowID       string `json:"workflowId,omitempty"`
	Status           string `json:"status,omitempty"`
	Mode             string `json:"mode,omitempty"`
	Finished         bool   `json:"finished"`
	StartedAt        string `json:"startedAt,omitempty"`
	StoppedAt        string `json:"stoppedAt,omitempty"`
	Duration         string `json:"duration,omitempty"`
	RetryOf          string `json:"retryOf,omitempty"`
	LastNodeExecuted string `json:"lastNodeExecuted,omitempty"`
	TopLevelError    string `json:"topLevelError,omitempty"`
	ExecutionURL     string `json:"executionUrl,omitempty"`
}

type executionNodeRun struct {
	NodeName        string `json:"nodeName"`
	RunIndex        int    `json:"runIndex"`
	Status          string `json:"status,omitempty"`
	StartTime       string `json:"startTime,omitempty"`
	ExecutionTimeMS int    `json:"executionTimeMs,omitempty"`
	Items           int    `json:"items"`
	Message         string `json:"message,omitempty"`
	Path            string `json:"path,omitempty"`
}

type executionFailure struct {
	NodeName    string `json:"nodeName,omitempty"`
	RunIndex    int    `json:"runIndex,omitempty"`
	ItemIndex   string `json:"itemIndex,omitempty"`
	ErrorName   string `json:"errorName,omitempty"`
	Message     string `json:"message"`
	Description string `json:"description,omitempty"`
	HTTPCode    string `json:"httpCode,omitempty"`
	Stack       string `json:"stack,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Path        string `json:"path,omitempty"`
}

func diagnoseExecution(execution n8n.Execution, limit int) executionDiagnosis {
	summary := summarizeExecution(execution)
	runs, failures := extractExecutionRunData(execution.Data)
	if len(failures) == 0 {
		failures = extractGenericExecutionFailures(execution.Data)
	}
	if len(failures) == 0 && summary.TopLevelError != "" {
		failures = append(failures, executionFailure{
			NodeName: summary.LastNodeExecuted,
			RunIndex: -1,
			Message:  summary.TopLevelError,
			Hint:     hintForError(summary.TopLevelError, "", ""),
			Path:     "data.resultData.error",
		})
	}
	if limit > 0 && len(runs) > limit {
		runs = runs[len(runs)-limit:]
	}
	return executionDiagnosis{
		Summary:  summary,
		Failures: dedupeExecutionFailures(failures),
		Runs:     runs,
		Hints:    hintsFromFailures(failures),
	}
}

func (a *app) executionDiagnosis(ctx context.Context, envCtx *environmentContext, executionID string, limit int) (executionDiagnosis, error) {
	execution, err := envCtx.Client.GetExecution(ctx, executionID, true)
	if err != nil {
		return executionDiagnosis{}, a.mapAPIError(err, "failed to fetch execution diagnosis", map[string]any{"executionId": executionID})
	}
	diagnosis := diagnoseExecution(*execution, limit)
	addExecutionURL(&diagnosis, envCtx, *execution)
	return diagnosis, nil
}

func addExecutionURL(diagnosis *executionDiagnosis, envCtx *environmentContext, execution n8n.Execution) {
	if diagnosis == nil || envCtx == nil || execution.ID.IsZero() || execution.WorkflowID.IsZero() {
		return
	}
	baseURL := strings.TrimRight(envCtx.Environment.BaseURL, "/")
	if baseURL == "" {
		return
	}
	diagnosis.Summary.ExecutionURL = fmt.Sprintf("%s/workflow/%s/executions/%s", baseURL, execution.WorkflowID.String(), execution.ID.String())
}

func shouldDiagnoseExecution(mode string, execution n8n.Execution) bool {
	switch strings.TrimSpace(mode) {
	case "", "auto":
		return executionFailed(execution)
	case "always":
		return true
	case "never":
		return false
	default:
		return executionFailed(execution)
	}
}

func executionFailed(execution n8n.Execution) bool {
	switch strings.ToLower(strings.TrimSpace(execution.Status)) {
	case "error", "failed", "crashed", "canceled", "cancelled":
		return true
	default:
		return false
	}
}

func renderDiagnosisText(diagnosis executionDiagnosis) string {
	var b strings.Builder
	s := diagnosis.Summary
	b.WriteString("Diagnosis:\n")
	if s.ExecutionURL != "" {
		fmt.Fprintf(&b, "- execution URL: %s\n", s.ExecutionURL)
	}
	if s.LastNodeExecuted != "" {
		fmt.Fprintf(&b, "- last node: %s\n", s.LastNodeExecuted)
	}
	if len(diagnosis.Failures) == 0 {
		b.WriteString("- failures: none found in execution data\n")
		return b.String()
	}
	for _, failure := range diagnosis.Failures {
		node := failure.NodeName
		if node == "" {
			node = s.LastNodeExecuted
		}
		fmt.Fprintf(&b, "- failed node=%s run=%d item=%s error=%s message=%s\n", node, failure.RunIndex, failure.ItemIndex, failure.ErrorName, failure.Message)
		if failure.Hint != "" {
			fmt.Fprintf(&b, "  fix: %s\n", failure.Hint)
		}
	}
	return b.String()
}

func summarizeExecution(execution n8n.Execution) executionSummary {
	summary := executionSummary{
		ID:         execution.ID.String(),
		WorkflowID: execution.WorkflowID.String(),
		Status:     execution.Status,
		Mode:       execution.Mode,
		Finished:   execution.Finished,
		StartedAt:  formatTime(execution.StartedAt),
		StoppedAt:  formatTime(execution.StoppedAt),
		RetryOf:    execution.RetryOf.String(),
	}
	if execution.StartedAt != nil && execution.StoppedAt != nil {
		summary.Duration = execution.StoppedAt.Sub(*execution.StartedAt).String()
	}
	resultData := mapFromAny(execution.Data["resultData"])
	summary.LastNodeExecuted = stringFromAny(resultData["lastNodeExecuted"])
	summary.TopLevelError = errorMessage(firstPresent(resultData, "error"))
	return summary
}

func extractExecutionRunData(data map[string]any) ([]executionNodeRun, []executionFailure) {
	resultData := mapFromAny(data["resultData"])
	runData := mapFromAny(resultData["runData"])
	if len(runData) == 0 {
		return nil, nil
	}

	nodeNames := make([]string, 0, len(runData))
	for nodeName := range runData {
		nodeNames = append(nodeNames, nodeName)
	}
	sort.Strings(nodeNames)

	runs := make([]executionNodeRun, 0)
	failures := make([]executionFailure, 0)
	for _, nodeName := range nodeNames {
		nodeRuns := sliceFromAny(runData[nodeName])
		for runIndex, rawRun := range nodeRuns {
			run := mapFromAny(rawRun)
			path := fmt.Sprintf("data.resultData.runData.%s[%d]", nodeName, runIndex)
			errorValue := firstPresent(run, "error")
			message := errorMessage(errorValue)
			status := executionRunStatus(run, message)
			nodeRun := executionNodeRun{
				NodeName:        nodeName,
				RunIndex:        runIndex,
				Status:          status,
				StartTime:       stringFromAny(firstPresent(run, "startTime")),
				ExecutionTimeMS: intFromAny(firstPresent(run, "executionTime")),
				Items:           countRunItems(run),
				Message:         message,
				Path:            path,
			}
			runs = append(runs, nodeRun)
			if message == "" {
				continue
			}
			errMap := mapFromAny(errorValue)
			failure := executionFailure{
				NodeName:    nodeName,
				RunIndex:    runIndex,
				ItemIndex:   stringFromAny(firstPresent(run, "itemIndex", "item")),
				ErrorName:   stringFromAny(firstPresent(errMap, "name", "type")),
				Message:     message,
				Description: stringFromAny(firstPresent(errMap, "description", "cause")),
				HTTPCode:    stringFromAny(firstPresent(errMap, "httpCode", "statusCode", "code")),
				Stack:       firstLine(stringFromAny(firstPresent(errMap, "stack"))),
				Path:        path,
			}
			failure.Hint = hintForError(failure.Message, failure.HTTPCode, failure.ErrorName)
			failures = append(failures, failure)
		}
	}
	return runs, failures
}

func extractGenericExecutionFailures(data map[string]any) []executionFailure {
	failures := make([]executionFailure, 0)
	var walk func(value any, path string, nodeName string)
	walk = func(value any, path string, nodeName string) {
		switch typed := value.(type) {
		case map[string]any:
			currentNode := nodeName
			if name, ok := typed["nodeName"].(string); ok && name != "" {
				currentNode = name
			}
			if node, ok := typed["node"].(map[string]any); ok {
				if name, ok := node["name"].(string); ok && name != "" {
					currentNode = name
				}
			}
			if errValue, ok := typed["error"]; ok {
				message := errorMessage(errValue)
				failures = append(failures, executionFailure{
					NodeName:  currentNode,
					RunIndex:  -1,
					ItemIndex: stringFromAny(firstPresent(typed, "itemIndex", "item")),
					Message:   message,
					Hint:      hintForError(message, "", ""),
					Path:      path,
				})
			}
			for key, child := range typed {
				childPath := key
				if path != "" {
					childPath = path + "." + key
				}
				walk(child, childPath, currentNode)
			}
		case []any:
			for i, child := range typed {
				childPath := fmt.Sprintf("%s[%d]", path, i)
				walk(child, childPath, nodeName)
			}
		}
	}
	walk(data, "", "")
	return failures
}

func executionRunStatus(run map[string]any, message string) string {
	for _, key := range []string{"executionStatus", "status"} {
		if status := stringFromAny(run[key]); status != "" {
			return status
		}
	}
	if message != "" {
		return "error"
	}
	return "success"
}

func countRunItems(run map[string]any) int {
	data := mapFromAny(run["data"])
	return countItems(data["main"])
}

func countItems(value any) int {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return 0
		}
		total := 0
		for _, child := range typed {
			total += countItems(child)
		}
		if total == 0 {
			return len(typed)
		}
		return total
	default:
		return 0
	}
}

func mapFromAny(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func sliceFromAny(value any) []any {
	if typed, ok := value.([]any); ok {
		return typed
	}
	return nil
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case float32:
		return int(typed)
	default:
		return 0
	}
}

func dedupeExecutionFailures(failures []executionFailure) []executionFailure {
	seen := make(map[string]struct{}, len(failures))
	deduped := make([]executionFailure, 0, len(failures))
	for _, failure := range failures {
		key := strings.Join([]string{failure.NodeName, fmt.Sprintf("%d", failure.RunIndex), failure.ItemIndex, failure.Message, failure.Path}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, failure)
	}
	return deduped
}

func hintsFromFailures(failures []executionFailure) []string {
	seen := map[string]struct{}{}
	hints := make([]string, 0)
	for _, failure := range failures {
		hint := failure.Hint
		if hint == "" {
			hint = hintForError(failure.Message, failure.HTTPCode, failure.ErrorName)
		}
		if hint == "" {
			continue
		}
		if _, ok := seen[hint]; ok {
			continue
		}
		seen[hint] = struct{}{}
		hints = append(hints, hint)
	}
	return hints
}

func hintForError(message, httpCode, errorName string) string {
	combined := strings.ToLower(strings.Join([]string{message, httpCode, errorName}, " "))
	switch {
	case strings.Contains(combined, "401"), strings.Contains(combined, "403"), strings.Contains(combined, "unauthorized"), strings.Contains(combined, "forbidden"):
		return "Check the credential bound to the failed node, including OAuth scopes and whether the credential is shared into the target project."
	case strings.Contains(combined, "credential"):
		return "Run workflow doctor or credential preflight for this workflow, then rebind or share the missing credential."
	case strings.Contains(combined, "404"), strings.Contains(combined, "not found"):
		return "Verify resource IDs, URLs, subworkflow IDs, and project ownership referenced by the failed node."
	case strings.Contains(combined, "timeout"), strings.Contains(combined, "timed out"), strings.Contains(combined, "etimedout"):
		return "Check downstream service availability and consider increasing node timeout/retry settings."
	case strings.Contains(combined, "429"), strings.Contains(combined, "rate limit"):
		return "The downstream service may be rate limiting; add retry/backoff or reduce concurrency."
	case strings.Contains(combined, "expression"), strings.Contains(combined, "undefined"), strings.Contains(combined, "cannot read"):
		return "Inspect the failed node expressions against the input item shape from the previous node."
	case strings.Contains(combined, "google") && strings.Contains(combined, "scope"):
		return "Review Google OAuth scopes on the credential and the HTTP Request authentication mode."
	default:
		return ""
	}
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func errorMessage(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		for _, key := range []string{"message", "description", "name"} {
			if text, ok := typed[key].(string); ok && text != "" {
				return text
			}
		}
	}
	return stringFromAny(value)
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexByte(value, '\n'); idx >= 0 {
		return value[:idx]
	}
	return value
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}
