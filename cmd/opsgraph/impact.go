package main

import (
	"github.com/sanjeev0120test/opsgraph/internal/impact"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newImpactCmd() *cobra.Command {
	var src sourceFlags
	var format string
	cmd := &cobra.Command{
		Use:               "impact <service>",
		Short:             "Show recursive downstream impact if a service fails",
		Long:              "Walks the dependency graph transitively to list every downstream service that would be impacted. For 1-hop neighbors only, use `opsgraph blast`.",
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
			svcs, err := ls.store.ListServices()
			if err != nil {
				return fail(2, "%v", err)
			}
			deps, err := ls.store.ListAllDependencies()
			if err != nil {
				return fail(2, "%v", err)
			}
			res := impact.Downstream(svc.ID, svcs, deps)
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), res)
			}
			cmd.Printf("ROOT       %s\n", res.Root)
			cmd.Printf("AFFECTED   %d (max depth %d)\n", len(res.Affected), res.MaxDepth)
			if len(res.Affected) == 0 {
				cmd.Println("TREE       (no downstream dependents)")
				return nil
			}
			cmd.Println("TREE")
			printImpactNode(cmd, res.Tree, "", true)
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func printImpactNode(cmd *cobra.Command, n impact.Node, prefix string, isLast bool) {
	health := n.Health
	if health == "" {
		health = "unknown"
	}
	if n.Depth == 0 {
		cmd.Printf("%s%s [%s]\n", prefix, n.ID, health)
	} else {
		branch := "├─"
		if isLast {
			branch = "└─"
		}
		cmd.Printf("%s%s %s [%s]\n", prefix, branch, n.ID, health)
	}
	nextPrefix := prefix
	if n.Depth > 0 {
		if isLast {
			nextPrefix += "   "
		} else {
			nextPrefix += "│  "
		}
	}
	for i, c := range n.Children {
		printImpactNode(cmd, c, nextPrefix, i == len(n.Children)-1)
	}
}
