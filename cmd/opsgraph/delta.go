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

func newDeltaCmd() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "delta <before> <after>",
		Short: "Diff two incident packs by deterministic evidence IDs",
		Long: "delta compares two fixture dirs or .opsgraph files by evidence ID set.\n" +
			"That is the unique org workflow: email morning.opsgraph and now.opsgraph,\n" +
			"diff them offline, no cluster and no SaaS timeline.\n\n" +
			"Exit 0 if the ID sets match, 1 if they differ.",
		Example: `  opsgraph delta morning.opsgraph now.opsgraph
  opsgraph delta fixtures/incident_checkout fixtures/fleet_healthy --format json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if err := requireArg("before", args[0]); err != nil {
				return err
			}
			if err := requireArg("after", args[1]); err != nil {
				return err
			}
			a, err := snapshotPack(args[0])
			if err != nil {
				return fail(2, "before: %v", err)
			}
			defer a.cleanup()
			b, err := snapshotPack(args[1])
			if err != nil {
				return fail(2, "after: %v", err)
			}
			defer b.cleanup()
			added, removed, shared := diffSorted(a.Evidence, b.Evidence)
			identical := len(added) == 0 && len(removed) == 0
			payload := map[string]any{
				"ok":          identical,
				"identical":   identical,
				"added":       added,
				"removed":     removed,
				"shared":      len(shared),
				"before":      a.public(),
				"after":       b.public(),
				"score_delta": b.Score - a.Score,
			}
			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), payload); err != nil {
					return fail(2, "%v", err)
				}
			} else {
				cmd.Printf("OPSGRAPH DELTA\n")
				cmd.Printf("  before  %s  %s %s score %d  %d evidence\n", a.Path, a.Hottest, a.Health, a.Score, len(a.Evidence))
				cmd.Printf("  after   %s  %s %s score %d  %d evidence\n", b.Path, b.Hottest, b.Health, b.Score, len(b.Evidence))
				cmd.Printf("  shared  %d  added %d  removed %d  score_delta %+d\n", len(shared), len(added), len(removed), b.Score-a.Score)
				if len(added) > 0 {
					cmd.Printf("  + %s\n", joinLimited(added, 12))
				}
				if len(removed) > 0 {
					cmd.Printf("  - %s\n", joinLimited(removed, 12))
				}
				if identical {
					cmd.Printf("identical evidence IDs\n")
				}
			}
			if !identical {
				return fail(1, "%d added, %d removed evidence id(s)", len(added), len(removed))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

type packSnapshot struct {
	Path     string
	Hottest  string
	Health   string
	Score    int
	Evidence []string
	SHA256   string
	cleanup  func()
}

func (s packSnapshot) public() map[string]any {
	out := map[string]any{
		"path":     s.Path,
		"hottest":  s.Hottest,
		"health":   s.Health,
		"score":    s.Score,
		"evidence": len(s.Evidence),
	}
	if s.SHA256 != "" {
		out["sha256"] = s.SHA256
	}
	return out
}

func snapshotPack(path string) (*packSnapshot, error) {
	ls, err := storeFromFixtureDir(path)
	if err != nil {
		return nil, err
	}
	id, _, err := pickHottestService(ls, time.Hour)
	if err != nil {
		ls.cleanup()
		return nil, err
	}
	res, err := ask.Ask(ls.store, id, ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
	if err != nil {
		ls.cleanup()
		return nil, err
	}
	sc := score.Compute(res)
	evs, err := ls.store.ListAllEvidence()
	if err != nil {
		ls.cleanup()
		return nil, err
	}
	ids := make([]string, 0, len(evs))
	seen := map[string]bool{}
	for _, e := range evs {
		if e.ID == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		ids = append(ids, e.ID)
	}
	sort.Strings(ids)
	return &packSnapshot{
		Path:     path,
		Hottest:  res.Service.ID,
		Health:   res.Service.Health,
		Score:    sc.Score,
		Evidence: ids,
		SHA256:   maybePackSHA256(path),
		cleanup:  ls.cleanup,
	}, nil
}

func diffSorted(a, b []string) (added, removed, shared []string) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			shared = append(shared, a[i])
			i++
			j++
		case a[i] < b[j]:
			removed = append(removed, a[i])
			i++
		default:
			added = append(added, b[j])
			j++
		}
	}
	removed = append(removed, a[i:]...)
	added = append(added, b[j:]...)
	if added == nil {
		added = []string{}
	}
	if removed == nil {
		removed = []string{}
	}
	return added, removed, shared
}

func joinLimited(ids []string, n int) string {
	if len(ids) <= n {
		return strings.Join(ids, " ")
	}
	return strings.Join(ids[:n], " ") + " …"
}
