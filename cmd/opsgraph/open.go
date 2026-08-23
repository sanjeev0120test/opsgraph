package main

import (
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newOpenCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "open [pack]",
		Short: "Open a fixture pack or .opsgraph file (hottest service)",
		Long: "open is the pack viewer: same as `opsgraph ask --fixture <pack>`.\n" +
			"You can also pass the file directly: `opsgraph incident.opsgraph`.",
		Example: `  opsgraph open incident.opsgraph
  opsgraph incident.opsgraph
  opsgraph open fixtures/incident_checkout --format json`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			path := ""
			if len(args) == 1 {
				path = args[0]
			} else {
				path = defaultPackPath()
			}
			if path == "" {
				return fail(2, "pass a pack path, or put incident.opsgraph in the current directory")
			}
			if err := requireArg("pack", path); err != nil {
				return err
			}
			if !openablePack(path) {
				return fail(2, "%q is not a fixture directory or .opsgraph/.zip pack", path)
			}
			return askHottestFromFixture(cmd, path, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func askHottestFromFixture(cmd *cobra.Command, path, format string) error {
	ls, err := storeFromFixtureDir(path)
	if err != nil {
		return fail(2, "%v", err)
	}
	defer ls.cleanup()
	picked, sc, err := pickHottestService(ls, time.Hour)
	if err != nil {
		return fail(1, "%v", err)
	}
	cmd.PrintErrf("auto-selected hottest service: %s (score %d)\n", picked, sc)
	res, err := ask.Ask(ls.store, picked, ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
	if err != nil {
		return failAsk(err)
	}
	if format == "json" {
		return output.JSON(cmd.OutOrStdout(), res)
	}
	return output.Table(cmd.OutOrStdout(), res)
}
