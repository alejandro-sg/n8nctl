package cmd

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/LogicMonitor-IT/n8nctl/internal/api"
	"github.com/LogicMonitor-IT/n8nctl/internal/output"
	"github.com/LogicMonitor-IT/n8nctl/pkg/n8n"
)

type projectSummary struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Role          string `json:"role,omitempty"`
	Type          string `json:"type,omitempty"`
	WorkflowCount *int   `json:"workflowCount,omitempty"`
}

func newProjectCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "project",
		Short: "Inspect n8n projects",
	}

	cmd.AddCommand(newProjectListCmd(a))
	return cmd
}

func newProjectListCmd(a *app) *cobra.Command {
	var envName string
	var limit int
	var skipWorkflowCounts bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List n8n projects with compact metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			envCtx, err := a.envContext(envName)
			if err != nil {
				return err
			}

			projects, err := envCtx.Client.ListProjects(cmd.Context(), api.ListProjectsParams{Limit: limit})
			if err != nil {
				return a.mapAPIError(err, "failed to list projects", map[string]any{
					"environment": envCtx.EnvironmentName,
				})
			}
			sort.SliceStable(projects, func(i, j int) bool {
				return projects[i].Name < projects[j].Name
			})

			summaries := make([]projectSummary, 0, len(projects))
			for _, project := range projects {
				summary := summarizeProject(project)
				if !skipWorkflowCounts {
					count, err := a.projectWorkflowCount(cmd.Context(), envCtx.Client, project.ID.String())
					if err != nil {
						return a.mapAPIError(err, "failed to count project workflows", map[string]any{
							"environment": envCtx.EnvironmentName,
							"projectId":   project.ID.String(),
							"projectName": project.Name,
						})
					}
					summary.WorkflowCount = &count
				}
				summaries = append(summaries, summary)
			}

			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":      "ok",
					"environment": envCtx.EnvironmentName,
					"projects":    summaries,
				})
			}

			rows := make([][]string, 0, len(summaries))
			for _, project := range summaries {
				workflowCount := "-"
				if project.WorkflowCount != nil {
					workflowCount = fmt.Sprintf("%d", *project.WorkflowCount)
				}
				rows = append(rows, []string{
					project.Name,
					project.ID,
					project.Role,
					project.Type,
					workflowCount,
				})
			}
			return output.WriteTable(a.deps.Streams.Out, []string{"NAME", "ID", "ROLE", "TYPE", "WORKFLOWS"}, rows)
		},
	}

	cmd.Flags().StringVar(&envName, "env", "", "target environment name")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum projects to show; 0 means all")
	cmd.Flags().BoolVar(&skipWorkflowCounts, "skip-workflow-counts", false, "skip per-project workflow count queries")
	return cmd
}

func summarizeProject(project n8n.Project) projectSummary {
	return projectSummary{
		ID:   project.ID.String(),
		Name: project.Name,
		Role: project.Role,
		Type: project.Type,
	}
}

func (a *app) projectWorkflowCount(ctx context.Context, client *api.Client, projectID string) (int, error) {
	if projectID == "" {
		return 0, nil
	}
	workflows, err := client.ListWorkflows(ctx, api.ListWorkflowsParams{
		ExcludePinnedData: true,
		ProjectID:         projectID,
	})
	if err != nil {
		return 0, err
	}
	return len(workflows), nil
}
