package main

import (
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newChangesCmd() *cobra.Command {
	var src sourceFlags
	var format string
	var service string
	var limit int
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "changes",
		Short: "List recent changes (optionally filtered by service)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if limit < 0 {
				return fail(2, "invalid --limit %d (must be >= 0)", limit)
			}
			ls, cfg, err := src.loadCtx(cmd.Context(), since)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()
			if since == 0 {
				since = cfg.Since()
			}
			// Same floor as ask so the change browser cannot hide a prime suspect.
			cutoff := ls.now.Add(-ask.ChangeLookback(since))

			list := make([]model.Change, 0)
			if service != "" {
				svc, err := ls.store.GetServiceByNameOrAlias(service)
				if err != nil {
					return failLookupStore(ls.store, service, err)
				}
				list, err = ls.store.ListChanges(svc.ID, cutoff)
				if err != nil {
					return fail(2, "%v", err)
				}
			} else {
				all, err := ls.store.ListAllChanges()
				if err != nil {
					return fail(2, "%v", err)
				}
				for _, c := range all {
					if c.At.Before(cutoff) {
						continue
					}
					list = append(list, c)
				}
			}
			// Match ask/git ingest: never surface future-dated rows from clock skew.
			trimmed := list[:0]
			for _, c := range list {
				if c.At.IsZero() || c.At.After(ls.now) {
					continue
				}
				trimmed = append(trimmed, c)
			}
			list = trimmed
			total := len(list)
			truncated := false
			if limit > 0 && len(list) > limit {
				list = list[:limit]
				truncated = true
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"changes":   list,
					"total":     total,
					"truncated": truncated,
					"limit":     limit,
					"service":   service,
				})
			}
			if len(list) == 0 {
				cmd.Println("(no changes)")
				return nil
			}
			for _, c := range list {
				ev := c.EvidenceID
				if ev == "" {
					ev = "-"
				}
				cmd.Printf("%s  %-10s %-14s %s [%s]\n", c.At.Format(time.RFC3339), c.Type, c.ServiceID, c.Summary, ev)
			}
			if truncated {
				cmd.Printf("... +%d more (raise --limit)\n", total-limit)
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	cmd.Flags().StringVar(&service, "service", "", "filter to one service name/alias")
	_ = cmd.RegisterFlagCompletionFunc("service", completeServiceArg)
	cmd.Flags().IntVar(&limit, "limit", 20, "max changes to show (0 = all)")
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window (default: config default_since or 60m)")
	_ = cmd.RegisterFlagCompletionFunc("format", completeFormatTableJSON)
	return cmd
}
