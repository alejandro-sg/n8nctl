package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/LogicMonitor-IT/n8nctl/internal/buildinfo"
)

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build and release metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			info := buildinfo.Current()
			if a.opts.JSON {
				return a.printJSON(map[string]any{
					"status":  "ok",
					"version": info,
				})
			}

			_, err := fmt.Fprintf(
				a.deps.Streams.Out,
				"n8nctl %s\ncommit: %s\nbuiltBy: %s\ndate: %s\n",
				info.Version,
				info.Commit,
				info.BuiltBy,
				info.Date,
			)
			return err
		},
	}
}
