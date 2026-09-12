package main

import (
	"sort"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/score"
	"github.com/spf13/cobra"
)

func newTopCmd() *cobra.Command {
	var src sourceFlags
	var format string
	var limit int
	var since time.Duration
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Rank services by incident severity score",
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
			svcs, err := ls.store.ListServices()
			if err != nil {
				return fail(2, "%v", err)
			}
			type row struct {
				Service string `json:"service"`
				Health  string `json:"health"`
				Score   int    `json:"score"`
				Level   string `json:"level"`
			}
			rows := make([]row, 0, len(svcs))
			skipped := 0
			for _, s := range svcs {
				if model.IsNoiseForPaging(s) {
					continue
				}
				res, err := askService(ls, s.ID, since)
				if err != nil {
					skipped++
					cmd.PrintErrf("warning: skip %s: %v\n", s.ID, err)
					continue
				}
				sc := score.Compute(res)
				rows = append(rows, row{Service: s.ID, Health: s.Health, Score: sc.Score, Level: sc.Level})
			}
			sort.SliceStable(rows, func(i, j int) bool {
				if rows[i].Score != rows[j].Score {
					return rows[i].Score > rows[j].Score
				}
				return rows[i].Service < rows[j].Service
			})
			if len(rows) == 0 && skipped > 0 {
				return fail(1, "top: all %d service(s) failed to score", skipped)
			}
			total := len(rows)
			truncated := false
			if limit > 0 && len(rows) > limit {
				rows = rows[:limit]
				truncated = true
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"services":  rows,
					"total":     total,
					"truncated": truncated,
					"limit":     limit,
					"skipped":   skipped,
				})
			}
			if len(rows) == 0 {
				cmd.Println("(no services)")
				return nil
			}
			cmd.Printf("%-4s %-16s %-12s %-6s %s\n", "#", "SERVICE", "HEALTH", "SCORE", "LEVEL")
			for i, r := range rows {
				cmd.Printf("%-4d %-16s %-12s %-6d %s\n", i+1, r.Service, r.Health, r.Score, r.Level)
			}
			if truncated {
				cmd.Printf("... +%d more (raise --limit)\n", total-limit)
			}
			if skipped > 0 {
				cmd.PrintErrf("warning: %d service(s) skipped due to errors\n", skipped)
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	_ = cmd.RegisterFlagCompletionFunc("format", completeFormatTableJSON)
	cmd.Flags().IntVar(&limit, "limit", 10, "max services to show (0 = all)")
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window")
	return cmd
}
