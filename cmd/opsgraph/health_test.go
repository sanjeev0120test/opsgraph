package main

import (
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

func TestSummarizeFleetHealthIgnoresStubs(t *testing.T) {
	svcs := []model.Service{
		{ID: "api", Health: model.HealthHealthy, Sources: []string{"fixture"}},
		{ID: "redis", Health: model.HealthUnknown, Sources: []string{"dependency"}},
	}
	got := summarizeFleetHealth(svcs)
	if !got.OK {
		t.Fatalf("healthy fleet + unknown stub must be ok: %+v", got)
	}
	if got.UnknownReal != 0 || got.Counts[model.HealthUnknown] != 1 {
		t.Fatalf("unknown display=%d real=%d", got.Counts[model.HealthUnknown], got.UnknownReal)
	}

	svcs = append(svcs, model.Service{ID: "payments", Health: model.HealthUnknown, Sources: []string{"fixture"}})
	got = summarizeFleetHealth(svcs)
	if got.OK || got.UnknownReal != 1 {
		t.Fatalf("real unknown must fail strict: ok=%v real=%d", got.OK, got.UnknownReal)
	}

	// A healthy stub must not cancel a real unknown (old: unknown - stubs).
	mixed := []model.Service{
		{ID: "cache", Health: model.HealthHealthy, Sources: []string{"dependency"}},
		{ID: "ghost", Health: model.HealthUnknown, Sources: []string{"kubernetes"}},
	}
	got = summarizeFleetHealth(mixed)
	if got.OK || got.UnknownReal != 1 {
		t.Fatalf("healthy stub must not hide real unknown: ok=%v real=%d", got.OK, got.UnknownReal)
	}

	gitOnly := []model.Service{
		{ID: "api", Health: model.HealthHealthy, Sources: []string{"kubernetes"}},
		{ID: "internal", Health: model.HealthUnknown, Sources: []string{"git"}},
	}
	got = summarizeFleetHealth(gitOnly)
	if !got.OK || got.UnknownReal != 0 {
		t.Fatalf("git folder must not fail --strict: ok=%v real=%d", got.OK, got.UnknownReal)
	}
}
