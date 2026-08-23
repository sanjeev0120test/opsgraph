package store

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, cleanup, err := OpenTemp()
	if err != nil {
		t.Fatalf("OpenTemp: %v", err)
	}
	t.Cleanup(cleanup)
	return s
}

func TestLatestChange(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertService(model.Service{ID: "a", Name: "a", Health: model.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.LatestChange("a"); err != nil || ok {
		t.Fatalf("empty LatestChange: ok=%v err=%v", ok, err)
	}
	if err := s.UpsertChange(model.Change{ID: "c1", ServiceID: "a", At: now.Add(-30 * time.Minute), Type: "commit", Summary: "older"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChange(model.Change{ID: "c2", ServiceID: "a", At: now.Add(-10 * time.Minute), Type: "deploy", Summary: "newer"}); err != nil {
		t.Fatal(err)
	}
	ch, ok, err := s.LatestChange("a")
	if err != nil || !ok || ch == nil || ch.ID != "c2" {
		t.Fatalf("LatestChange: %+v ok=%v err=%v", ch, ok, err)
	}
}

func TestListAllChangesAndAlerts(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.UpsertService(model.Service{ID: "a", Name: "a", Health: model.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChange(model.Change{ID: "c2", ServiceID: "a", At: now.Add(-10 * time.Minute), Type: "commit", Summary: "newer"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertChange(model.Change{ID: "c1", ServiceID: "a", At: now.Add(-30 * time.Minute), Type: "commit", Summary: "older"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlert(model.Alert{ID: "al1", ServiceID: "a", At: now, Name: "A", Status: "resolved"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlert(model.Alert{ID: "al2", ServiceID: "a", At: now.Add(-time.Minute), Name: "B", Status: "firing"}); err != nil {
		t.Fatal(err)
	}
	changes, err := s.ListAllChanges()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 || changes[0].ID != "c2" || changes[1].ID != "c1" {
		t.Fatalf("ListAllChanges order: %+v", changes)
	}
	alerts, err := s.ListAllAlerts()
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 2 || alerts[0].ID != "al2" || alerts[0].Status != "firing" {
		t.Fatalf("ListAllAlerts firing-first: %+v", alerts)
	}
}

func TestResetClearsEntities(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertService(model.Service{ID: "a", Name: "a", Health: model.HealthHealthy}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertOwner(model.Owner{ID: "o", Name: "O"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reset(); err != nil {
		t.Fatal(err)
	}
	counts, err := s.Counts()
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range counts {
		if v != 0 {
			t.Fatalf("after Reset %s=%d, want 0", k, v)
		}
	}
	v, err := s.UserVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Fatalf("schema version lost after Reset: %d", v)
	}
}

func TestSchemaUserVersion(t *testing.T) {
	s := newTestStore(t)
	v, err := s.UserVersion()
	if err != nil {
		t.Fatal(err)
	}
	if v != SchemaVersion {
		t.Fatalf("user_version = %d, want %d", v, SchemaVersion)
	}
	if s.Path() == "" {
		t.Fatal("Path() empty")
	}
	if strings.Contains(s.Path(), "?") {
		t.Fatalf("Path must stay a filesystem path, got %q", s.Path())
	}
}

func TestOpenUsesDefensiveMode(t *testing.T) {
	s := newTestStore(t)
	var before, after int64
	if err := s.db.QueryRow(`PRAGMA schema_version`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA schema_version=999`); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`PRAGMA schema_version`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("schema_version changed %d -> %d; _defensive=1 should no-op that write", before, after)
	}
	ver, err := s.UserVersion()
	if err != nil {
		t.Fatal(err)
	}
	if ver != SchemaVersion {
		t.Fatalf("user_version=%d want %d (migrations must still work under defensive)", ver, SchemaVersion)
	}
}

func TestUpsertIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	svc := model.Service{ID: "checkout", Name: "checkout", Aliases: []string{"checkout-api"}, Health: model.HealthDegraded}
	for i := 0; i < 3; i++ {
		if err := s.UpsertService(svc); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	all, err := s.ListServices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("want 1 service after repeated upsert, got %d", len(all))
	}
	if all[0].Health != model.HealthDegraded || len(all[0].Aliases) != 1 {
		t.Fatalf("round-trip mismatch: %+v", all[0])
	}
}

func TestGetServiceByNameOrAlias(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertService(model.Service{ID: "checkout", Name: "checkout", Aliases: []string{"checkout-api", "checkout-service"}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"checkout", "CHECKOUT-API", "checkout-service"} {
		got, err := s.GetServiceByNameOrAlias(q)
		if err != nil {
			t.Fatalf("resolve %q: %v", q, err)
		}
		if got.ID != "checkout" {
			t.Fatalf("resolve %q => %q", q, got.ID)
		}
	}
	if _, err := s.GetServiceByNameOrAlias("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestGetServiceByNameOrAliasAmbiguous(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpsertService(model.Service{ID: "a", Name: "a", Aliases: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertService(model.Service{ID: "b", Name: "b", Aliases: []string{"shared"}}); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetServiceByNameOrAlias("shared")
	if err == nil || !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("want ErrAmbiguous, got %v", err)
	}
}

func TestListChangesSinceWindow(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.UpsertChange(model.Change{ID: "c1", ServiceID: "checkout", At: now.Add(-10 * time.Minute), Type: "deploy", Summary: "recent"}))
	must(s.UpsertChange(model.Change{ID: "c2", ServiceID: "checkout", At: now.Add(-2 * time.Hour), Type: "commit", Summary: "old"}))

	got, err := s.ListChanges("checkout", now.Add(-60*time.Minute))
	must(err)
	if len(got) != 1 || got[0].ID != "c1" {
		t.Fatalf("window filter failed: %+v", got)
	}
}

func TestListEvidenceSorted(t *testing.T) {
	s := newTestStore(t)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	_ = s.UpsertEvidence(model.Evidence{ID: "ev-2", At: now, Summary: "b"})
	_ = s.UpsertEvidence(model.Evidence{ID: "ev-1", At: now.Add(-time.Minute), Summary: "a"})
	got, err := s.ListEvidence([]string{"ev-2", "ev-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "ev-1" || got[1].ID != "ev-2" {
		t.Fatalf("evidence not sorted by (at,id): %+v", got)
	}
}

func TestDependenciesBothDirections(t *testing.T) {
	s := newTestStore(t)
	_ = s.UpsertDependency(model.Dependency{FromServiceID: "checkout", ToServiceID: "auth", Type: "http"})
	_ = s.UpsertDependency(model.Dependency{FromServiceID: "order", ToServiceID: "checkout", Type: "http"})
	got, err := s.ListDependencies("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 edges touching checkout, got %d: %+v", len(got), got)
	}
}
