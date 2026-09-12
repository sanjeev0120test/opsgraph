package main

import (
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

// alertAlwaysVisible reports statuses that ignore --since (still live / silenced).
func alertAlwaysVisible(status string) bool {
	return model.AlertLive(status)
}

func newAlertsCmd() *cobra.Command {
	var src sourceFlags
	var format string
	var firingOnly bool
	var service string
	var since time.Duration
	var limit int
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "List alerts across the fleet",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if err := validSince(since); err != nil {
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
			list, err := ls.store.ListAllAlerts()
			if err != nil {
				return fail(2, "%v", err)
			}
			if list == nil {
				list = []model.Alert{}
			}
			if service != "" {
				svc, err := ls.store.GetServiceByNameOrAlias(service)
				if err != nil {
					return failLookupStore(ls.store, service, err)
				}
				filtered := list[:0]
				for _, a := range list {
					if a.ServiceID == svc.ID {
						filtered = append(filtered, a)
					}
				}
				list = filtered
			}
			if firingOnly {
				filtered := list[:0]
				for _, a := range list {
					if model.AlertActive(a.Status) {
						filtered = append(filtered, a)
					}
				}
				list = filtered
			} else {
				// Historical window: keep live statuses always; trim resolved by --since.
				cutoff := ls.now.Add(-since)
				filtered := list[:0]
				for _, a := range list {
					if alertAlwaysVisible(a.Status) {
						filtered = append(filtered, a)
						continue
					}
					if a.At.IsZero() || a.At.After(ls.now) || a.At.Before(cutoff) {
						continue
					}
					filtered = append(filtered, a)
				}
				list = filtered
			}
			total := len(list)
			truncated := false
			if limit > 0 && len(list) > limit {
				list = list[:limit]
				truncated = true
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"alerts":    list,
					"total":     total,
					"limit":     limit,
					"truncated": truncated,
					"service":   service,
					"firing":    firingOnly,
				})
			}
			if len(list) == 0 {
				cmd.Println("(no alerts)")
				return nil
			}
			for _, a := range list {
				ev := a.EvidenceID
				if ev == "" {
					ev = "-"
				}
				cmd.Printf("%s  %-10s %-10s %-14s %s [%s]\n",
					a.At.Format(time.RFC3339), a.Status, a.Severity, a.ServiceID, a.Name, ev)
			}
			if truncated {
				cmd.Printf("... +%d more (raise --limit)\n", total-limit)
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	_ = cmd.RegisterFlagCompletionFunc("format", completeFormatTableJSON)
	cmd.Flags().BoolVar(&firingOnly, "firing", false, "only active alerts (firing or pending; excludes suppressed/resolved)")
	cmd.Flags().StringVar(&service, "service", "", "filter to one service name/alias")
	_ = cmd.RegisterFlagCompletionFunc("service", completeServiceArg)
	cmd.Flags().DurationVar(&since, "since", 0, "lookback for resolved alerts (live+suppressed always shown; default: config)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max alerts to show (0 = all)")
	return cmd
}
