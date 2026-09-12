package main

import (
	"sort"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/score"
	"github.com/spf13/cobra"
)

func newReceiptCmd() *cobra.Command {
	var src sourceFlags
	var since time.Duration
	var format string
	cmd := &cobra.Command{
		Use:   "receipt [pack]",
		Short: "Print a pasteable incident receipt (service, score, evidence IDs)",
		Long: "receipt is the unique shareable summary: hottest service, severity score,\n" +
			"and the deterministic evidence IDs anyone can replay with opsgraph test.\n" +
			"Paste it into Slack, a ticket, or a postmortem. No SaaS, no LLM.",
		Example: `  opsgraph receipt
  opsgraph receipt incident.opsgraph
  opsgraph receipt --fixture fixtures/incident_checkout --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if len(args) == 1 {
				src.fixture = args[0]
			}
			ls, cfg, err := src.loadCtx(cmd.Context(), since)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()
			if since == 0 {
				since = cfg.Since()
			}
			rec, err := buildReceipt(ls, since, src.fixture)
			if err != nil {
				return fail(1, "%v", err)
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), rec)
			}
			printReceipt(cmd, rec)
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

type incidentReceipt struct {
	OK          bool     `json:"ok"`
	Service     string   `json:"service"`
	Health      string   `json:"health"`
	Score       int      `json:"score"`
	Level       string   `json:"level"`
	GeneratedAt string   `json:"generated_at"`
	EvidenceIDs []string `json:"evidence_ids"`
	SHA256      string   `json:"sha256,omitempty"`
}

func buildReceipt(ls *loadedStore, since time.Duration, packPath string) (*incidentReceipt, error) {
	id, _, err := pickHottestService(ls, since)
	if err != nil {
		return nil, err
	}
	res, err := ask.Ask(ls.store, id, ask.Options{Since: since, Now: ls.now, WithRunbook: true})
	if err != nil {
		return nil, err
	}
	sc := score.Compute(res)
	ids := make([]string, 0, len(res.Evidence))
	seen := map[string]bool{}
	for _, e := range res.Evidence {
		if e.ID == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return &incidentReceipt{
		OK:          true,
		Service:     res.Service.ID,
		Health:      res.Service.Health,
		Score:       sc.Score,
		Level:       sc.Level,
		GeneratedAt: res.GeneratedAt.UTC().Format(time.RFC3339),
		EvidenceIDs: ids,
		SHA256:      maybePackSHA256(packPath),
	}, nil
}

func printReceipt(cmd *cobra.Command, rec *incidentReceipt) {
	cmd.Printf("OPSGRAPH RECEIPT\n")
	cmd.Printf("  service     %s  %s  score %d (%s)\n", rec.Service, rec.Health, rec.Score, rec.Level)
	cmd.Printf("  generated   %s\n", rec.GeneratedAt)
	cmd.Printf("  evidence    %s\n", strings.Join(rec.EvidenceIDs, " "))
	if rec.SHA256 != "" {
		cmd.Printf("  sha256      %s\n", rec.SHA256)
	}
	cmd.Printf("detail: opsgraph ask %s   ·   opsgraph handoff %s\n", rec.Service, rec.Service)
	cmd.Printf("replay: opsgraph test <pack>   ·   prove: opsgraph prove\n")
}
