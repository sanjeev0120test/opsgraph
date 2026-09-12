// Package pathfind finds dependency paths between services.
package pathfind

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// Path is an ordered list of service IDs from start to goal along depends-on edges.
type Path struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Nodes []string `json:"nodes"`
	Hops  int      `json:"hops"`
}

// Shortest finds the shortest depends-on path from → to (BFS).
// Direction follows dependency edges: walker moves From → To.
func Shortest(deps []model.Dependency, from, to string) (Path, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" {
		return Path{}, fmt.Errorf("path requires non-empty service ids")
	}
	if from == to {
		return Path{From: from, To: to, Nodes: []string{from}, Hops: 0}, nil
	}
	adj := map[string][]string{}
	for _, d := range deps {
		f, t := strings.TrimSpace(d.FromServiceID), strings.TrimSpace(d.ToServiceID)
		if f == "" || t == "" {
			continue
		}
		adj[f] = append(adj[f], t)
	}
	for k := range adj {
		adj[k] = uniqSorted(adj[k])
	}
	type item struct {
		id   string
		path []string
	}
	q := []item{{from, []string{from}}}
	seen := map[string]bool{from: true}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		for _, next := range adj[cur.id] {
			if seen[next] {
				continue
			}
			np := append(append([]string{}, cur.path...), next)
			if next == to {
				return Path{From: from, To: to, Nodes: np, Hops: len(np) - 1}, nil
			}
			seen[next] = true
			q = append(q, item{next, np})
		}
	}
	return Path{}, fmt.Errorf("no dependency path from %q to %q", from, to)
}

func uniqSorted(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
