// Package impact computes recursive downstream blast impact trees.
package impact

import (
	"sort"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// Node is one service in the impact tree.
type Node struct {
	ID       string `json:"id"`
	Health   string `json:"health,omitempty"`
	Depth    int    `json:"depth"`
	Children []Node `json:"children"`
}

// Result is the full downstream impact of a service outage.
type Result struct {
	Root     string   `json:"root"`
	Affected []string `json:"affected"`
	MaxDepth int      `json:"max_depth"`
	Tree     Node     `json:"tree"`
}

// Downstream builds a recursive tree of services that depend (transitively) on root.
// Edge direction: From depends on To, so children of X are services whose To==X.
func Downstream(root string, services []model.Service, deps []model.Dependency) Result {
	health := map[string]string{}
	for _, s := range services {
		health[s.ID] = s.Health
	}
	children := map[string][]string{}
	for _, d := range deps {
		from, to := strings.TrimSpace(d.FromServiceID), strings.TrimSpace(d.ToServiceID)
		if from == "" || to == "" || from == to {
			continue
		}
		children[to] = append(children[to], from)
	}
	for k := range children {
		children[k] = uniqSorted(children[k])
	}

	root = strings.TrimSpace(root)
	if root == "" {
		return Result{Root: "", Affected: []string{}, Tree: Node{ID: "", Children: []Node{}}}
	}

	seen := map[string]bool{root: true}
	affected := []string{}
	maxDepth := 0

	var walk func(id string, depth int) Node
	walk = func(id string, depth int) Node {
		if depth > maxDepth {
			maxDepth = depth
		}
		n := Node{ID: id, Health: health[id], Depth: depth, Children: []Node{}}
		for _, child := range children[id] {
			if seen[child] {
				// Diamond: show the extra edge as a leaf so the tree matches the DAG.
				n.Children = append(n.Children, Node{
					ID: child, Health: health[child], Depth: depth + 1, Children: []Node{},
				})
				continue
			}
			seen[child] = true
			affected = append(affected, child)
			n.Children = append(n.Children, walk(child, depth+1))
		}
		return n
	}

	tree := walk(root, 0)
	sort.Strings(affected)
	return Result{Root: root, Affected: affected, MaxDepth: maxDepth, Tree: tree}
}

// Cycles reports unique simple cycles along depends-on edges (From → To).
// Self-loops are omitted (they are not a multi-node cycle). Order is deterministic.
func Cycles(deps []model.Dependency) [][]string {
	adj := map[string][]string{}
	nodes := map[string]bool{}
	for _, d := range deps {
		from, to := strings.TrimSpace(d.FromServiceID), strings.TrimSpace(d.ToServiceID)
		if from == "" || to == "" || from == to {
			continue
		}
		adj[from] = append(adj[from], to)
		nodes[from] = true
		nodes[to] = true
	}
	for k := range adj {
		adj[k] = uniqSorted(adj[k])
	}
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	stack := []string{}
	seenCycle := map[string]bool{}
	var out [][]string

	var visit func(id string)
	visit = func(id string) {
		color[id] = gray
		stack = append(stack, id)
		for _, next := range adj[id] {
			switch color[next] {
			case white:
				visit(next)
			case gray:
				cyc := cycleFromStack(stack, next)
				key := strings.Join(cyc, "\x00")
				if !seenCycle[key] {
					seenCycle[key] = true
					out = append(out, cyc)
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = black
	}
	for _, id := range ids {
		if color[id] == white {
			visit(id)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i], "\x00") < strings.Join(out[j], "\x00")
	})
	return out
}

func cycleFromStack(stack []string, start string) []string {
	i := 0
	for i < len(stack) && stack[i] != start {
		i++
	}
	raw := append(append([]string{}, stack[i:]...), start)
	return rotateCycle(raw)
}

// rotateCycle turns [b a b] into the lexicographically smallest rotation [a b a].
func rotateCycle(cyc []string) []string {
	if len(cyc) < 2 {
		return cyc
	}
	body := cyc[:len(cyc)-1]
	best := 0
	for i := 1; i < len(body); i++ {
		if lessRotated(body, i, best) {
			best = i
		}
	}
	out := make([]string, 0, len(cyc))
	out = append(out, body[best:]...)
	out = append(out, body[:best]...)
	out = append(out, out[0])
	return out
}

func lessRotated(body []string, i, j int) bool {
	n := len(body)
	for k := 0; k < n; k++ {
		a, b := body[(i+k)%n], body[(j+k)%n]
		if a != b {
			return a < b
		}
	}
	return false
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
