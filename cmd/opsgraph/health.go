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
			fleet := summarizeFleetHealth(svcs)
			out := struct {
				Total   int                 `json:"total"`
				OK      bool                `json:"ok"`
				Counts  map[string]int      `json:"counts"`
				ByState map[string][]string `json:"by_state"`
			}{Total: fleet.Total, OK: fleet.OK, Counts: fleet.Counts, ByState: fleet.ByState}
			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), out); err != nil {
					return fail(2, "%v", err)
				}
			} else {
				status := "ok"
				if !fleet.OK {
					status = "not_ok"
				}
				cmd.Printf("FLEET HEALTH  (%d services)  %s\n", out.Total, status)
				if out.Total == 0 {
					cmd.Println("(no services)")
				} else {
					for _, k := range []string{model.HealthUnhealthy, model.HealthDegraded, model.HealthUnknown, model.HealthHealthy} {
						ids := fleet.ByState[k]
						if len(ids) == 0 {
							cmd.Printf("  %-12s %d\n", k, fleet.Counts[k])
							continue
						}
						cmd.Printf("  %-12s %d  (%s)\n", k, fleet.Counts[k], strings.Join(ids, ", "))
					}
				}
			}
			if strict && !fleet.OK {
				return fail(1, "fleet not healthy: %d degraded, %d unhealthy, %d unknown",
					fleet.DegradedReal, fleet.UnhealthyReal, fleet.UnknownReal)
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

type fleetHealth struct {
	Total         int
	OK            bool
	Counts        map[string]int
	ByState       map[string][]string
	DegradedReal  int
	UnhealthyReal int
	UnknownReal   int
}

func summarizeFleetHealth(svcs []model.Service) fleetHealth {
	counts := map[string]int{
		model.HealthHealthy: 0, model.HealthDegraded: 0,
		model.HealthUnhealthy: 0, model.HealthUnknown: 0,
	}
	by := map[string][]string{
		model.HealthHealthy: {}, model.HealthDegraded: {},
		model.HealthUnhealthy: {}, model.HealthUnknown: {},
	}
	var degradedReal, unhealthyReal, unknownReal int
	for _, s := range svcs {
		h := s.Health
		switch h {
		case model.HealthHealthy, model.HealthDegraded, model.HealthUnhealthy, model.HealthUnknown:
		default:
			h = model.HealthUnknown
		}
		counts[h]++
		by[h] = append(by[h], s.ID)
		if model.IsNoiseForPaging(s) {
			continue
		}
		switch h {
		case model.HealthDegraded:
			degradedReal++
		case model.HealthUnhealthy:
			unhealthyReal++
		case model.HealthUnknown:
			unknownReal++
		}
	}
	for k := range by {
		sort.Strings(by[k])
	}
	return fleetHealth{
		Total:         len(svcs),
		OK:            degradedReal == 0 && unhealthyReal == 0 && unknownReal == 0,
		Counts:        counts,
		ByState:       by,
		DegradedReal:  degradedReal,
		UnhealthyReal: unhealthyReal,
		UnknownReal:   unknownReal,
	}
}
