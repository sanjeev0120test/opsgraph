package ingest_test

import (
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/fixtures"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
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
