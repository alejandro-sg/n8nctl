package cmd

import (
	stdjson "encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/LogicMonitor-IT/n8nctl/internal/api"
	clierrors "github.com/LogicMonitor-IT/n8nctl/internal/errors"
	"github.com/LogicMonitor-IT/n8nctl/internal/output"
	workflowutil "github.com/LogicMonitor-IT/n8nctl/internal/workflow"
	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

func newWorkflowCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage n8n workflows",
	}

	cmd.AddCommand(
		newWorkflowListCmd(a),
		newWorkflowGetCmd(a),
		newWorkflowValidateCmd(a),
		newWorkflowIssuesCmd(a),
		newWorkflowDiffCmd(a),
		newWorkflowCreateCmd(a),
		newWorkflowDeployCmd(a),
		newWorkflowMoveCmd(a),
		newWorkflowCloneCmd(a),
		newWorkflowRunCmd(a),
		newWorkflowDependenciesCmd(a),
		newWorkflowDriftCmd(a),
		newWorkflowCleanupCmd(a),
		newWorkflowDoctorCmd(a),
		newWorkflowRebindCredentialCmd(a),
		newWorkflowActivateCmd(a),
		newWorkflowDeactivateCmd(a),
	)

	return cmd
}

func newWorkflowListCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List workflows in an environment",
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
			workflows, err := envCtx.Client.ListWorkflows(cmd.Context(), api.ListWorkflowsParams{
				Limit:             limit,
				ExcludePinnedData: true,
				ProjectID:         projectID,
			})
			if err != nil {
				return a.mapAPIError(err, "failed to list workflows", map[string]any{
					"environment": envCtx.EnvironmentName,
				})
			}
			sortWorkflows(workflows)

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"project":     project,
					"workflows":   workflows,
				})
			}

			rows := make([][]string, 0, len(workflows))
			for _, workflow := range workflows {
				rows = append(rows, []string{
					workflow.Name,
					workflow.ID.String(),
					fmt.Sprintf("%t", workflow.Active),
					formatTime(workflow.UpdatedAt),
				})
			}

			return output.WriteTable(a.deps.Streams.Out, []string{"NAME", "ID", "ACTIVE", "UPDATED_AT"}, rows)
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().IntVar(&limit, "limit", 50, "maximum workflows to show")
	return cmd
}

func newWorkflowGetCmd(a *app) *cobra.Command {
	var envName string
	var projectName string

	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Fetch a workflow by id or exact name",
		Args:  cobra.ExactArgs(1),
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
			workflow, err := a.resolveWorkflowInProject(cmd.Context(), envCtx.Client, args[0], projectID)
			if err != nil {
				return err
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"project":     project,
					"workflow":    workflow,
				})
			}

			payload, err := stdjson.MarshalIndent(workflow, "", "  ")
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to render workflow JSON", nil)
			}
			_, err = fmt.Fprintf(a.deps.Streams.Out, "%s\n", string(payload))
			return err
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	return cmd
}

func newWorkflowValidateCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var allowActive bool

	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Validate a local workflow JSON file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := a.resolvePath(args[0])

			opts := workflowutil.ValidationOptions{
				AllowActive:   allowActive,
				RuntimeEngine: "n8n-runtime",
			}
			if envName != "" {
				cfg, _, err := a.loadConfig()
				if err != nil {
					return err
				}
				resolvedEnvName, _, err := cfg.ResolveEnvironment(envName)
				if err != nil {
					return err
				}
				opts.EnvironmentName = resolvedEnvName
				opts.ProductionHosts = cfg.ProductionHosts(resolvedEnvName)
				opts.RuntimeEngine = cfg.Validation.Engine
				opts.RequireRemoteContext = cfg.Validation.RequireRemoteContext
				if projectName != "" {
					opts.ProjectName = projectName
				}
			}

			_, result, err := workflowutil.ValidateFile(filePath, opts)
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to validate workflow file", map[string]any{
					"path": filePath,
				})
			}

			a.renderWarnings(result.Warnings())
			if err := a.requireValidation(result); err != nil {
				return err
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":               "ok",
					"file":                 result.File,
					"workflowName":         result.WorkflowName,
					"nodeCount":            result.NodeCount,
					"connectionCount":      result.ConnectionCount,
					"credentialReferences": result.CredentialReferences,
					"warnings":             result.Warnings(),
				})
			}

			_, err = fmt.Fprintf(a.deps.Streams.Out, "OK %s\nname: %s\nnodes: %d\nconnections: %d\ncredentials: %d references\n",
				result.File,
				result.WorkflowName,
				result.NodeCount,
				result.ConnectionCount,
				result.CredentialReferences,
			)
			return err
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "environment name for environment-aware validation checks")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id for context-aware messages")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "permit active=true in workflow JSON")
	return cmd
}

func newWorkflowIssuesCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var projectName string
	var credentialPreflight string

	cmd := &cobra.Command{
		Use:   "issues --id <workflow-id>",
		Short: "Print setup blockers for a remote workflow",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			if strings.TrimSpace(workflowID) == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow issues requires --id", nil)
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, false)
			if err != nil {
				return err
			}

			remoteWorkflow, err := envCtx.Client.GetWorkflow(cmd.Context(), workflowID, false)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch workflow for issues", map[string]any{
					"workflowId": workflowID,
				})
			}
			result := workflowutil.Validate(*remoteWorkflow, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				RuntimeEngine:   "go",
			})
			if envCtx.Config.Validation.Engine == "" || envCtx.Config.Validation.Engine == "n8n-runtime" {
				runtimeFindings, err := workflowutil.ValidateWorkflowWithRuntime(*remoteWorkflow)
				if err != nil {
					result.Findings = append(result.Findings, workflowutil.Finding{
						Severity:    "warning",
						Code:        "runtime_validator_unavailable",
						Message:     fmt.Sprintf("n8n runtime validator was unavailable: %v", err),
						Source:      "go",
						Remediation: "Install Node.js or set validation.engine to go for structural-only validation.",
					})
				} else {
					result.Findings = append(result.Findings, runtimeFindings...)
				}
			}
			credentialResult, err := a.credentialPreflight(cmd.Context(), envCtx, *remoteWorkflow, project, credentialPreflight, credentialPreflightWarn)
			if err != nil {
				return err
			}
			result.Findings = append(result.Findings, credentialResult.Findings...)

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       "ok",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": remoteWorkflow.Name,
					"workflowId":   remoteWorkflow.ID.String(),
					"findings":     result.Findings,
				})
			}
			if len(result.Findings) == 0 {
				_, err = fmt.Fprintf(a.deps.Streams.Out, "No issues found for workflow %s (%s)\n", remoteWorkflow.Name, remoteWorkflow.ID.String())
				return err
			}
			for _, finding := range result.Findings {
				fmt.Fprintf(a.deps.Streams.Out, "%s [%s] %s", strings.ToUpper(finding.Severity), finding.Code, finding.Message)
				if finding.Path != "" {
					fmt.Fprintf(a.deps.Streams.Out, " (%s)", finding.Path)
				}
				if finding.Remediation != "" {
					fmt.Fprintf(a.deps.Streams.Out, "\n  fix: %s", finding.Remediation)
				}
				fmt.Fprintln(a.deps.Streams.Out)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "remote workflow id")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().StringVar(&credentialPreflight, "credential-preflight", "", "credential preflight mode: fail, warn, or skip")
	return cmd
}

func newWorkflowDiffCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var projectName string
	var allowActive bool

	cmd := &cobra.Command{
		Use:   "diff <file>",
		Short: "Diff a local workflow file against the remote workflow",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}

			filePath := a.resolvePath(args[0])
			localWorkflow, result, err := workflowutil.ValidateFile(filePath, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				AllowActive:     allowActive,
			})
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to validate workflow file", map[string]any{
					"path": filePath,
				})
			}
			a.renderWarnings(result.Warnings())
			if err := a.requireValidation(result); err != nil {
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

			remoteWorkflow, found, err := a.findWorkflowInProject(cmd.Context(), envCtx.Client, workflowID, localWorkflow.Name, projectID)
			if err != nil {
				return err
			}
			if !found {
				return clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowNotFound, fmt.Sprintf("remote workflow %q was not found", localWorkflow.Name), map[string]any{
					"workflowName": localWorkflow.Name,
				})
			}

			diffResult, err := workflowutil.Diff(*localWorkflow, *remoteWorkflow)
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to diff workflows", nil)
			}
			diffResult.WorkflowID = remoteWorkflow.ID.String()

			status := "equal"
			if !diffResult.Equal {
				status = "changed"
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       status,
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": localWorkflow.Name,
					"workflowId":   remoteWorkflow.ID.String(),
					"equal":        diffResult.Equal,
					"changes":      diffResult.Changes,
					"diff":         strings.TrimSpace(diffResult.Diff),
				})
			}

			if diffResult.Equal {
				_, err = fmt.Fprintf(a.deps.Streams.Out, "No differences found for workflow %s (%s)\n", localWorkflow.Name, remoteWorkflow.ID.String())
				return err
			}

			if _, err := fmt.Fprintf(a.deps.Streams.Out, "Workflow %s (%s) differs from local file:\n", localWorkflow.Name, remoteWorkflow.ID.String()); err != nil {
				return err
			}
			for _, change := range diffResult.Changes {
				name := change.Name
				if name == "" {
					name = change.Field
				}
				if name == "" {
					name = change.Category
				}
				fmt.Fprintf(a.deps.Streams.Out, "- %s %s: %s", change.Category, name, change.Change)
				if change.Field != "" && change.Field != name {
					fmt.Fprintf(a.deps.Streams.Out, " (%s)", change.Field)
				}
				fmt.Fprintln(a.deps.Streams.Out)
			}
			_, err = fmt.Fprintf(a.deps.Streams.Out, "\n%s\n", strings.TrimSpace(diffResult.Diff))
			return err
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "explicit remote workflow id to diff against")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "permit active=true in workflow JSON")
	return cmd
}

func newWorkflowDeployCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var projectName string
	var reason string
	var activate bool
	var allowActive bool
	var credentialPreflight string
	var backupFile string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "deploy <file>",
		Short: "Create or update a workflow in n8n Cloud",
		Long:  "Create or update a workflow in n8n Cloud. Production deploys fail closed unless --yes is provided after reviewing the target environment, project, workflow, and credential preflight.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}

			filePath := a.resolvePath(args[0])
			localWorkflow, result, err := workflowutil.ValidateFile(filePath, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				AllowActive:     allowActive,
				RuntimeEngine:   envCtx.Config.Validation.Engine,
			})
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to validate workflow file", map[string]any{
					"path": filePath,
				})
			}
			a.renderWarnings(result.Warnings())
			if err := a.requireValidation(result); err != nil {
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

			remoteWorkflow, found, err := a.findWorkflowInProject(cmd.Context(), envCtx.Client, workflowID, localWorkflow.Name, projectID)
			if err != nil {
				return err
			}
			if !found && project == nil {
				return clierrors.New(clierrors.ExitSafety, clierrors.CodeProjectNotFound, "workflow deploy create path requires --project or environment default_project", map[string]any{
					"environment":  envCtx.EnvironmentName,
					"workflowName": localWorkflow.Name,
				})
			}

			actions := make([]string, 0, 4)
			if found {
				actions = append(actions, "update")
				if project != nil && remoteWorkflow.ProjectID.String() != "" && remoteWorkflow.ProjectID.String() != project.ID {
					actions = append(actions, "move")
				}
				if remoteWorkflow.Active && !activate {
					actions = append(actions, "deactivate")
				}
			} else {
				actions = append(actions, "create")
			}
			if activate {
				actions = append(actions, "activate")
			}

			credentialResult, err := a.credentialPreflight(cmd.Context(), envCtx, *localWorkflow, project, credentialPreflight, credentialPreflightFail)
			if err != nil {
				return err
			}
			result.Findings = append(result.Findings, credentialResult.Findings...)
			a.renderWarnings(credentialResult.Findings)

			if err := a.requireProdConfirmation(envCtx, project, localWorkflow.Name, workflowID, "deploy", &credentialResult); err != nil {
				return err
			}

			if err := a.requireCredentialPreflight(credentialResult, localWorkflow.Name); err != nil {
				return err
			}

			if a.opts.DryRun {
				if a.opts.JSON {
					return a.printJSON(map[string]any{
						"status":       "dry-run",
						"environment":  envCtx.EnvironmentName,
						"project":      project,
						"workflowName": localWorkflow.Name,
						"workflowId":   workflowID,
						"actions":      actions,
						"warnings":     result.Warnings(),
						"credentials":  credentialResult,
					})
				}

				_, err = fmt.Fprintf(a.deps.Streams.Out, "Dry run for %s in %s", localWorkflow.Name, envCtx.EnvironmentName)
				if project != nil {
					fmt.Fprintf(a.deps.Streams.Out, " project %s (%s)", project.Name, project.ID)
				}
				fmt.Fprintf(a.deps.Streams.Out, ":\n- %s\ncredentials checked: %d\n", strings.Join(actions, "\n- "), credentialResult.Checked)
				return err
			}

			var backupPath string
			if found && envCtx.Config.Safety.BackupBeforeUpdate {
				backupPath, err = a.backupWorkflowWithOptions(envCtx, remoteWorkflow, reason, backupFile, backupDir)
				if err != nil {
					return err
				}
			}

			prepared := workflowutil.PrepareForWrite(*localWorkflow)
			if project != nil {
				prepared.ProjectID = n8n.ID(project.ID)
			}
			var deployed *n8n.Workflow
			status := "created"

			if found {
				status = "updated"
				if remoteWorkflow.Active && !activate {
					if _, err := envCtx.Client.DeactivateWorkflow(cmd.Context(), remoteWorkflow.ID.String()); err != nil {
						return a.mapAPIError(err, "failed to deactivate remote workflow before update", map[string]any{
							"workflowId": remoteWorkflow.ID.String(),
						})
					}
				}
				deployed, err = envCtx.Client.UpdateWorkflow(cmd.Context(), remoteWorkflow.ID.String(), prepared)
				if err != nil {
					return a.mapAPIError(err, "failed to update workflow", map[string]any{
						"workflowId":   remoteWorkflow.ID.String(),
						"workflowName": localWorkflow.Name,
					})
				}
				if project != nil && remoteWorkflow.ProjectID.String() != "" && remoteWorkflow.ProjectID.String() != project.ID {
					if err := envCtx.Client.TransferWorkflow(cmd.Context(), remoteWorkflow.ID.String(), project.ID, true); err != nil {
						return a.mapAPIError(err, "failed to move workflow to target project after deploy", map[string]any{
							"workflowId": remoteWorkflow.ID.String(),
							"project":    project,
						})
					}
				}
			} else {
				deployed, err = envCtx.Client.CreateWorkflow(cmd.Context(), prepared)
				if err != nil {
					return a.mapAPIError(err, "failed to create workflow", map[string]any{
						"workflowName": localWorkflow.Name,
					})
				}
			}

			if activate {
				deployed, err = envCtx.Client.ActivateWorkflow(cmd.Context(), deployed.ID.String())
				if err != nil {
					return a.mapAPIError(err, "failed to activate workflow after deploy", map[string]any{
						"workflowId": deployed.ID.String(),
					})
				}
			}
			projectVerification, verifiedWorkflow, err := a.verifyWorkflowProject(cmd.Context(), envCtx, deployed.ID.String(), project)
			if err != nil {
				return err
			}
			if verifiedWorkflow != nil {
				deployed = verifiedWorkflow
			}

			response := map[string]any{
				"status":       status,
				"workflowName": deployed.Name,
				"workflowId":   deployed.ID.String(),
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"active":       deployed.Active,
				"actions":      actions,
				"credentials":  credentialResult,
				"projectCheck": projectVerification,
			}
			if backupPath != "" {
				response["backupPath"] = backupPath
			}
			if len(result.Warnings()) > 0 {
				response["warnings"] = result.Warnings()
			}

			if a.opts.JSON {
				return a.printJSON(response)
			}

			if _, err := fmt.Fprintf(a.deps.Streams.Out, "%s workflow %s (%s) in %s", titleWord(status), deployed.Name, deployed.ID.String(), envCtx.EnvironmentName); err != nil {
				return err
			}
			if project != nil {
				fmt.Fprintf(a.deps.Streams.Out, " project %s (%s)", project.Name, project.ID)
			}
			fmt.Fprintf(a.deps.Streams.Out, "; active=%t; credentials checked=%d\n", deployed.Active, credentialResult.Checked)
			if backupPath != "" {
				_, err = fmt.Fprintf(a.deps.Streams.Out, "Backup: %s\n", backupPath)
				return err
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "explicit remote workflow id to update")
	cmd.Flags().StringVar(&projectName, "project", "", "target project name or id")
	cmd.Flags().StringVar(&reason, "reason", "", "backup/deploy reason label")
	cmd.Flags().StringVar(&credentialPreflight, "credential-preflight", "", "credential preflight mode: fail, warn, or skip")
	cmd.Flags().StringVar(&backupFile, "backup-file", "", "write the pre-update backup to this exact file")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "write generated pre-update backups under this directory")
	cmd.Flags().BoolVar(&activate, "activate", false, "activate the workflow after deploy")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "permit active=true in workflow JSON")
	return cmd
}

func newWorkflowCreateCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var activate bool
	var allowActive bool
	var credentialPreflight string

	cmd := &cobra.Command{
		Use:   "create <file>",
		Short: "Create a workflow in a target project",
		Long:  "Create a workflow in a target project. Production creates fail closed unless --yes is provided after reviewing the target environment, project, workflow, and credential preflight.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, true)
			if err != nil {
				return err
			}

			filePath := a.resolvePath(args[0])
			localWorkflow, result, err := workflowutil.ValidateFile(filePath, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				AllowActive:     allowActive,
				RuntimeEngine:   envCtx.Config.Validation.Engine,
			})
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to validate workflow file", map[string]any{"path": filePath})
			}
			a.renderWarnings(result.Warnings())
			if err := a.requireValidation(result); err != nil {
				return err
			}
			credentialResult, err := a.credentialPreflight(cmd.Context(), envCtx, *localWorkflow, project, credentialPreflight, credentialPreflightFail)
			if err != nil {
				return err
			}
			if err := a.requireCredentialPreflight(credentialResult, localWorkflow.Name); err != nil {
				return err
			}
			if err := a.requireProdConfirmation(envCtx, project, localWorkflow.Name, "", "create workflow", &credentialResult); err != nil {
				return err
			}

			prepared := workflowutil.PrepareForWrite(*localWorkflow)
			prepared.ProjectID = n8n.ID(project.ID)
			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":       "dry-run",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": prepared.Name,
					"actions":      []string{"create"},
					"credentials":  credentialResult,
				}, fmt.Sprintf("Dry run create workflow %s in project %s (%s); credentials checked=%d\n", prepared.Name, project.Name, project.ID, credentialResult.Checked))
			}

			created, err := envCtx.Client.CreateWorkflow(cmd.Context(), prepared)
			if err != nil {
				return a.mapAPIError(err, "failed to create workflow", map[string]any{"workflowName": prepared.Name, "project": project})
			}
			actions := []string{"create"}
			if activate {
				created, err = envCtx.Client.ActivateWorkflow(cmd.Context(), created.ID.String())
				if err != nil {
					return a.mapAPIError(err, "failed to activate workflow after create", map[string]any{"workflowId": created.ID.String()})
				}
				actions = append(actions, "activate")
			}
			projectVerification, verifiedWorkflow, err := a.verifyWorkflowProject(cmd.Context(), envCtx, created.ID.String(), project)
			if err != nil {
				return err
			}
			if verifiedWorkflow != nil {
				created = verifiedWorkflow
			}
			return a.printOrText(map[string]any{
				"status":       "created",
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"workflowName": created.Name,
				"workflowId":   created.ID.String(),
				"active":       created.Active,
				"actions":      actions,
				"credentials":  credentialResult,
				"projectCheck": projectVerification,
			}, fmt.Sprintf("Created workflow %s (%s) in %s project %s (%s); active=%t\n", created.Name, created.ID.String(), envCtx.EnvironmentName, project.Name, project.ID, created.Active))
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "target project name or id")
	cmd.Flags().StringVar(&credentialPreflight, "credential-preflight", "", "credential preflight mode: fail, warn, or skip")
	cmd.Flags().BoolVar(&activate, "activate", false, "activate the workflow after create")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "permit active=true in workflow JSON")
	return cmd
}

func newWorkflowMoveCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var shareCredentials bool
	var backupFile string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "move <name-or-id>",
		Short: "Move a workflow to another project",
		Long:  "Move a workflow to another project. Production moves fail closed unless --yes is provided after reviewing the target environment, project, and workflow.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, true)
			if err != nil {
				return err
			}
			workflow, err := a.resolveWorkflow(cmd.Context(), envCtx.Client, args[0])
			if err != nil {
				return err
			}
			if err := a.requireProdConfirmation(envCtx, project, workflow.Name, workflow.ID.String(), "move workflow", nil); err != nil {
				return err
			}
			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":       "dry-run",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": workflow.Name,
					"workflowId":   workflow.ID.String(),
					"actions":      []string{"move"},
				}, fmt.Sprintf("Dry run move workflow %s (%s) to project %s (%s)\n", workflow.Name, workflow.ID.String(), project.Name, project.ID))
			}
			var backupPath string
			if envCtx.Config.Safety.BackupBeforeUpdate {
				backupPath, err = a.backupWorkflowWithOptions(envCtx, workflow, "move", backupFile, backupDir)
				if err != nil {
					return err
				}
			}
			if err := envCtx.Client.TransferWorkflow(cmd.Context(), workflow.ID.String(), project.ID, shareCredentials); err != nil {
				return a.mapAPIError(err, "failed to move workflow", map[string]any{"workflowId": workflow.ID.String(), "project": project})
			}
			projectVerification, _, err := a.verifyWorkflowProject(cmd.Context(), envCtx, workflow.ID.String(), project)
			if err != nil {
				return err
			}
			response := map[string]any{
				"status":       "moved",
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"workflowName": workflow.Name,
				"workflowId":   workflow.ID.String(),
				"actions":      []string{"move"},
				"projectCheck": projectVerification,
			}
			if backupPath != "" {
				response["backupPath"] = backupPath
			}
			return a.printOrText(response, fmt.Sprintf("Moved workflow %s (%s) to project %s (%s)\n", workflow.Name, workflow.ID.String(), project.Name, project.ID))
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "target project name or id")
	cmd.Flags().StringVar(&backupFile, "backup-file", "", "write the pre-move backup to this exact file")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "write generated pre-move backups under this directory")
	cmd.Flags().BoolVar(&shareCredentials, "share-credentials", false, "share associated credentials with the destination project")
	return cmd
}

func newWorkflowCloneCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var newName string
	var activate bool

	cmd := &cobra.Command{
		Use:   "clone <name-or-id>",
		Short: "Clone a workflow into a target project",
		Long:  "Clone a workflow into a target project. Production clones fail closed unless --yes is provided after reviewing the target environment, project, and workflow.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, true)
			if err != nil {
				return err
			}
			source, err := a.resolveWorkflow(cmd.Context(), envCtx.Client, args[0])
			if err != nil {
				return err
			}
			name := strings.TrimSpace(newName)
			if name == "" {
				name = source.Name + " (Copy)"
			}
			if err := a.requireProdConfirmation(envCtx, project, name, source.ID.String(), "clone workflow", nil); err != nil {
				return err
			}

			prepared := workflowutil.PrepareForWrite(*source)
			prepared.Name = name
			prepared.ProjectID = n8n.ID(project.ID)
			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":       "dry-run",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": name,
					"sourceId":     source.ID.String(),
					"actions":      []string{"clone"},
				}, fmt.Sprintf("Dry run clone workflow %s (%s) to %s in project %s (%s)\n", source.Name, source.ID.String(), name, project.Name, project.ID))
			}
			created, err := envCtx.Client.CreateWorkflow(cmd.Context(), prepared)
			if err != nil {
				return a.mapAPIError(err, "failed to clone workflow", map[string]any{"sourceId": source.ID.String(), "project": project})
			}
			actions := []string{"clone"}
			if activate {
				created, err = envCtx.Client.ActivateWorkflow(cmd.Context(), created.ID.String())
				if err != nil {
					return a.mapAPIError(err, "failed to activate cloned workflow", map[string]any{"workflowId": created.ID.String()})
				}
				actions = append(actions, "activate")
			}
			return a.printOrText(map[string]any{
				"status":       "cloned",
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"workflowName": created.Name,
				"workflowId":   created.ID.String(),
				"sourceId":     source.ID.String(),
				"active":       created.Active,
				"actions":      actions,
			}, fmt.Sprintf("Cloned workflow %s (%s) to %s (%s) in project %s (%s); active=%t\n", source.Name, source.ID.String(), created.Name, created.ID.String(), project.Name, project.ID, created.Active))
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "target project name or id")
	cmd.Flags().StringVar(&newName, "name", "", "name for the cloned workflow")
	cmd.Flags().BoolVar(&activate, "activate", false, "activate the cloned workflow")
	return cmd
}

func newWorkflowRunCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var wait bool
	var timeout time.Duration
	var startNode string
	var destinationNode string
	var inputPath string
	var diagnoseOnFailure string

	cmd := &cobra.Command{
		Use:   "run <name-or-id>",
		Short: "Run a workflow when the target n8n public API supports it",
		Args:  cobra.ExactArgs(1),
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
			workflow, err := a.resolveWorkflowInProject(cmd.Context(), envCtx.Client, args[0], projectID)
			if err != nil {
				return err
			}

			request := api.RunWorkflowRequest{}
			if startNode != "" {
				request.StartNodes = []string{startNode}
			}
			request.DestinationNode = destinationNode
			if inputPath != "" {
				payload, err := os.ReadFile(a.resolvePath(inputPath))
				if err != nil {
					return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeUsageError, "failed to read workflow input file", map[string]any{"path": inputPath})
				}
				var input map[string]any
				if err := stdjson.Unmarshal(payload, &input); err != nil {
					return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeUsageError, "failed to parse workflow input JSON", map[string]any{"path": inputPath})
				}
				request.RunData = input
			}

			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":       "dry-run",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": workflow.Name,
					"workflowId":   workflow.ID.String(),
					"actions":      []string{"run"},
				}, fmt.Sprintf("Dry run workflow run %s (%s) in %s\n", workflow.Name, workflow.ID.String(), envCtx.EnvironmentName))
			}

			run, err := envCtx.Client.RunWorkflow(cmd.Context(), workflow.ID.String(), request)
			if err != nil {
				var apiErr *api.APIError
				if errors.As(err, &apiErr) && (apiErr.StatusCode == 404 || apiErr.StatusCode == 405 || apiErr.StatusCode == 501) {
					return clierrors.Wrap(err, clierrors.ExitAPI, clierrors.CodeUnsupportedEndpoint, "public workflow run endpoint is not available on this n8n instance; use execution retry or a webhook/manual trigger path", map[string]any{
						"workflowId":  workflow.ID.String(),
						"statusCode":  apiErr.StatusCode,
						"environment": envCtx.EnvironmentName,
					})
				}
				return a.mapAPIError(err, "public workflow run endpoint is not available on this n8n instance; use execution retry or a webhook/manual trigger path", map[string]any{
					"workflowId": workflow.ID.String(),
				})
			}
			executionID := run.ResolvedExecutionID().String()
			var execution *n8n.Execution
			if wait && executionID != "" {
				execution, err = a.waitExecution(cmd.Context(), envCtx, executionID, timeout, 2*time.Second)
				if err != nil {
					return err
				}
			}

			payload := map[string]any{
				"status":       "started",
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"workflowName": workflow.Name,
				"workflowId":   workflow.ID.String(),
				"executionId":  executionID,
				"run":          run,
			}
			text := fmt.Sprintf("Started workflow %s (%s)", workflow.Name, workflow.ID.String())
			if executionID != "" {
				text += fmt.Sprintf("; execution=%s", executionID)
			}
			if execution != nil {
				payload["execution"] = execution
				payload["status"] = execution.Status
				text += fmt.Sprintf("; status=%s", execution.Status)
				if shouldDiagnoseExecution(diagnoseOnFailure, *execution) {
					diagnosis, err := a.executionDiagnosis(cmd.Context(), envCtx, execution.ID.String(), 25)
					if err != nil {
						return err
					}
					payload["diagnosis"] = diagnosis
					text += "\n" + renderDiagnosisText(diagnosis)
				}
			}
			text += "\n"
			if err := a.printOrText(payload, text); err != nil {
				return err
			}
			if wait && execution != nil && a.opts.CI && executionFailed(*execution) {
				return clierrors.New(clierrors.ExitAPI, clierrors.CodeExecutionFailed, "workflow run finished unsuccessfully", map[string]any{"executionId": execution.ID.String(), "status": execution.Status})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().BoolVar(&wait, "wait", false, "wait for execution completion")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time to wait")
	cmd.Flags().StringVar(&diagnoseOnFailure, "diagnose-on-failure", "auto", "diagnose failed executions after --wait: auto, always, or never")
	cmd.Flags().StringVar(&startNode, "start-node", "", "node name to start from when supported")
	cmd.Flags().StringVar(&destinationNode, "destination-node", "", "destination node name when supported")
	cmd.Flags().StringVar(&inputPath, "input", "", "JSON input file for supported run endpoints")
	return cmd
}

func newWorkflowDependenciesCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var local bool
	var remote bool

	cmd := &cobra.Command{
		Use:   "dependencies [file] --local | --id <workflow-id> --remote",
		Short: "Report workflow dependencies discovered from nodes and parameters",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if local || len(args) == 1 {
				if remote || strings.TrimSpace(workflowID) != "" {
					return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "local dependency inspection cannot be combined with --remote or --id", nil)
				}
				if len(args) != 1 {
					return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow dependencies --local requires a workflow JSON file", nil)
				}
				workflowDoc, err := workflowutil.LoadFile(a.resolvePath(args[0]))
				if err != nil {
					return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to read workflow file", map[string]any{"path": args[0]})
				}
				dependencies := workflowutil.Dependencies(*workflowDoc)
				if a.opts.JSON {
					return a.printJSON(map[string]any{
						"status":       "ok",
						"mode":         "local",
						"workflowName": workflowDoc.Name,
						"dependencies": dependencies,
					})
				}
				rows := make([][]string, 0, len(dependencies))
				for _, dependency := range dependencies {
					rows = append(rows, []string{dependency.Type, dependency.NodeName, dependency.Name, dependency.ID, dependency.Detail})
				}
				return output.WriteTable(a.deps.Streams.Out, []string{"TYPE", "NODE", "NAME", "ID", "DETAIL"}, rows)
			}

			if !remote && strings.TrimSpace(workflowID) == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow dependencies requires a local file or --id <workflow-id>", nil)
			}
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			if strings.TrimSpace(workflowID) == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow dependencies requires --id", nil)
			}
			workflow, err := envCtx.Client.GetWorkflow(cmd.Context(), workflowID, false)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch workflow dependencies", map[string]any{"workflowId": workflowID})
			}
			dependencies := workflowutil.Dependencies(*workflow)
			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       "ok",
					"mode":         "remote",
					"environment":  envCtx.EnvironmentName,
					"workflowName": workflow.Name,
					"workflowId":   workflow.ID.String(),
					"dependencies": dependencies,
				})
			}
			rows := make([][]string, 0, len(dependencies))
			for _, dependency := range dependencies {
				rows = append(rows, []string{dependency.Type, dependency.NodeName, dependency.Name, dependency.ID, dependency.Detail})
			}
			return output.WriteTable(a.deps.Streams.Out, []string{"TYPE", "NODE", "NAME", "ID", "DETAIL"}, rows)
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "remote workflow id")
	cmd.Flags().BoolVar(&local, "local", false, "inspect a local workflow JSON file without API credentials")
	cmd.Flags().BoolVar(&remote, "remote", false, "inspect a remote workflow by id")
	return cmd
}

func newWorkflowDriftCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var projectName string
	var allowActive bool

	cmd := &cobra.Command{
		Use:   "drift <file>",
		Short: "Compare local workflow JSON with the remote workflow, including placement and activation drift",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			filePath := a.resolvePath(args[0])
			localWorkflow, result, err := workflowutil.ValidateFile(filePath, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				AllowActive:     allowActive,
				RuntimeEngine:   envCtx.Config.Validation.Engine,
			})
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitUsage, clierrors.CodeValidationFailed, "failed to validate workflow file", map[string]any{"path": filePath})
			}
			a.renderWarnings(result.Warnings())
			if err := a.requireValidation(result); err != nil {
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
			remoteWorkflow, found, err := a.findWorkflowInProject(cmd.Context(), envCtx.Client, workflowID, localWorkflow.Name, projectID)
			if err != nil {
				return err
			}
			if !found {
				return clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowNotFound, fmt.Sprintf("remote workflow %q was not found", localWorkflow.Name), map[string]any{"workflowName": localWorkflow.Name})
			}
			diffResult, err := workflowutil.Diff(*localWorkflow, *remoteWorkflow)
			if err != nil {
				return clierrors.Wrap(err, clierrors.ExitInternal, clierrors.CodeInternalFailure, "failed to compare workflow drift", nil)
			}
			diffResult.WorkflowID = remoteWorkflow.ID.String()
			extraChanges := make([]workflowutil.DiffChange, 0, 2)
			if localWorkflow.Active != remoteWorkflow.Active {
				extraChanges = append(extraChanges, workflowutil.DiffChange{Category: "activation", Field: "active", Change: fmt.Sprintf("%t -> %t", remoteWorkflow.Active, localWorkflow.Active)})
			}
			if project != nil && remoteWorkflow.ProjectID.String() != "" && remoteWorkflow.ProjectID.String() != project.ID {
				extraChanges = append(extraChanges, workflowutil.DiffChange{Category: "project", Field: "projectId", Change: fmt.Sprintf("%q -> %q", remoteWorkflow.ProjectID.String(), project.ID)})
			}
			changes := append(diffResult.Changes, extraChanges...)
			drifted := !diffResult.Equal || len(extraChanges) > 0
			status := "in-sync"
			if drifted {
				status = "drift"
			}

			payload := map[string]any{
				"status":       status,
				"environment":  envCtx.EnvironmentName,
				"project":      project,
				"workflowName": localWorkflow.Name,
				"workflowId":   remoteWorkflow.ID.String(),
				"equal":        !drifted,
				"changes":      changes,
				"diff":         strings.TrimSpace(diffResult.Diff),
			}
			if a.opts.JSON {
				if err := a.printJSON(payload); err != nil {
					return err
				}
			} else if !drifted {
				if _, err := fmt.Fprintf(a.deps.Streams.Out, "No drift found for workflow %s (%s)\n", localWorkflow.Name, remoteWorkflow.ID.String()); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(a.deps.Streams.Out, "Workflow %s (%s) has drift:\n", localWorkflow.Name, remoteWorkflow.ID.String())
				for _, change := range changes {
					name := change.Name
					if name == "" {
						name = change.Field
					}
					if name == "" {
						name = change.Category
					}
					fmt.Fprintf(a.deps.Streams.Out, "- %s %s: %s\n", change.Category, name, change.Change)
				}
			}
			if drifted && a.opts.CI {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeDriftFound, "workflow drift found", map[string]any{
					"workflowName": localWorkflow.Name,
					"workflowId":   remoteWorkflow.ID.String(),
					"changes":      changes,
				})
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "explicit remote workflow id to compare")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().BoolVar(&allowActive, "allow-active", false, "permit active=true in workflow JSON")
	return cmd
}

func newWorkflowCleanupCmd(a *app) *cobra.Command {
	var envName string
	var projectName string
	var workflowID string
	var prefix string
	var backupFile string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "cleanup --project <project> (--id <workflow-id> | --prefix <name-prefix>)",
		Short: "Delete workflows safely within an explicit project scope",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, true)
			if err != nil {
				return err
			}
			workflowID = strings.TrimSpace(workflowID)
			prefix = strings.TrimSpace(prefix)
			if (workflowID == "") == (prefix == "") {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow cleanup requires exactly one of --id or --prefix", nil)
			}
			if strings.TrimSpace(backupFile) != "" && prefix != "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "--backup-file can only be used when cleanup targets one workflow by --id", nil)
			}

			targets := make([]n8n.Workflow, 0)
			if workflowID != "" {
				workflow, err := a.resolveWorkflowInProject(cmd.Context(), envCtx.Client, workflowID, project.ID)
				if err != nil {
					return err
				}
				targets = append(targets, *workflow)
			} else {
				workflows, err := envCtx.Client.ListWorkflows(cmd.Context(), api.ListWorkflowsParams{ProjectID: project.ID, Limit: 0})
				if err != nil {
					return a.mapAPIError(err, "failed to list workflows for cleanup", map[string]any{"project": project})
				}
				for _, workflow := range workflows {
					if strings.HasPrefix(workflow.Name, prefix) {
						targets = append(targets, workflow)
					}
				}
			}

			actionLabel := "cleanup workflow"
			if prefix != "" {
				actionLabel = "cleanup workflows"
			}
			label := workflowID
			if prefix != "" {
				label = prefix
			}
			if err := a.requireProdConfirmation(envCtx, project, label, workflowID, actionLabel, nil); err != nil {
				return err
			}

			type cleanupResult struct {
				WorkflowID   string `json:"workflowId"`
				WorkflowName string `json:"workflowName"`
				BackupPath   string `json:"backupPath,omitempty"`
				Status       string `json:"status"`
			}
			results := make([]cleanupResult, 0, len(targets))
			if a.opts.DryRun {
				for _, workflow := range targets {
					results = append(results, cleanupResult{WorkflowID: workflow.ID.String(), WorkflowName: workflow.Name, Status: "would-delete"})
				}
				return a.printOrText(map[string]any{
					"status":      "dry-run",
					"environment": envCtx.EnvironmentName,
					"project":     project,
					"prefix":      prefix,
					"targets":     results,
				}, fmt.Sprintf("Dry run cleanup in project %s (%s); workflows matched=%d\n", project.Name, project.ID, len(results)))
			}

			for _, workflow := range targets {
				result := cleanupResult{WorkflowID: workflow.ID.String(), WorkflowName: workflow.Name, Status: "deleted"}
				if envCtx.Config.Safety.BackupBeforeUpdate {
					path, err := a.backupWorkflowWithOptions(envCtx, &workflow, "cleanup", backupFile, backupDir)
					if err != nil {
						return err
					}
					result.BackupPath = path
				}
				if err := envCtx.Client.DeleteWorkflow(cmd.Context(), workflow.ID.String()); err != nil {
					return a.mapAPIError(err, "failed to delete workflow during cleanup", map[string]any{"workflowId": workflow.ID.String(), "project": project})
				}
				results = append(results, result)
			}

			return a.printOrText(map[string]any{
				"status":      "deleted",
				"environment": envCtx.EnvironmentName,
				"project":     project,
				"prefix":      prefix,
				"deleted":     results,
			}, fmt.Sprintf("Deleted %d workflow(s) from project %s (%s)\n", len(results), project.Name, project.ID))
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&projectName, "project", "", "target project name or id")
	cmd.Flags().StringVar(&workflowID, "id", "", "workflow id to delete")
	cmd.Flags().StringVar(&prefix, "prefix", "", "delete workflows whose names start with this prefix")
	cmd.Flags().StringVar(&backupFile, "backup-file", "", "write the pre-delete backup to this exact file; only valid with one target")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "write generated pre-delete backups under this directory")
	return cmd
}

func newWorkflowDoctorCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var projectName string
	var credentialPreflight string

	cmd := &cobra.Command{
		Use:   "doctor --id <workflow-id>",
		Short: "Run workflow issues, credential preflight, and dependency checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			if strings.TrimSpace(workflowID) == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow doctor requires --id", nil)
			}
			project, err := a.resolveProject(cmd.Context(), envCtx, projectName, false)
			if err != nil {
				return err
			}
			workflow, err := envCtx.Client.GetWorkflow(cmd.Context(), workflowID, false)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch workflow for doctor", map[string]any{"workflowId": workflowID})
			}
			validation := workflowutil.Validate(*workflow, workflowutil.ValidationOptions{
				EnvironmentName: envCtx.EnvironmentName,
				ProductionHosts: envCtx.Config.ProductionHosts(envCtx.EnvironmentName),
				RuntimeEngine:   "go",
			})
			if envCtx.Config.Validation.Engine == "" || envCtx.Config.Validation.Engine == "n8n-runtime" {
				runtimeFindings, err := workflowutil.ValidateWorkflowWithRuntime(*workflow)
				if err != nil {
					validation.Findings = append(validation.Findings, workflowutil.Finding{
						Severity:    "warning",
						Code:        "runtime_validator_unavailable",
						Message:     fmt.Sprintf("n8n runtime validator was unavailable: %v", err),
						Source:      "go",
						Remediation: "Install Node.js or set validation.engine to go for structural-only validation.",
					})
				} else {
					validation.Findings = append(validation.Findings, runtimeFindings...)
				}
			}
			credentialResult, err := a.credentialPreflight(cmd.Context(), envCtx, *workflow, project, credentialPreflight, credentialPreflightWarn)
			if err != nil {
				return err
			}
			findings := append(validation.Findings, credentialResult.Findings...)
			dependencies := workflowutil.Dependencies(*workflow)

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       "ok",
					"environment":  envCtx.EnvironmentName,
					"project":      project,
					"workflowName": workflow.Name,
					"workflowId":   workflow.ID.String(),
					"findings":     findings,
					"dependencies": dependencies,
					"credentials":  credentialResult,
				})
			}
			for _, finding := range findings {
				fmt.Fprintf(a.deps.Streams.Out, "%s [%s] %s", strings.ToUpper(finding.Severity), finding.Code, finding.Message)
				if finding.NodeName != "" {
					fmt.Fprintf(a.deps.Streams.Out, " (node %s)", finding.NodeName)
				}
				if finding.Remediation != "" {
					fmt.Fprintf(a.deps.Streams.Out, "\n  fix: %s", finding.Remediation)
				}
				fmt.Fprintln(a.deps.Streams.Out)
			}
			fmt.Fprintf(a.deps.Streams.Out, "dependencies: %d; credentials checked: %d\n", len(dependencies), credentialResult.Checked)
			return nil
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "remote workflow id")
	cmd.Flags().StringVar(&projectName, "project", "", "project name or id")
	cmd.Flags().StringVar(&credentialPreflight, "credential-preflight", "", "credential preflight mode: fail, warn, or skip")
	return cmd
}

func newWorkflowRebindCredentialCmd(a *app) *cobra.Command {
	var envName string
	var workflowID string
	var nodeName string
	var credentialIdentifier string
	var allGoogleDrive string
	var credentialPreflight string
	var backupFile string
	var backupDir string

	cmd := &cobra.Command{
		Use:   "rebind-credential --id <workflow-id>",
		Short: "Rebind workflow node credential references",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}
			if workflowID == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "workflow rebind-credential requires --id", nil)
			}
			if credentialIdentifier == "" && allGoogleDrive == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "pass --credential or --all-google-drive", nil)
			}
			if credentialIdentifier != "" && nodeName == "" && allGoogleDrive == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeUsageError, "--credential requires --node unless --all-google-drive is used", nil)
			}
			workflow, err := envCtx.Client.GetWorkflow(cmd.Context(), workflowID, false)
			if err != nil {
				return a.mapAPIError(err, "failed to fetch workflow for credential rebind", map[string]any{"workflowId": workflowID})
			}
			credentialNameOrID := credentialIdentifier
			if allGoogleDrive != "" {
				credentialNameOrID = allGoogleDrive
			}
			credentials, err := envCtx.Client.ListCredentials(cmd.Context(), api.ListCredentialsParams{})
			if err != nil {
				return a.mapAPIError(err, "failed to list credentials for rebind", map[string]any{"workflowId": workflowID})
			}
			credential, err := a.resolveCredential(credentialNameOrID, credentials)
			if err != nil {
				return err
			}

			credentialType := credential.Type
			if allGoogleDrive != "" && credentialType == "" {
				credentialType = "googleDriveOAuth2Api"
			}
			if credentialType == "" {
				return clierrors.New(clierrors.ExitUsage, clierrors.CodeValidationFailed, "target credential type is unknown; rebind by a credential with a type", map[string]any{
					"credential": credentialNameOrID,
				})
			}

			updated := workflowutil.PrepareForWrite(*workflow)
			changedNodes := make([]string, 0)
			for i := range updated.Nodes {
				node := &updated.Nodes[i]
				if allGoogleDrive != "" {
					if !isGoogleDriveNode(*node) {
						continue
					}
				} else if node.Name != nodeName {
					continue
				}
				if node.Credentials == nil {
					node.Credentials = map[string]n8n.CredentialReference{}
				}
				key := credentialType
				if allGoogleDrive != "" {
					key = "googleDriveOAuth2Api"
				}
				node.Credentials[key] = n8n.CredentialReference{
					ID:   credential.ID,
					Name: credential.Name,
				}
				changedNodes = append(changedNodes, node.Name)
			}
			if len(changedNodes) == 0 {
				return clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowNotFound, "no matching workflow nodes found for credential rebind", map[string]any{
					"workflowId": workflowID,
					"node":       nodeName,
				})
			}
			credentialResult, err := a.credentialPreflight(cmd.Context(), envCtx, updated, nil, credentialPreflight, credentialPreflightFail)
			if err != nil {
				return err
			}
			if err := a.requireCredentialPreflight(credentialResult, updated.Name); err != nil {
				return err
			}
			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":      "dry-run",
					"environment": envCtx.EnvironmentName,
					"workflowId":  workflow.ID.String(),
					"credential":  credential,
					"credentials": credentialResult,
					"nodes":       changedNodes,
					"actions":     []string{"rebind-credential"},
				}, fmt.Sprintf("Dry run rebind credential %s (%s) on %d node(s)\n", credential.Name, credential.ID.String(), len(changedNodes)))
			}
			var backupPath string
			if envCtx.Config.Safety.BackupBeforeUpdate {
				backupPath, err = a.backupWorkflowWithOptions(envCtx, workflow, "rebind-credential", backupFile, backupDir)
				if err != nil {
					return err
				}
			}
			deployed, err := envCtx.Client.UpdateWorkflow(cmd.Context(), workflow.ID.String(), updated)
			if err != nil {
				return a.mapAPIError(err, "failed to update workflow credential binding", map[string]any{"workflowId": workflow.ID.String()})
			}
			response := map[string]any{
				"status":       "updated",
				"environment":  envCtx.EnvironmentName,
				"workflowName": deployed.Name,
				"workflowId":   deployed.ID.String(),
				"credential":   credential,
				"credentials":  credentialResult,
				"nodes":        changedNodes,
				"actions":      []string{"rebind-credential"},
			}
			if backupPath != "" {
				response["backupPath"] = backupPath
			}
			return a.printOrText(response, fmt.Sprintf("Updated credential binding for %d node(s) in workflow %s (%s)\n", len(changedNodes), deployed.Name, deployed.ID.String()))
		},
	}
	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().StringVar(&workflowID, "id", "", "remote workflow id")
	cmd.Flags().StringVar(&nodeName, "node", "", "node name to rebind")
	cmd.Flags().StringVar(&credentialIdentifier, "credential", "", "credential name or id")
	cmd.Flags().StringVar(&allGoogleDrive, "all-google-drive", "", "rebind all Google Drive nodes to this credential name or id")
	cmd.Flags().StringVar(&credentialPreflight, "credential-preflight", "", "credential preflight mode: fail, warn, or skip")
	cmd.Flags().StringVar(&backupFile, "backup-file", "", "write the pre-update backup to this exact file")
	cmd.Flags().StringVar(&backupDir, "backup-dir", "", "write generated pre-update backups under this directory")
	return cmd
}

func (a *app) resolveCredential(identifier string, credentials []n8n.Credential) (*n8n.Credential, error) {
	identifier = strings.TrimSpace(identifier)
	for _, credential := range credentials {
		if credential.ID.String() == identifier {
			return &credential, nil
		}
	}
	matches := make([]n8n.Credential, 0)
	for _, credential := range credentials {
		if credential.Name == identifier {
			matches = append(matches, credential)
		}
	}
	switch len(matches) {
	case 0:
		return nil, clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowNotFound, fmt.Sprintf("credential %q was not found", identifier), map[string]any{"credential": identifier})
	case 1:
		return &matches[0], nil
	default:
		return nil, clierrors.New(clierrors.ExitResolution, clierrors.CodeWorkflowAmbiguous, fmt.Sprintf("credential name %q matched multiple credentials", identifier), map[string]any{"credential": identifier, "matchCount": len(matches)})
	}
}

func isGoogleDriveNode(node n8n.Node) bool {
	lowerType := strings.ToLower(node.Type)
	if strings.Contains(lowerType, "googledrive") {
		return true
	}
	for key := range node.Credentials {
		if strings.Contains(strings.ToLower(key), "googledrive") {
			return true
		}
	}
	return false
}

func newWorkflowActivateCmd(a *app) *cobra.Command {
	return newWorkflowToggleCmd(a, "activate")
}

func newWorkflowDeactivateCmd(a *app) *cobra.Command {
	return newWorkflowToggleCmd(a, "deactivate")
}

func newWorkflowToggleCmd(a *app, action string) *cobra.Command {
	var envName string

	cmd := &cobra.Command{
		Use:   fmt.Sprintf("%s <name-or-id>", action),
		Short: fmt.Sprintf("%s a workflow", action),
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}

			workflow, err := a.resolveWorkflow(cmd.Context(), envCtx.Client, args[0])
			if err != nil {
				return err
			}

			if a.opts.DryRun {
				return a.printOrText(map[string]any{
					"status":       "dry-run",
					"environment":  envCtx.EnvironmentName,
					"workflowId":   workflow.ID.String(),
					"workflowName": workflow.Name,
					"active":       workflow.Active,
					"actions":      []string{action},
				}, fmt.Sprintf("Dry run %s workflow %s (%s); active=%t\n", action, workflow.Name, workflow.ID.String(), workflow.Active))
			}

			var updated *n8n.Workflow
			status := "deactivated"
			switch action {
			case "activate":
				status = "activated"
				updated, err = envCtx.Client.ActivateWorkflow(cmd.Context(), workflow.ID.String())
			case "deactivate":
				updated, err = envCtx.Client.DeactivateWorkflow(cmd.Context(), workflow.ID.String())
			}
			if err != nil {
				return a.mapAPIError(err, fmt.Sprintf("failed to %s workflow", action), map[string]any{
					"workflowId": workflow.ID.String(),
				})
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":       status,
					"environment":  envCtx.EnvironmentName,
					"workflowId":   updated.ID.String(),
					"workflowName": updated.Name,
					"active":       updated.Active,
				})
			}

			_, err = fmt.Fprintf(a.deps.Streams.Out, "%s workflow %s (%s); active=%t\n", titleWord(status), updated.Name, updated.ID.String(), updated.Active)
			return err
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	return cmd
}

func titleWord(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value[:1]) + value[1:]
}
