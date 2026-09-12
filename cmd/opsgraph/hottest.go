package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/score"
)

func pickHottestService(ls *loadedStore, since time.Duration) (id string, sc int, err error) {
	svcs, err := ls.store.ListServices()
	if err != nil {
		return "", 0, err
	}
	if len(svcs) == 0 {
		return "", 0, fmt.Errorf("no services to auto-select; dump needs deploy,statefulset,daemonset, or job\nnext: %s", k8sDumpCmd)
	}
	type row struct {
		id    string
		score int
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
			fmt.Fprintf(os.Stderr, "warning: hottest skipped %s: %v\n", s.ID, err)
			continue
		}
		if !hottestHasSignal(s, res) {
			continue
		}
		rows = append(rows, row{id: s.ID, score: score.Compute(res).Score})
	}
	if len(rows) == 0 {
		return "", 0, fmt.Errorf("could not score any service (%d failed); pass a service name", skipped)
	}
	apps := make([]row, 0, len(rows))
	for _, r := range rows {
		if !model.IsSystemAgent(r.id) {
			apps = append(apps, r)
		}
	}
	if len(apps) > 0 {
		rows = apps
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].id < rows[j].id
	})
	return rows[0].id, rows[0].score, nil
}

// hottestHasSignal drops unknown-health rows with no change/alert so a git
// folder or a Completed Job event cannot beat a healthy app (unknown=10).
func hottestHasSignal(s model.Service, res model.AskResult) bool {
	if s.Health == model.HealthDegraded || s.Health == model.HealthUnhealthy {
		return true
	}
	if s.Health == model.HealthHealthy {
		return true
	}
	if _, ok := ask.RecentSuspectChange(res); ok {
		return true
	}
	for _, a := range res.Alerts {
		if model.AlertActive(a.Status) {
			return true
		}
	}
	return false
}
