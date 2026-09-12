package main

import (
	"sort"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newHealthCmd() *cobra.Command {
	var src sourceFlags
	var format string
	var strict bool
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Fleet health dashboard: counts by health state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			ls, _, err := src.loadCtx(cmd.Context(), 0)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()
			svcs, err := ls.store.ListServices()
			if err != nil {
				return fail(2, "%v", err)
			}
			counts := map[string]int{
				model.HealthHealthy: 0, model.HealthDegraded: 0,
				model.HealthUnhealthy: 0, model.HealthUnknown: 0,
			}
			by := map[string][]string{
				model.HealthHealthy: {}, model.HealthDegraded: {},
				model.HealthUnhealthy: {}, model.HealthUnknown: {},
			}
			for _, s := range svcs {
				h := s.Health
				switch h {
				case model.HealthHealthy, model.HealthDegraded, model.HealthUnhealthy, model.HealthUnknown:
				default:
					h = model.HealthUnknown
				}
				counts[h]++
				by[h] = append(by[h], s.ID)
			}
			for k := range by {
				sort.Strings(by[k])
			}
			stubs := 0
			for _, s := range svcs {
				if model.IsDependencyStub(s) {
					stubs++
				}
			}
			// Synthesized dependency targets are not paging services.
			unknownReal := counts[model.HealthUnknown] - stubs
			if unknownReal < 0 {
				unknownReal = 0
			}
			ok := counts[model.HealthDegraded] == 0 &&
				counts[model.HealthUnhealthy] == 0 &&
				unknownReal == 0
			out := struct {
				Total   int                 `json:"total"`
				OK      bool                `json:"ok"`
				Counts  map[string]int      `json:"counts"`
				ByState map[string][]string `json:"by_state"`
			}{Total: len(svcs), OK: ok, Counts: counts, ByState: by}
			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), out); err != nil {
					return fail(2, "%v", err)
				}
			} else {
				status := "ok"
				if !ok {
					status = "not_ok"
				}
				cmd.Printf("FLEET HEALTH  (%d services)  %s\n", out.Total, status)
				if out.Total == 0 {
					cmd.Println("(no services)")
				} else {
					for _, k := range []string{model.HealthUnhealthy, model.HealthDegraded, model.HealthUnknown, model.HealthHealthy} {
						ids := by[k]
						if len(ids) == 0 {
							cmd.Printf("  %-12s %d\n", k, counts[k])
							continue
						}
						cmd.Printf("  %-12s %d  (%s)\n", k, counts[k], strings.Join(ids, ", "))
					}
				}
			}
			if strict && !ok {
				return fail(1, "fleet not healthy: %d degraded, %d unhealthy, %d unknown",
					counts[model.HealthDegraded], counts[model.HealthUnhealthy], counts[model.HealthUnknown])
			}
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 1 if any non-stub service is degraded, unhealthy, or unknown")
	_ = cmd.RegisterFlagCompletionFunc("format", completeFormatTableJSON)
	return cmd
}
