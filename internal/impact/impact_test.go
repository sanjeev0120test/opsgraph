package impact_test

import (
	"strings"
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/impact"
	"github.com/sanjeev0120test/opsgraph/internal/model"
)

func TestDownstreamImpact(t *testing.T) {
	svcs := []model.Service{{ID: "auth"}, {ID: "checkout"}, {ID: "order"}}
	deps := []model.Dependency{
		{FromServiceID: "checkout", ToServiceID: "auth"},
		{FromServiceID: "order", ToServiceID: "checkout"},
	}
	res := impact.Downstream("auth", svcs, deps)
	if len(res.Affected) != 2 {
		t.Fatalf("affected=%v", res.Affected)
	}
	if res.MaxDepth < 1 {
		t.Fatalf("max depth=%d", res.MaxDepth)
	}
}

func TestDownstreamCycleAndDiamond(t *testing.T) {
	svcs := []model.Service{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	deps := []model.Dependency{
		{FromServiceID: "b", ToServiceID: "a"},
		{FromServiceID: "c", ToServiceID: "a"},
		{FromServiceID: "d", ToServiceID: "b"},
		{FromServiceID: "d", ToServiceID: "c"},
		{FromServiceID: "a", ToServiceID: "b"}, // cycle a ↔ b
		{FromServiceID: "", ToServiceID: "a"},
		{FromServiceID: "a", ToServiceID: "a"},
		{FromServiceID: "b", ToServiceID: "a"}, // duplicate
	}
	res := impact.Downstream("a", svcs, deps)
	if res.MaxDepth < 1 {
		t.Fatalf("cycle must not prevent a walk: %+v", res)
	}
	want := []string{"b", "c", "d"}
	if len(res.Affected) != 3 {
		t.Fatalf("affected=%v want %v", res.Affected, want)
	}
	for i, id := range want {
		if res.Affected[i] != id {
			t.Fatalf("affected=%v want %v", res.Affected, want)
		}
	}
}

func TestDownstreamDiamondShowsBothParents(t *testing.T) {
	svcs := []model.Service{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	deps := []model.Dependency{
		{FromServiceID: "b", ToServiceID: "a"},
		{FromServiceID: "c", ToServiceID: "a"},
		{FromServiceID: "d", ToServiceID: "b"},
		{FromServiceID: "d", ToServiceID: "c"},
	}
	res := impact.Downstream("a", svcs, deps)
	var sawD int
	var walk func(n impact.Node)
	walk = func(n impact.Node) {
		if n.ID == "d" {
			sawD++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(res.Tree)
	if sawD < 2 {
		t.Fatalf("diamond should show d under both parents, appearances=%d tree=%+v", sawD, res.Tree)
	}
}

func TestDownstreamEmptyRoot(t *testing.T) {
	res := impact.Downstream("  ", nil, []model.Dependency{{FromServiceID: "a", ToServiceID: "b"}})
	if res.Root != "" || len(res.Affected) != 0 {
		t.Fatalf("empty root: %+v", res)
	}
}

func TestCyclesDeterministic(t *testing.T) {
	deps := []model.Dependency{
		{FromServiceID: "b", ToServiceID: "c"},
		{FromServiceID: "c", ToServiceID: "a"},
		{FromServiceID: "a", ToServiceID: "b"},
		{FromServiceID: "x", ToServiceID: "x"},
	}
	var first [][]string
	for i := 0; i < 100; i++ {
		got := impact.Cycles(deps)
		if len(got) != 1 {
			t.Fatalf("iter %d: cycles=%v", i, got)
		}
		if strings.Join(got[0], ",") != "a,b,c,a" {
			t.Fatalf("iter %d: want a→b→c→a, got %v", i, got[0])
		}
		if first == nil {
			first = got
		}
	}
}

func TestCyclesNone(t *testing.T) {
	if got := impact.Cycles([]model.Dependency{
		{FromServiceID: "checkout", ToServiceID: "auth"},
		{FromServiceID: "order", ToServiceID: "checkout"},
	}); len(got) != 0 {
		t.Fatalf("acyclic graph reported cycles: %v", got)
	}
}
