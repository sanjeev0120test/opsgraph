package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestValidSince(t *testing.T) {
	if err := validSince(0); err != nil {
		t.Fatalf("zero: %v", err)
	}
	if err := validSince(time.Hour); err != nil {
		t.Fatalf("positive: %v", err)
	}
	if err := validSince(-time.Minute); err == nil {
		t.Fatal("expected error for negative since")
	}
}

func TestLiveHasIncidentSignalRequiresScrapeEvidence(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	cfg := config.Default()
	cfg.Connectors.Prometheus.Enabled = true
	cfg.Connectors.Prometheus.URL = "http://127.0.0.1:9090"
	if liveHasIncidentSignal(s, cfg) {
		t.Fatal("configured Prom URL alone must not count as a scrape")
	}
	if err := s.SetMeta("connector:prometheus", "ok"); err != nil {
		t.Fatal(err)
	}
	if !liveHasIncidentSignal(s, cfg) {
		t.Fatal("successful Prom scrape meta must count")
	}
	if !liveIsQuietConnectorOnly(s) {
		t.Fatal("meta-only scrape must be quiet (no rich signal)")
	}
}

func TestLiveIsQuietFalseWhenAlertsPresent(t *testing.T) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := s.SetMeta("connector:prometheus", "ok"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlert(model.Alert{
		ID: "a", ServiceID: "checkout", At: now, Name: "X", Status: "firing", Source: "prometheus",
	}); err != nil {
		t.Fatal(err)
	}
	if liveIsQuietConnectorOnly(s) {
		t.Fatal("alerts must count as rich signal")
	}
}

func TestWatchFatalLoad(t *testing.T) {
	if !watchFatalLoad(ErrEmptyStore) {
		t.Fatal("empty store must be fatal for watch")
	}
	if !watchFatalLoad(fmt.Errorf("%w: pass --fixture", ErrNoDataSource)) {
		t.Fatal("no data source must be fatal")
	}
	if !watchFatalLoad(fmt.Errorf("%w -1h (must be >= 0)", ErrInvalidSince)) {
		t.Fatal("invalid since must be fatal")
	}
	if !watchFatalLoad(fmt.Errorf("read config %q: no such file", "x.yaml")) {
		t.Fatal("config read errors must be fatal")
	}
	if watchFatalLoad(fmt.Errorf("temporary network blip")) {
		t.Fatal("transient errors must retry")
	}
}

func TestLiveConnectorsEnabledRequiresUsableConfig(t *testing.T) {
	if liveConnectorsEnabled(nil) {
		t.Fatal("nil config")
	}
	cfg := config.Default()
	if liveConnectorsEnabled(cfg) {
		t.Fatal("defaults should not prefer live (k8s off, no URLs)")
	}
	cfg.Connectors.Kubernetes.Enabled = true
	if liveConnectorsEnabled(cfg) {
		t.Fatal("k8s enabled without snapshot must not prefer live")
	}
	cfg.Connectors.Kubernetes.Snapshot = "k8s/deployments.yaml"
	if !liveConnectorsEnabled(cfg) {
		t.Fatal("k8s with snapshot should prefer live")
	}
	cfg2 := config.Default()
	cfg2.Connectors.Prometheus.Enabled = true
	if liveConnectorsEnabled(cfg2) {
		t.Fatal("prom enabled without url must not prefer live")
	}
	cfg2.Connectors.Prometheus.URL = "http://127.0.0.1:9090"
	if !liveConnectorsEnabled(cfg2) {
		t.Fatal("prom with url should prefer live")
	}
	cfg3 := config.Default()
	cfg3.Connectors.Plugins = []config.PluginConnector{{
		Name: "deploys", Command: []string{"./bin/deploys"}, Enabled: true,
	}}
	if !liveConnectorsEnabled(cfg3) {
		t.Fatal("an enabled plugin should prefer live so ask re-runs it")
	}
	cfg3.Connectors.Plugins[0].Enabled = false
	if liveConnectorsEnabled(cfg3) {
		t.Fatal("a disabled plugin must not prefer live")
	}
}

func TestResolveDataDirConfigRelative(t *testing.T) {
	t.Setenv("OPSGRAPH_DATA_DIR", "")
	cfg := config.Default()
	cfg.DataDir = "mystore"
	got := resolveDataDir("", cfg, filepath.Join("cfg", "dir"))
	want := filepath.Clean(filepath.Join("cfg", "dir", "mystore"))
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	abs := "/var/opsgraph"
	if runtime.GOOS == "windows" {
		abs = `C:\opsgraph-data`
	}
	cfg.DataDir = abs
	if got := resolveDataDir("", cfg, "ignored"); got != abs {
		t.Fatalf("abs data_dir: got %q", got)
	}
	if got := resolveDataDir("flag-data", cfg, "cfg"); got != "flag-data" {
		t.Fatalf("flag wins: got %q", got)
	}
	t.Setenv("OPSGRAPH_DATA_DIR", "env-data")
	if got := resolveDataDir("", cfg, "cfg"); got != "env-data" {
		t.Fatalf("env wins over config: got %q", got)
	}
	if got := resolveDataDir("flag-data", cfg, "cfg"); got != "flag-data" {
		t.Fatalf("flag still wins over env: got %q", got)
	}
}
