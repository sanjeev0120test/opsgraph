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

func TestPickHottestSkipsGitOnlyUnknown(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertService(model.Service{ID: "api", Name: "api", Health: model.HealthHealthy, Sources: []string{"kubernetes"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertService(model.Service{ID: "internal", Name: "internal", Health: model.HealthUnknown, Sources: []string{"git"}}); err != nil {
		t.Fatal(err)
	}
	ls := &loadedStore{store: s, now: now, cleanup: func() {}}
	id, _, err := pickHottestService(ls, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id != "api" {
		t.Fatalf("hottest=%q want api (git folder must not win on unknown=10)", id)
	}
}

func TestPickHottestPrefersAppOverAgent(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertService(model.Service{ID: "checkout", Name: "checkout", Health: model.HealthHealthy, Sources: []string{"kubernetes"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertService(model.Service{ID: "fluentbit", Name: "fluentbit", Health: model.HealthDegraded, Sources: []string{"kubernetes"}}); err != nil {
		t.Fatal(err)
	}
	ls := &loadedStore{store: s, now: now, cleanup: func() {}}
	id, _, err := pickHottestService(ls, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if id != "checkout" {
		t.Fatalf("hottest=%q want checkout (kube-system agent must not steal ask)", id)
	}
}
