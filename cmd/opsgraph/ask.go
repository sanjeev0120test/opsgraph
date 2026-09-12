package main

import (
	"errors"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ai"
	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newAskCmd() *cobra.Command {
	var (
		fixture    string
		configPath string
		dataDir    string
		format     string
		since      time.Duration
		withRB     bool
		useAI      bool
	)
	cmd := &cobra.Command{
		Use:   "ask [service]",
		Short: "Show evidence-backed incident context for a service",
		Long: "ask prints timeline, blast radius, owner, and runbook validity.\n" +
			"Omit the service name to auto-select the hottest service by severity score.\n" +
			"With no flags, a k8s-snapshot.yaml (or services/ / apps/ git layout) in cwd is enough.",
		Example: `  opsgraph ask
  opsgraph ask checkout --fixture fixtures/incident_checkout
  opsgraph ask --fixture fixtures/incident_checkout
  opsgraph ask --fixture incident.opsgraph
  opsgraph ask checkout --format json --ai
  opsgraph ask --data-dir .opsgraph/data`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeServiceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			service := ""
			if len(args) == 1 {
				if err := requireArg("service", args[0]); err != nil {
					return err
				}
				service = args[0]
			}
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if err := validSince(since); err != nil {
				return fail(2, "%v", err)
			}
			var err error
			fixture, err = resolveFixtureExclusive(fixture, dataDir)
			if err != nil {
				return err
			}
			cfgPath := configPathOrEnv(configPath)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fail(2, "%v", err)
			}
			if since == 0 {
				since = cfg.Since()
			}

			ls, err := loadAskStore(cmd.Context(), fixture, cfgPath, dataDir, cfg, since)
			if err != nil {
				if errors.Is(err, ErrEmptyStore) {
					return fail(1, "%v", err)
				}
				return fail(2, "%v", err)
			}
			defer ls.cleanup()
			if service == "" {
				picked, sc, err := pickHottestService(ls, since)
				if err != nil {
					return fail(1, "%v", err)
				}
				service = picked
				cmd.PrintErrf("auto-selected hottest service: %s (score %d)\n", service, sc)
			}
			res, err := ask.Ask(ls.store, service, ask.Options{Since: since, Now: ls.now, WithRunbook: withRB})
			if err != nil {
				return failAskStore(ls.store, err)
			}

			if useAI {
				cfg.AI.Enabled = true
				res.AISummary = ai.Summarize(cmd.Context(), cfg, res)
			}

			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), res)
			}
			return output.Table(cmd.OutOrStdout(), res)
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "path to a fixture pack directory (or OPSGRAPH_FIXTURE)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to .opsgraph.yaml (or OPSGRAPH_CONFIG)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "read from a persistent store (or OPSGRAPH_DATA_DIR)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window (default: config default_since or 60m)")
	cmd.Flags().BoolVar(&withRB, "runbook", true, "verify the service runbook")
	cmd.Flags().BoolVar(&useAI, "ai", false, "add a local AI summary (needs Ollama; degrades gracefully)")
	return cmd
}
