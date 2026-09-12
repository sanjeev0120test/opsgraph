package main

import (
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newResolveCmd() *cobra.Command {
	var src sourceFlags
	var format string
	cmd := &cobra.Command{
		Use:               "resolve <name-or-alias>",
		Short:             "Resolve a service name or alias to its canonical id",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeServiceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArg("service", args[0]); err != nil {
				return err
			}
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			ls, _, err := src.loadCtx(cmd.Context(), 0)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()
			svc, err := ls.store.GetServiceByNameOrAlias(args[0])
			if err != nil {
				return failLookupStore(ls.store, args[0], err)
			}
			aliases := svc.Aliases
			if aliases == nil {
				aliases = []string{}
			}
			out := struct {
				OK      bool     `json:"ok"`
				Query   string   `json:"query"`
				ID      string   `json:"id"`
				Name    string   `json:"name"`
				Aliases []string `json:"aliases"`
				Health  string   `json:"health"`
				OwnerID string   `json:"owner_id,omitempty"`
			}{OK: true, Query: args[0], ID: svc.ID, Name: svc.Name, Aliases: aliases, Health: svc.Health, OwnerID: svc.OwnerID}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), out)
			}
			cmd.Printf("QUERY    %s\n", out.Query)
			cmd.Printf("ID       %s\n", out.ID)
			cmd.Printf("NAME     %s\n", out.Name)
			if len(out.Aliases) > 0 {
				cmd.Printf("ALIASES  %s\n", strings.Join(out.Aliases, ", "))
			}
			cmd.Printf("HEALTH   %s\n", out.Health)
			if out.OwnerID != "" {
				cmd.Printf("OWNER    %s\n", out.OwnerID)
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}
