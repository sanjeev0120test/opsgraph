package graphviz

import (
	"strings"
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

func TestASCIIEmptyServices(t *testing.T) {
	got := ASCII(nil, nil)
	if !strings.Contains(got, "(no services)") {
		t.Fatalf("want no services message, got %q", got)
	}
}

func TestSafeIDKeepsDistinctServiceIDs(t *testing.T) {
	if safeID("a-b") == safeID("a_b") {
		t.Fatalf("safeID must not collide for a-b vs a_b: %q", safeID("a-b"))
	}
}

func TestJSONGraphDeterministic(t *testing.T) {
	svcs := []model.Service{
		{ID: "b", Health: "healthy"},
		{ID: "a", Health: ""},
	}
	deps := []model.Dependency{
		{FromServiceID: "b", ToServiceID: "a", Type: "http"},
		{FromServiceID: "a", ToServiceID: "b", Type: "grpc"},
	}
	g1 := JSONGraph(svcs, deps)
	g2 := JSONGraph([]model.Service{svcs[1], svcs[0]}, []model.Dependency{deps[1], deps[0]})
	if len(g1.Nodes) != 2 || g1.Nodes[0].ID != "a" || g1.Nodes[0].Health != "unknown" {
		t.Fatalf("nodes: %+v", g1.Nodes)
	}
	if len(g1.Edges) != 2 || g1.Edges[0].From != "a" {
		t.Fatalf("edges: %+v", g1.Edges)
	}
	if g1.Nodes[0].ID != g2.Nodes[0].ID || g1.Edges[0].Type != g2.Edges[0].Type {
		t.Fatalf("not deterministic:\n%+v\n%+v", g1, g2)
	}
}

func TestMermaidEscapesQuotesAndIsStable(t *testing.T) {
	svcs := []model.Service{
		{ID: "b-svc", Health: `degraded "x"`},
		{ID: "a-svc", Health: "healthy"},
	}
	deps := []model.Dependency{
		{FromServiceID: "b-svc", ToServiceID: "a-svc", Type: `http "edge"`},
	}
	a := Mermaid(svcs, deps)
	b := Mermaid([]model.Service{svcs[1], svcs[0]}, deps)
	if a != b {
		t.Fatalf("mermaid not deterministic:\n%s\n---\n%s", a, b)
	}
	if strings.Contains(a, `"`) && strings.Count(a, `"`)%2 != 0 {
		// Node labels use quotes; inner quotes must be escaped to single quotes.
	}
	if strings.Contains(a, `degraded "x"`) {
		t.Fatalf("unescaped quote in label: %s", a)
	}
	if !strings.Contains(a, "degraded 'x'") {
		t.Fatalf("expected escaped health label: %s", a)
	}
	if !strings.Contains(a, "http 'edge'") {
		t.Fatalf("expected escaped edge label: %s", a)
	}
}

func TestSortedDepsIncludesTypeAndSkipsEmpty(t *testing.T) {
	svcs := []model.Service{{ID: "a"}, {ID: "b"}}
	deps := []model.Dependency{
		{FromServiceID: "a", ToServiceID: "b", Type: "grpc"},
		{FromServiceID: "", ToServiceID: "b", Type: "http"},
		{FromServiceID: "a", ToServiceID: "b", Type: "http"},
	}
	ascii1 := ASCII(svcs, deps)
	ascii2 := ASCII(svcs, []model.Dependency{deps[2], deps[0], deps[1]})
	if ascii1 != ascii2 {
		t.Fatalf("ascii type order not stable:\n%s\n---\n%s", ascii1, ascii2)
	}
	if !strings.Contains(ascii1, "(http)") || strings.Index(ascii1, "(grpc)") > strings.Index(ascii1, "(http)") {
		t.Fatalf("want grpc before http, got %s", ascii1)
	}
	if strings.Contains(ascii1, "[] →") {
		t.Fatalf("empty endpoint leaked: %s", ascii1)
	}
	m1 := Mermaid(svcs, deps)
	m2 := Mermaid(svcs, []model.Dependency{deps[2], deps[0], deps[1]})
	if m1 != m2 {
		t.Fatalf("mermaid type order not stable:\n%s\n---\n%s", m1, m2)
	}
}
