package mcpserver

import (
	"context"
)

func (s *Server) handleVersion(ctx context.Context, args EmptyArgs) ToolResult {
	return s.invoke(ctx, invocation{ToolName: "version", CLIArgs: []string{"version"}})
}

func (s *Server) handleEnvList(ctx context.Context, args EmptyArgs) ToolResult {
	return s.invoke(ctx, invocation{ToolName: "env_list", CLIArgs: []string{"env", "list"}})
}

func (s *Server) handleEnvDoctor(ctx context.Context, args EnvDoctorArgs) ToolResult {
	cliArgs := []string{"env", "doctor"}
	addFlag(&cliArgs, "--env", args.Env)
	addBoolFlag(&cliArgs, "--all", args.All)
	return s.invoke(ctx, invocation{
		ToolName: "env_doctor",
		CLIArgs:  cliArgs,
		Audit:    map[string]any{"requestedEnv": args.Env, "all": args.All},
	})
}

func (s *Server) handleProjectList(ctx context.Context, args ProjectListArgs) ToolResult {
	if err := requireValue("env", args.Env); err != nil {
		return s.securityResult(invocation{ToolName: "project_list", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"project", "list", "--env", args.Env}
	addIntFlag(&cliArgs, "--limit", args.Limit)
	addBoolFlag(&cliArgs, "--skip-workflow-counts", args.SkipWorkflowCounts)
	return s.invoke(ctx, invocation{
		ToolName: "project_list",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"limit": args.Limit, "skipWorkflowCounts": args.SkipWorkflowCounts},
	})
}

func (s *Server) handleWorkflowList(ctx context.Context, args WorkflowListArgs) ToolResult {
	if err := requireValue("env", args.Env); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_list", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "list", "--env", args.Env}
	addFlag(&cliArgs, "--project", args.Project)
	addIntFlag(&cliArgs, "--limit", args.Limit)
	return s.invoke(ctx, invocation{
		ToolName: "workflow_list",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"project": args.Project},
	})
}

func (s *Server) handleWorkflowGet(ctx context.Context, args WorkflowGetArgs) ToolResult {
	if err := requireValue("identifier", args.Identifier); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_get", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "get", args.Identifier, "--env", args.Env}
	addFlag(&cliArgs, "--project", args.Project)
	return s.invoke(ctx, invocation{
		ToolName: "workflow_get",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"workflow": args.Identifier, "project": args.Project},
	})
}

func (s *Server) handleWorkflowValidate(ctx context.Context, args WorkflowValidateArgs) ToolResult {
	cliArgs := []string{"workflow", "validate"}
	if err := s.addWorkspaceInput(&cliArgs, args.File); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_validate"}, err.Error())
	}
	addFlag(&cliArgs, "--env", args.Env)
	addFlag(&cliArgs, "--project", args.Project)
	addBoolFlag(&cliArgs, "--allow-active", args.AllowActive)
	return s.invoke(ctx, invocation{
		ToolName: "workflow_validate",
		CLIArgs:  cliArgs,
		Audit:    map[string]any{"file": args.File, "env": args.Env, "project": args.Project},
	})
}

func (s *Server) handleWorkflowDiff(ctx context.Context, args WorkflowFileCompareArgs) ToolResult {
	return s.handleWorkflowFileCompare(ctx, "workflow_diff", "diff", args)
}

func (s *Server) handleWorkflowDrift(ctx context.Context, args WorkflowFileCompareArgs) ToolResult {
	return s.handleWorkflowFileCompare(ctx, "workflow_drift", "drift", args)
}

func (s *Server) handleWorkflowFileCompare(ctx context.Context, toolName string, command string, args WorkflowFileCompareArgs) ToolResult {
	cliArgs := []string{"workflow", command}
	if err := s.addWorkspaceInput(&cliArgs, args.File); err != nil {
		return s.securityResult(invocation{ToolName: toolName, Env: args.Env, Remote: true}, err.Error())
	}
	addFlag(&cliArgs, "--env", args.Env)
	addFlag(&cliArgs, "--id", args.ID)
	addFlag(&cliArgs, "--project", args.Project)
	addBoolFlag(&cliArgs, "--allow-active", args.AllowActive)
	return s.invoke(ctx, invocation{
		ToolName: toolName,
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"file": args.File, "workflowId": args.ID, "project": args.Project},
	})
}

func (s *Server) handleWorkflowIssues(ctx context.Context, args WorkflowIssuesArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_issues", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "issues", "--env", args.Env, "--id", args.ID}
	addFlag(&cliArgs, "--project", args.Project)
	addFlag(&cliArgs, "--credential-preflight", args.CredentialPreflight)
	return s.invoke(ctx, invocation{
		ToolName: "workflow_issues",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"workflowId": args.ID, "project": args.Project},
	})
}

func (s *Server) handleWorkflowDependencies(ctx context.Context, args WorkflowDependenciesArgs) ToolResult {
	cliArgs := []string{"workflow", "dependencies"}
	remote := false
	if args.File != "" && args.ID != "" {
		return s.securityResult(invocation{ToolName: "workflow_dependencies"}, "pass either file or id, not both")
	}
	if args.File != "" {
		if err := s.addWorkspaceInput(&cliArgs, args.File); err != nil {
			return s.securityResult(invocation{ToolName: "workflow_dependencies"}, err.Error())
		}
		cliArgs = append(cliArgs, "--local")
	} else {
		if err := requireValue("id", args.ID); err != nil {
			return s.securityResult(invocation{ToolName: "workflow_dependencies", Env: args.Env, Remote: true}, err.Error())
		}
		cliArgs = append(cliArgs, "--remote", "--id", args.ID, "--env", args.Env)
		remote = true
	}
	return s.invoke(ctx, invocation{
		ToolName: "workflow_dependencies",
		CLIArgs:  cliArgs,
		Remote:   remote,
		Env:      args.Env,
		Audit:    map[string]any{"file": args.File, "workflowId": args.ID},
	})
}

func (s *Server) handleWorkflowDoctor(ctx context.Context, args WorkflowIssuesArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_doctor", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "doctor", "--env", args.Env, "--id", args.ID}
	addFlag(&cliArgs, "--project", args.Project)
	addFlag(&cliArgs, "--credential-preflight", args.CredentialPreflight)
	return s.invoke(ctx, invocation{
		ToolName: "workflow_doctor",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"workflowId": args.ID, "project": args.Project},
	})
}

func (s *Server) handleWorkflowDeploy(ctx context.Context, args WorkflowDeployArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	cliArgs := []string{"workflow", "deploy"}
	if err := s.addWorkspaceInput(&cliArgs, args.File); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_deploy", Env: args.Env, Remote: true}, err.Error())
	}
	addFlag(&cliArgs, "--env", args.Env)
	addFlag(&cliArgs, "--id", args.ID)
	addFlag(&cliArgs, "--project", args.Project)
	addFlag(&cliArgs, "--reason", args.Reason)
	addBoolFlag(&cliArgs, "--activate", args.Activate)
	addBoolFlag(&cliArgs, "--allow-active", args.AllowActive)
	if err := addCredentialPreflight(&cliArgs, args.CredentialPreflight, dryRun); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_deploy", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-file", args.BackupFile, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_deploy", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-dir", args.BackupDir, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_deploy", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_deploy", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"file": args.File, "workflowId": args.ID, "project": args.Project},
		map[string]any{
			"env":                  args.Env,
			"file":                 args.File,
			"id":                   args.ID,
			"project":              args.Project,
			"reason":               args.Reason,
			"activate":             args.Activate,
			"allow_active":         args.AllowActive,
			"credential_preflight": args.CredentialPreflight,
			"backup_file":          args.BackupFile,
			"backup_dir":           args.BackupDir,
		})
}

func (s *Server) handleWorkflowCreate(ctx context.Context, args WorkflowCreateArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	cliArgs := []string{"workflow", "create"}
	if err := s.addWorkspaceInput(&cliArgs, args.File); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_create", Env: args.Env, Remote: true}, err.Error())
	}
	addFlag(&cliArgs, "--env", args.Env)
	addFlag(&cliArgs, "--project", args.Project)
	addBoolFlag(&cliArgs, "--activate", args.Activate)
	addBoolFlag(&cliArgs, "--allow-active", args.AllowActive)
	if err := addCredentialPreflight(&cliArgs, args.CredentialPreflight, dryRun); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_create", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_create", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"file": args.File, "project": args.Project},
		map[string]any{
			"env":                  args.Env,
			"file":                 args.File,
			"project":              args.Project,
			"activate":             args.Activate,
			"allow_active":         args.AllowActive,
			"credential_preflight": args.CredentialPreflight,
		})
}

func (s *Server) handleWorkflowMove(ctx context.Context, args WorkflowMoveArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("identifier", args.Identifier); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_move", Env: args.Env, Remote: true}, err.Error())
	}
	if err := requireValue("project", args.Project); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_move", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "move", args.Identifier, "--env", args.Env, "--project", args.Project}
	addBoolFlag(&cliArgs, "--share-credentials", args.ShareCredentials)
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-file", args.BackupFile, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_move", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-dir", args.BackupDir, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_move", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_move", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflow": args.Identifier, "project": args.Project},
		map[string]any{
			"env":               args.Env,
			"identifier":        args.Identifier,
			"project":           args.Project,
			"share_credentials": args.ShareCredentials,
			"backup_file":       args.BackupFile,
			"backup_dir":        args.BackupDir,
		})
}

func (s *Server) handleWorkflowClone(ctx context.Context, args WorkflowCloneArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("identifier", args.Identifier); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_clone", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "clone", args.Identifier, "--env", args.Env}
	addFlag(&cliArgs, "--project", args.Project)
	addFlag(&cliArgs, "--name", args.Name)
	addBoolFlag(&cliArgs, "--activate", args.Activate)
	return s.invokeMutation(ctx, "workflow_clone", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflow": args.Identifier, "project": args.Project, "name": args.Name},
		map[string]any{
			"env":        args.Env,
			"identifier": args.Identifier,
			"project":    args.Project,
			"name":       args.Name,
			"activate":   args.Activate,
		})
}

func (s *Server) handleWorkflowRun(ctx context.Context, args WorkflowRunArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("identifier", args.Identifier); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_run", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", "run", args.Identifier, "--env", args.Env}
	addFlag(&cliArgs, "--project", args.Project)
	addBoolFlag(&cliArgs, "--wait", args.Wait)
	addDurationFlag(&cliArgs, "--timeout", args.TimeoutSeconds)
	addFlag(&cliArgs, "--diagnose-on-failure", args.DiagnoseOnFailure)
	addFlag(&cliArgs, "--start-node", args.StartNode)
	addFlag(&cliArgs, "--destination-node", args.DestinationNode)
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--input", args.Input, true); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_run", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_run", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflow": args.Identifier, "project": args.Project},
		map[string]any{
			"env":                 args.Env,
			"identifier":          args.Identifier,
			"project":             args.Project,
			"wait":                args.Wait,
			"timeout_seconds":     args.TimeoutSeconds,
			"diagnose_on_failure": args.DiagnoseOnFailure,
			"start_node":          args.StartNode,
			"destination_node":    args.DestinationNode,
			"input":               args.Input,
		})
}

func (s *Server) handleWorkflowActivate(ctx context.Context, args WorkflowToggleArgs) ToolResult {
	return s.handleWorkflowToggle(ctx, "workflow_activate", "activate", args)
}

func (s *Server) handleWorkflowDeactivate(ctx context.Context, args WorkflowToggleArgs) ToolResult {
	return s.handleWorkflowToggle(ctx, "workflow_deactivate", "deactivate", args)
}

func (s *Server) handleWorkflowToggle(ctx context.Context, toolName string, command string, args WorkflowToggleArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("identifier", args.Identifier); err != nil {
		return s.securityResult(invocation{ToolName: toolName, Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"workflow", command, args.Identifier, "--env", args.Env}
	return s.invokeMutation(ctx, toolName, args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflow": args.Identifier},
		map[string]any{
			"env":        args.Env,
			"identifier": args.Identifier,
		})
}

func (s *Server) handleWorkflowCleanup(ctx context.Context, args WorkflowCleanupArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("project", args.Project); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_cleanup", Env: args.Env, Remote: true}, err.Error())
	}
	if args.ID == "" && args.Prefix == "" {
		return s.securityResult(invocation{ToolName: "workflow_cleanup", Env: args.Env, Remote: true}, "id or prefix is required")
	}
	if !dryRun && args.Prefix != "" {
		return s.securityResult(invocation{ToolName: "workflow_cleanup", Env: args.Env, Remote: true}, "non-dry-run cleanup by prefix is blocked in MCP; pass an explicit id")
	}
	cliArgs := []string{"workflow", "cleanup", "--env", args.Env, "--project", args.Project}
	addFlag(&cliArgs, "--id", args.ID)
	addFlag(&cliArgs, "--prefix", args.Prefix)
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-file", args.BackupFile, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_cleanup", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-dir", args.BackupDir, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_cleanup", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_cleanup", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflowId": args.ID, "prefix": args.Prefix, "project": args.Project},
		map[string]any{
			"env":         args.Env,
			"project":     args.Project,
			"id":          args.ID,
			"prefix":      args.Prefix,
			"backup_file": args.BackupFile,
			"backup_dir":  args.BackupDir,
		})
}

func (s *Server) handleWorkflowRebindCredential(ctx context.Context, args WorkflowRebindCredentialArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, err.Error())
	}
	if args.Credential == "" && args.AllGoogleDrive == "" {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, "credential or all_google_drive is required")
	}
	if args.Credential != "" && args.Node == "" && args.AllGoogleDrive == "" {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, "node is required when credential is set without all_google_drive")
	}
	cliArgs := []string{"workflow", "rebind-credential", "--env", args.Env, "--id", args.ID}
	addFlag(&cliArgs, "--node", args.Node)
	addFlag(&cliArgs, "--credential", args.Credential)
	addFlag(&cliArgs, "--all-google-drive", args.AllGoogleDrive)
	if err := addCredentialPreflight(&cliArgs, args.CredentialPreflight, dryRun); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-file", args.BackupFile, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, err.Error())
	}
	if err := s.addOptionalWorkspaceFlag(&cliArgs, "--backup-dir", args.BackupDir, false); err != nil {
		return s.securityResult(invocation{ToolName: "workflow_rebind_credential", Env: args.Env, Remote: true}, err.Error())
	}
	return s.invokeMutation(ctx, "workflow_rebind_credential", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"workflowId": args.ID, "node": args.Node},
		map[string]any{
			"env":                  args.Env,
			"id":                   args.ID,
			"node":                 args.Node,
			"credential":           args.Credential,
			"all_google_drive":     args.AllGoogleDrive,
			"credential_preflight": args.CredentialPreflight,
			"backup_file":          args.BackupFile,
			"backup_dir":           args.BackupDir,
		})
}

func (s *Server) invokeMutation(ctx context.Context, toolName string, env string, dryRun bool, confirm bool, phrase string, cliArgs []string, audit map[string]any, nextCallArguments map[string]any) ToolResult {
	return s.invoke(ctx, invocation{
		ToolName:           toolName,
		CLIArgs:            cliArgs,
		NextCallArguments:  compactCallArguments(nextCallArguments),
		Remote:             true,
		Env:                env,
		Mutating:           true,
		DryRun:             dryRun,
		ConfirmMutation:    confirm,
		ConfirmationPhrase: phrase,
		Audit:              audit,
	})
}

func (s *Server) handleExecutionList(ctx context.Context, args ExecutionListArgs) ToolResult {
	cliArgs := []string{"execution", "list", "--env", args.Env}
	addFlag(&cliArgs, "--project", args.Project)
	addFlag(&cliArgs, "--workflow", args.Workflow)
	addFlag(&cliArgs, "--status", args.Status)
	addIntFlag(&cliArgs, "--limit", args.Limit)
	return s.invoke(ctx, invocation{
		ToolName: "execution_list",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"workflow": args.Workflow, "project": args.Project},
	})
}

func (s *Server) handleExecutionGet(ctx context.Context, args ExecutionIDArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "execution_get", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"execution", "get", args.ID, "--env", args.Env}
	return s.invoke(ctx, invocation{
		ToolName: "execution_get",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"executionId": args.ID},
	})
}

func (s *Server) handleExecutionWait(ctx context.Context, args ExecutionWaitArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "execution_wait", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"execution", "wait", args.ID, "--env", args.Env}
	addDurationFlag(&cliArgs, "--timeout", args.TimeoutSeconds)
	addDurationFlag(&cliArgs, "--interval", args.IntervalSeconds)
	addFlag(&cliArgs, "--diagnose-on-failure", args.DiagnoseOnFailure)
	return s.invoke(ctx, invocation{
		ToolName: "execution_wait",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"executionId": args.ID},
	})
}

func (s *Server) handleExecutionFailures(ctx context.Context, args ExecutionIDArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "execution_failures", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"execution", "failures", args.ID, "--env", args.Env}
	return s.invoke(ctx, invocation{
		ToolName: "execution_failures",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"executionId": args.ID},
	})
}

func (s *Server) handleExecutionDiagnose(ctx context.Context, args ExecutionDiagnoseArgs) ToolResult {
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "execution_diagnose", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"execution", "diagnose", args.ID, "--env", args.Env}
	addIntFlag(&cliArgs, "--limit", args.Limit)
	return s.invoke(ctx, invocation{
		ToolName: "execution_diagnose",
		CLIArgs:  cliArgs,
		Remote:   true,
		Env:      args.Env,
		Audit:    map[string]any{"executionId": args.ID},
	})
}

func (s *Server) handleExecutionRetry(ctx context.Context, args ExecutionRetryArgs) ToolResult {
	dryRun := dryRunValue(args.DryRun)
	if err := requireValue("id", args.ID); err != nil {
		return s.securityResult(invocation{ToolName: "execution_retry", Env: args.Env, Remote: true}, err.Error())
	}
	cliArgs := []string{"execution", "retry", args.ID, "--env", args.Env}
	addBoolFlag(&cliArgs, "--load-workflow", args.LoadWorkflow)
	addBoolFlag(&cliArgs, "--wait", args.Wait)
	addFlag(&cliArgs, "--diagnose-on-failure", args.DiagnoseOnFailure)
	return s.invokeMutation(ctx, "execution_retry", args.Env, dryRun, args.ConfirmMutation, args.ConfirmationPhrase, cliArgs,
		map[string]any{"executionId": args.ID},
		map[string]any{
			"env":                 args.Env,
			"id":                  args.ID,
			"load_workflow":       args.LoadWorkflow,
			"wait":                args.Wait,
			"diagnose_on_failure": args.DiagnoseOnFailure,
		})
}
