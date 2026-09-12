package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/score"
)

func pickHottestService(ls *loadedStore, since time.Duration) (id string, sc int, err error) {
	svcs, err := ls.store.ListServices()
	if err != nil {
		return "", 0, err
	}
	if len(svcs) == 0 {
		return "", 0, fmt.Errorf("no services to auto-select; pass a service name")
	}
	type row struct {
		id    string
		score int
	}
	rows := make([]row, 0, len(svcs))
	skipped := 0
	for _, s := range svcs {
		if model.IsDependencyStub(s) {
			continue
		}
		res, err := askService(ls, s.ID, since)
		if err != nil {
			skipped++
			continue
		}
		rows = append(rows, row{id: s.ID, score: score.Compute(res).Score})
	}
	if len(rows) == 0 {
		return "", 0, fmt.Errorf("could not score any service (%d failed); pass a service name", skipped)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		return rows[i].id < rows[j].id
	})
	return rows[0].id, rows[0].score, nil
}
