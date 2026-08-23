package ingest_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/fixtures"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestWritePackRoundTrip(t *testing.T) {
	fsys, err := fixtures.CheckoutFS()
	if err != nil {
		t.Fatal(err)
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now, err := ingest.IngestFixtureFS(s, fsys)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := ingest.WritePack(s, now, dir); err != nil {
		t.Fatal(err)
	}
	s2, cleanup2, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup2)
	now2, err := ingest.IngestFixtureDir(s2, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !now2.Equal(now) {
		t.Fatalf("now = %v, want %v", now2, now)
	}
	co, err := s2.GetService("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if co.Health != "degraded" {
		t.Fatalf("checkout health = %q", co.Health)
	}
	alerts, err := s2.ListAlerts("checkout", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].Name != "CheckoutErrorRateHigh" {
		t.Fatalf("alerts: %+v", alerts)
	}
	rb, err := s2.GetRunbook("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.Steps) != 3 {
		t.Fatalf("runbook steps = %d", len(rb.Steps))
	}
}

func TestWritePackLabeledServiceHashStableFromScratch(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := s.UpsertService(model.Service{
		ID: "svc", Name: "svc", Health: model.HealthDegraded,
		Aliases: []string{"z", "a"}, Labels: map[string]string{"tier": "prod", "app": "svc"},
		Sources: []string{"kubernetes", "fixture"},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	var first string
	for i := 0; i < 25; i++ {
		dir := t.TempDir()
		if err := ingest.WritePack(s, now, dir); err != nil {
			t.Fatal(err)
		}
		zipPath := filepath.Join(t.TempDir(), "p.opsgraph")
		if err := ingest.ZipPack(dir, zipPath); err != nil {
			t.Fatal(err)
		}
		sum, err := ingest.FileSHA256(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = sum
			continue
		}
		if sum != first {
			t.Fatalf("labeled pack hash drifted on from-scratch run %d\n%s\n%s", i+1, first, sum)
		}
	}
}
