package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/score"
	"github.com/spf13/cobra"
)

func newProveCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "prove",
		Short: "Prove opsgraph works on this machine (offline, no cluster, no account)",
		Long: "prove runs the built-in incident, writes a pack, and replays it bit-identically.\n" +
			"Anyone can validate the unique property: the same evidence IDs and JSON on any OS.\n\n" +
			"  opsgraph prove",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			ls, err := embeddedCheckout()
			if err != nil {
				return fail(2, "%v", err)
			}
			defer ls.cleanup()

			res, err := ask.Ask(ls.store, "checkout", ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
			if err != nil {
				return failAsk(err)
			}
			sc := score.Compute(res)

			tmp, err := os.MkdirTemp("", "opsgraph-prove-*")
			if err != nil {
				return fail(2, "%v", err)
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			dir := filepath.Join(tmp, "pack")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fail(2, "%v", err)
			}
			if err := ingest.WritePack(ls.store, ls.now, dir); err != nil {
				return fail(2, "write pack: %v", err)
			}
			n, err := writePackGoldens(ls, dir)
			if err != nil {
				return fail(2, "write goldens: %v", err)
			}
			if err := provePackReplay(dir); err != nil {
				return fail(1, "pack replay failed: %v", err)
			}
			zipPath := filepath.Join(tmp, "incident.opsgraph")
			if err := ingest.ZipPack(dir, zipPath); err != nil {
				return fail(2, "zip pack: %v", err)
			}
			if err := provePackReplay(zipPath); err != nil {
				return fail(1, "zip pack replay failed: %v", err)
			}
			sum, err := ingest.FileSHA256(zipPath)
			if err != nil {
				return fail(2, "sha256: %v", err)
			}

			payload := map[string]any{
				"ok":          true,
				"service":     res.Service.ID,
				"health":      res.Service.Health,
				"score":       sc.Score,
				"evidence":    len(res.Evidence),
				"goldens":     n,
				"pack_replay": true,
				"zip_replay":  true,
				"sha256":      sum,
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), payload)
			}
			cmd.Printf("ok  opsgraph prove\n")
			cmd.Printf("    service:  %s (%s, score %d)\n", res.Service.ID, res.Service.Health, sc.Score)
			cmd.Printf("    evidence: %d ids (deterministic)\n", len(res.Evidence))
			cmd.Printf("    pack:     directory replay identical\n")
			cmd.Printf("    zip:      .opsgraph archive replay identical\n")
			cmd.Printf("    sha256:   %s\n", sum)
			cmd.Printf("next: opsgraph pack --out ./incident.opsgraph\n")
			cmd.Printf("      opsgraph test ./incident.opsgraph\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}
