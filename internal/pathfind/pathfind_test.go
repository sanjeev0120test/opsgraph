package pathfind_test

import (
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/pathfind"
)

func TestShortestPath(t *testing.T) {
	deps := []model.Dependency{
		{FromServiceID: "order", ToServiceID: "checkout"},
		{FromServiceID: "checkout", ToServiceID: "auth"},
		{FromServiceID: "checkout", ToServiceID: "redis"},
	}
	p, err := pathfind.Shortest(deps, "order", "auth")
	if err != nil {
		t.Fatal(err)
	}
	if p.Hops != 2 || len(p.Nodes) != 3 {
		t.Fatalf("got %+v", p)
	}
	if p.Nodes[0] != "order" || p.Nodes[1] != "checkout" || p.Nodes[2] != "auth" {
		t.Fatalf("nodes = %v", p.Nodes)
	}
}

func TestNoPath(t *testing.T) {
	_, err := pathfind.Shortest(nil, "a", "b")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShortestThroughCycle(t *testing.T) {
	deps := []model.Dependency{
		{FromServiceID: "a", ToServiceID: "b"},
		{FromServiceID: "b", ToServiceID: "c"},
		{FromServiceID: "c", ToServiceID: "a"},
		{FromServiceID: "", ToServiceID: "b"},
	}
	var first pathfind.Path
	for i := 0; i < 100; i++ {
		p, err := pathfind.Shortest(deps, "a", "c")
		if err != nil {
			t.Fatal(err)
		}
		if p.Hops != 2 || p.Nodes[0] != "a" || p.Nodes[2] != "c" {
			t.Fatalf("iter %d: %+v", i, p)
		}
		if first.Nodes == nil {
			first = p
		} else if p.Hops != first.Hops || p.Nodes[1] != first.Nodes[1] {
			t.Fatalf("nondeterministic: %+v vs %+v", first, p)
		}
	}
}

func TestShortestAnyReverseDependents(t *testing.T) {
	deps := []model.Dependency{
		{FromServiceID: "order", ToServiceID: "checkout"},
		{FromServiceID: "checkout", ToServiceID: "auth"},
	}
	p, err := pathfind.ShortestAny(deps, "auth", "order")
	if err != nil {
		t.Fatal(err)
	}
	if p.Direction != "dependents" || p.Hops != 2 || p.Nodes[0] != "auth" || p.Nodes[2] != "order" {
		t.Fatalf("reverse path: %+v", p)
	}
}

func TestShortestEmptyIDs(t *testing.T) {
	if _, err := pathfind.Shortest(nil, "", "a"); err == nil {
		t.Fatal("empty from must fail")
	}
}
