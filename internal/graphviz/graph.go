// Package graphviz renders the service dependency graph (ASCII, Mermaid, or JSON).
package graphviz

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// Node is a service vertex in the JSON graph.
type Node struct {
	ID     string `json:"id"`
	Health string `json:"health"`
}

// Edge is a dependency From → To (From depends on To).
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type"`
}

// Graph is a machine-readable adjacency list.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// JSONGraph builds a deterministic nodes+edges payload.
func JSONGraph(services []model.Service, deps []model.Dependency) Graph {
	svcs := append([]model.Service(nil), services...)
	sort.SliceStable(svcs, func(i, j int) bool { return svcs[i].ID < svcs[j].ID })
	nodes := make([]Node, 0, len(svcs))
	for _, s := range svcs {
		h := s.Health
		if h == "" {
			h = "unknown"
		}
		nodes = append(nodes, Node{ID: s.ID, Health: h})
	}
	sorted := sortedDeps(deps)
	edges := make([]Edge, 0, len(sorted))
	for _, d := range sorted {
		edges = append(edges, Edge{From: d.FromServiceID, To: d.ToServiceID, Type: d.Type})
	}
	return Graph{Nodes: nodes, Edges: edges}
}

// ASCII returns a simple text dependency map. Edge direction: From → To means From depends on To.
func ASCII(services []model.Service, deps []model.Dependency) string {
	health := map[string]string{}
	for _, s := range services {
		health[s.ID] = s.Health
	}
	var b strings.Builder
	b.WriteString("Dependency graph (From → To = From depends on To)\n")
	if len(services) == 0 {
		b.WriteString("(no services)\n")
		return b.String()
	}
	if len(deps) == 0 {
		b.WriteString("(no edges)\n")
		return b.String()
	}
	sorted := sortedDeps(deps)
	for _, d := range sorted {
		fh, th := health[d.FromServiceID], health[d.ToServiceID]
		if fh == "" {
			fh = "unknown"
		}
		if th == "" {
			th = "unknown"
		}
		fmt.Fprintf(&b, "  %s [%s] → %s [%s]  (%s)\n", d.FromServiceID, fh, d.ToServiceID, th, d.Type)
	}
	return b.String()
}

// Mermaid returns a Mermaid flowchart definition.
func Mermaid(services []model.Service, deps []model.Dependency) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	svcs := append([]model.Service(nil), services...)
	sort.SliceStable(svcs, func(i, j int) bool { return svcs[i].ID < svcs[j].ID })
	seen := map[string]bool{}
	for _, s := range svcs {
		seen[s.ID] = true
		label := mermaidEscape(s.ID) + `\n` + mermaidEscape(s.Health)
		fmt.Fprintf(&b, "  %s[\"%s\"]\n", safeID(s.ID), label)
	}
	sorted := sortedDeps(deps)
	for _, d := range sorted {
		if !seen[d.FromServiceID] {
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", safeID(d.FromServiceID), mermaidEscape(d.FromServiceID))
			seen[d.FromServiceID] = true
		}
		if !seen[d.ToServiceID] {
			fmt.Fprintf(&b, "  %s[\"%s\"]\n", safeID(d.ToServiceID), mermaidEscape(d.ToServiceID))
			seen[d.ToServiceID] = true
		}
		fmt.Fprintf(&b, "  %s -->|%s| %s\n", safeID(d.FromServiceID), mermaidEscape(d.Type), safeID(d.ToServiceID))
	}
	out := b.String()
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func safeID(id string) string {
	// Hex-escape non-alnum so distinct ids (a-b vs a_b) never collide.
	var b strings.Builder
	b.WriteString("n_")
	if id == "" {
		b.WriteString("unknown")
		return b.String()
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			fmt.Fprintf(&b, "_%x", r)
		}
	}
	return b.String()
}

func sortedDeps(deps []model.Dependency) []model.Dependency {
	sorted := make([]model.Dependency, 0, len(deps))
	for _, d := range deps {
		if strings.TrimSpace(d.FromServiceID) == "" || strings.TrimSpace(d.ToServiceID) == "" {
			continue
		}
		sorted = append(sorted, d)
	}
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].FromServiceID != sorted[j].FromServiceID {
			return sorted[i].FromServiceID < sorted[j].FromServiceID
		}
		if sorted[i].ToServiceID != sorted[j].ToServiceID {
			return sorted[i].ToServiceID < sorted[j].ToServiceID
		}
		return sorted[i].Type < sorted[j].Type
	})
	return sorted
}

func mermaidEscape(s string) string {
	s = strings.ReplaceAll(s, `"`, "'")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}
