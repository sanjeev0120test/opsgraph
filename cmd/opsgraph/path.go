package main

import (
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/pathfind"
	"github.com/spf13/cobra"
)

func newPathCmd() *cobra.Command {
	var src sourceFlags
	var format string
	cmd := &cobra.Command{
		Use:               "path <from> <to>",
		Short:             "Find the shortest path between two services (depends-on, then dependents)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeServiceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArg("service", args[0]); err != nil {
				return err
			}
			if err := requireArg("service", args[1]); err != nil {
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
			from, err := ls.store.GetServiceByNameOrAlias(args[0])
			if err != nil {
				return failLookup(args[0], err)
			}
			to, err := ls.store.GetServiceByNameOrAlias(args[1])
			if err != nil {
				return failLookup(args[1], err)
			}
			if from.ID == to.ID {
				return fail(2, "path requires two distinct services (both resolve to %q)", from.ID)
			}
			deps, err := ls.store.ListAllDependencies()
			if err != nil {
				return fail(2, "%v", err)
			}
			p, err := pathfind.ShortestAny(deps, from.ID, to.ID)
			if err != nil {
				if format == "json" {
					_ = output.JSON(cmd.OutOrStdout(), map[string]any{
						"from":      from.ID,
						"to":        to.ID,
						"found":     false,
						"ok":        false,
						"nodes":     []string{},
						"hops":      0,
						"direction": "",
						"error":     err.Error(),
					})
				}
				return fail(1, "%v", err)
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"from":      p.From,
					"to":        p.To,
					"found":     true,
					"ok":        true,
					"nodes":     p.Nodes,
					"hops":      p.Hops,
					"direction": p.Direction,
				})
			}
			cmd.Printf("PATH  %v\n", p.Nodes)
			cmd.Printf("HOPS  %d\n", p.Hops)
			if p.Direction == "dependents" {
				cmd.Printf("VIA   dependents (reverse of depends-on)\n")
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}
