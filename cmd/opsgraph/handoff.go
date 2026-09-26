package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/fingerprint"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/score"
	"github.com/spf13/cobra"
)

func newHandoffCmd() *cobra.Command {
	var src sourceFlags
	var since time.Duration
	var format string
	cmd := &cobra.Command{
		Use:               "handoff <service>",
		Short:             "Write a short, evidence-backed handoff note for a service",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeServiceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArg("service", args[0]); err != nil {
				return err
			}
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			ls, cfg, err := src.loadCtx(cmd.Context(), since)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()
			if since == 0 {
				since = cfg.Since()
			}
			res, err := askService(ls, args[0], since)
			if err != nil {
				return failAsk(err)
			}
			note := handoffNote(res)
			if format == "json" {
				sc := score.Compute(res)
				fp := fingerprint.Of(res)
				return output.JSON(cmd.OutOrStdout(), map[string]any{
					"service":     res.Service.ID,
					"health":      res.Service.Health,
					"score":       sc.Score,
					"level":       sc.Level,
					"fingerprint": fp.Fingerprint,
					"note":        note,
				})
			}
			fmt.Fprint(cmd.OutOrStdout(), note)
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func handoffNote(res model.AskResult) string {
	sc := score.Compute(res)
	fp := fingerprint.Of(res)
	var b strings.Builder
	fmt.Fprintf(&b, "# Handoff: %s\n\n", res.Service.ID)
	if note := model.RolloutNote(res.Service); note != "" {
		fmt.Fprintf(&b, "- Health: **%s** (%s)\n", res.Service.Health, note)
	} else {
		fmt.Fprintf(&b, "- Health: **%s**\n", res.Service.Health)
	}
	fmt.Fprintf(&b, "- Severity: **%d** (%s)\n", sc.Score, sc.Level)
	fmt.Fprintf(&b, "- Fingerprint: `%s`\n", fp.Fingerprint)
	fmt.Fprintf(&b, "- As of: %s (window last %s)\n", res.GeneratedAt.UTC().Format(time.RFC3339), res.Window)
	if res.Owner != nil {
		who := res.Owner.Name
		if who == "" {
			who = res.Owner.ID
		}
		fmt.Fprintf(&b, "- Owner: %s", who)
		if res.Owner.Email != "" {
			fmt.Fprintf(&b, " <%s>", res.Owner.Email)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## What happened\n")
	if c, ok := ask.RecentSuspectChange(res); ok {
		fmt.Fprintf(&b, "- Prime suspect: %s %q", c.Type, c.Summary)
		if c.EvidenceID != "" {
			fmt.Fprintf(&b, " [%s]", c.EvidenceID)
		}
		b.WriteString("\n")
	} else if len(res.Changes) > 0 {
		b.WriteString("- No change inside the 30m suspect window; older lookback changes exist.\n")
	} else {
		b.WriteString("- No change inside the 30m suspect window.\n")
	}
	if note := model.RolloutNote(res.Service); note != "" {
		fmt.Fprintf(&b, "- Rollout: %s\n", note)
	}
	for _, c := range res.Correlations {
		fmt.Fprintf(&b, "- Linked: %s", c.Summary)
		if c.ChangeEvidence != "" {
			fmt.Fprintf(&b, " [%s]", c.ChangeEvidence)
		}
		if c.AlertEvidence != "" {
			fmt.Fprintf(&b, " [%s]", c.AlertEvidence)
		}
		b.WriteString("\n")
	}
	for _, a := range res.Alerts {
		if !model.AlertLive(a.Status) {
			continue
		}
		label := "Alert"
		if a.Status == "suppressed" {
			label = "Suppressed alert"
		}
		fmt.Fprintf(&b, "- %s: **%s** (%s, %s)", label, a.Name, a.Severity, a.Status)
		if a.EvidenceID != "" {
			fmt.Fprintf(&b, " [%s]", a.EvidenceID)
		}
		b.WriteString("\n")
	}
	for _, u := range res.Upstream {
		if u.Health == model.HealthUnhealthy || u.Health == model.HealthDegraded {
			fmt.Fprintf(&b, "- Upstream pressure: %s is %s\n", u.ID, u.Health)
		}
	}
	if res.RunbookResult != nil {
		fmt.Fprintf(&b, "- Runbook `%s` is **%s**\n", res.RunbookResult.Path, res.RunbookResult.Status)
	}
	b.WriteString("\n## Next for the next on-call\n")
	if len(res.Recommendations) == 0 {
		b.WriteString("1. Re-run `opsgraph ask` and verify current health.\n")
	} else {
		// Prefer first recommendations + always keep the trailing handoff (R6).
		recs := res.Recommendations
		if len(recs) > 6 {
			tail := recs[len(recs)-1]
			recs = append(append([]string{}, recs[:5]...), tail)
		}
		for i, r := range recs {
			fmt.Fprintf(&b, "%d. %s\n", i+1, r)
		}
	}
	if len(res.Evidence) > 0 {
		b.WriteString("\n## Evidence IDs\n")
		for _, e := range res.Evidence {
			fmt.Fprintf(&b, "- `%s` — %s\n", e.ID, e.Summary)
		}
	}
	return b.String()
}
