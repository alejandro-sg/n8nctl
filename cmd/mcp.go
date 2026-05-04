package cmd

import (
	"bytes"
	"context"
	"time"

	"github.com/spf13/cobra"

	"github.com/alejandro-sg/n8nctl/internal/buildinfo"
	"github.com/alejandro-sg/n8nctl/internal/mcpserver"
)

func newMCPCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run n8nctl MCP integrations",
	}
	cmd.AddCommand(newMCPServeCmd(a))
	return cmd
}

func newMCPServeCmd(a *app) *cobra.Command {
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve n8nctl tools over local MCP stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := mcpserver.New(mcpserver.Config{
				WorkingDir:    a.deps.WorkingDir,
				ToolTimeout:   timeout,
				RunCLI:        a.mcpCLIRunner(),
				Now:           a.deps.Now,
				ServerVersion: buildinfo.Current().Version,
			})
			if err != nil {
				return err
			}
			return server.Serve(cmd.Context(), a.deps.Streams.In, a.deps.Streams.Out)
		},
	}
	cmd.Flags().DurationVar(&timeout, "tool-timeout", 2*time.Minute, "maximum duration for one MCP tool call")
	return cmd
}

func (a *app) mcpCLIRunner() mcpserver.CLIRunner {
	return func(ctx context.Context, args []string) mcpserver.CLIResult {
		out := &bytes.Buffer{}
		errOut := &bytes.Buffer{}
		deps := a.deps
		deps.Streams = Streams{
			In:     bytes.NewBuffer(nil),
			Out:    out,
			ErrOut: errOut,
		}
		exitCode := ExecuteWithContextAndArgs(ctx, args, deps)
		return mcpserver.CLIResult{
			Stdout:   out.String(),
			Stderr:   errOut.String(),
			ExitCode: exitCode,
		}
	}
}
