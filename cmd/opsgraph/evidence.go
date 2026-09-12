package main

import (
	"errors"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/store"
	"github.com/spf13/cobra"
)

func newEvidenceCmd() *cobra.Command {
	var src sourceFlags
	var format string
	var service string
	var limit int
	cmd := &cobra.Command{
		Use:               "evidence [id]",
		Short:             "List evidence or look up a single evidence record by ID",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeEvidenceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if limit < 0 {
				return fail(2, "invalid --limit %d (must be >= 0)", limit)
			}
			ls, _, err := src.loadCtx(cmd.Context(), 0)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()

			if len(args) == 1 {
				if err := requireArg("evidence id", args[0]); err != nil {
					return err
				}
				ev, err := ls.store.GetEvidence(args[0])
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						return fail(1, "evidence %q not found", args[0])
					}
					return fail(2, "%v", err)
				}
				if format == "json" {
					return output.JSON(cmd.OutOrStdout(), ev)
				}
				cmd.Printf("ID       %s\n", ev.ID)
				cmd.Printf("KIND     %s\n", ev.Kind)
				cmd.Printf("SOURCE   %s\n", ev.Source)
				cmd.Printf("AT       %s\n", ev.At.UTC().Format("2006-01-02T15:04:05Z"))
				cmd.Printf("SUMMARY  %s\n", ev.Summary)
				if ev.ServiceID != "" {
					cmd.Printf("SERVICE  %s\n", ev.ServiceID)
				}
				if ev.RawRef != "" {
					cmd.Printf("RAW_REF  %s\n", ev.RawRef)
				}
				return nil
			}

			var evs []model.Evidence
			if service != "" {
				svc, err := ls.store.GetServiceByNameOrAlias(service)
				if err != nil {
					return failLookupStore(ls.store, service, err)
				}
				evs, err = ls.store.ListEvidenceForService(svc.ID)
				if err != nil {
					return fail(2, "%v", err)
				}
			} else {
				evs, err = ls.store.ListAllEvidence()
				if err != nil {
					return fail(2, "%v", err)
				}
			}
			total := len(evs)
			truncated := false
			if limit > 0 && len(evs) > limit {
				evs = evs[:limit]
				truncated = true
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"evidence":  evs,
					"total":     total,
					"limit":     limit,
					"truncated": truncated,
					"service":   service,
				})
			}
			if len(evs) == 0 {
				cmd.Println("(no evidence)")
				return nil
			}
			for _, ev := range evs {
				if service != "" {
					cmd.Printf("%s  %-10s %-12s %s\n", ev.At.UTC().Format("2006-01-02T15:04:05Z"), ev.Kind, ev.ID, ev.Summary)
				} else {
					cmd.Printf("%s  %-10s %-12s %-14s %s\n", ev.At.UTC().Format("2006-01-02T15:04:05Z"), ev.Kind, ev.ID, ev.ServiceID, ev.Summary)
				}
			}
			if truncated {
				cmd.Printf("... +%d more (raise --limit)\n", total-limit)
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	cmd.Flags().StringVar(&service, "service", "", "filter list to one service name/alias")
	_ = cmd.RegisterFlagCompletionFunc("service", completeServiceArg)
	cmd.Flags().IntVar(&limit, "limit", 0, "max evidence rows when listing (0 = all)")
	return cmd
}
