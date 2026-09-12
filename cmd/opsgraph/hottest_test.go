package main

import (
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestPickHottestSkipsDependencyStub(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertService(model.Service{ID: "order", Name: "order", Health: model.HealthHealthy, Sources: []string{"fixture"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertService(model.Service{ID: "redis", Name: "redis", Health: model.HealthUnknown, Sources: []string{"dependency"}}); err != nil {
		t.Fatal(err)
	}
	ls := &loadedStore{store: s, now: now, cleanup: func() {}}
	id, _, err := pickHottestService(ls, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id != "order" {
		t.Fatalf("hottest=%q want order (redis stub must not win on unknown=10)", id)
	}
}
