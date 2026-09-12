package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/fixtures"
	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// ErrEmptyStore means a persistent --data-dir has no services yet.
var ErrEmptyStore = errors.New("empty store")

// ErrNoDataSource means no fixture, config, or persisted store is available.
var ErrNoDataSource = errors.New("no data source")

// ErrInvalidSince means --since was negative or otherwise unusable.
var ErrInvalidSince = errors.New("invalid --since")

// nowUTC is the wall clock seed for the engine. It is truncated to the second
// because fixture clocks are, and packs golden their own `ask` output: a
// sub-second `generated_at` would never match its own replay.
func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// loadedStore holds an opened store plus the effective "now" and a cleanup func.
// source is one of: fixture | persisted | live (for status/operator clarity).
type loadedStore struct {
	store   *store.Store
	now     time.Time
	cleanup func()
	source  string
	root    string // materialized fixture directory (for expected/ goldens)
}

// storeFromFixtureDir ingests an on-disk fixture pack (directory or .zip/.opsgraph)
// into an ephemeral store.
func storeFromFixtureDir(path string) (*loadedStore, error) {
	dir, extra, err := ingest.MaterializeFixture(path)
	if err != nil {
		return nil, err
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		extra()
		return nil, err
	}
	now, err := ingest.IngestFixtureDir(s, dir)
	if err != nil {
		cleanup()
		extra()
		return nil, err
	}
	return &loadedStore{
		store: s, now: now, source: "fixture", root: dir,
		cleanup: func() { cleanup(); extra() },
	}, nil
}

// storeFromFixtureFS ingests a fixture pack filesystem (e.g. embedded) into an
// ephemeral store.
func storeFromFixtureFS(fsys fs.FS) (*loadedStore, error) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		return nil, err
	}
	now, err := ingest.IngestFixtureFS(s, fsys)
	if err != nil {
		cleanup()
		return nil, err
	}
	return &loadedStore{store: s, now: now, cleanup: cleanup, source: "fixture"}, nil
}

// embeddedCheckout returns an ephemeral store loaded from the built-in demo pack.
func embeddedCheckout() (*loadedStore, error) {
	fsys, err := fixtures.CheckoutFS()
	if err != nil {
		return nil, err
	}
	return storeFromFixtureFS(fsys)
}

// configPathOrEnv returns the explicit --config flag, else OPSGRAPH_CONFIG.
func configPathOrEnv(flag string) string {
	if strings.TrimSpace(flag) != "" {
		return strings.TrimSpace(flag)
	}
	return strings.TrimSpace(os.Getenv("OPSGRAPH_CONFIG"))
}

// resolveConfigPath returns the config path to use: the explicit flag /
// OPSGRAPH_CONFIG, or ./.opsgraph.yaml if present, or "".
func resolveConfigPath(configPath string) string {
	if p := configPathOrEnv(configPath); p != "" {
		return p
	}
	if _, err := os.Stat(".opsgraph.yaml"); err == nil {
		return ".opsgraph.yaml"
	}
	return ""
}

// resolveDataDir picks --data-dir, else OPSGRAPH_DATA_DIR, else config.data_dir
// resolved against the config file's directory, else the default under configDir/CWD.
func resolveDataDir(flag string, cfg *config.Config, configDir string) string {
	if flag != "" {
		return flag
	}
	if v := strings.TrimSpace(os.Getenv("OPSGRAPH_DATA_DIR")); v != "" {
		return v
	}
	dir := config.DefaultDataDir
	if cfg != nil && strings.TrimSpace(cfg.DataDir) != "" {
		dir = cfg.DataDir
	}
	if filepath.IsAbs(dir) {
		return dir
	}
	if configDir == "" {
		configDir = "."
	}
	return filepath.Clean(filepath.Join(configDir, dir))
}

// storeFromDataDir opens the persistent store under dataDir (no re-ingest).
// When ingest persisted as_of (fixture clock), that wins over wall clock so
// ask/verify match --fixture determinism.
func storeFromDataDir(dataDir string, now time.Time) (*loadedStore, error) {
	s, err := store.Open(dataDir)
	if err != nil {
		return nil, err
	}
	if asOf, ok, err := s.AsOf(); err != nil {
		_ = s.Close()
		return nil, err
	} else if ok {
		now = asOf
	}
	return &loadedStore{store: s, now: now, cleanup: func() { _ = s.Close() }, source: "persisted"}, nil
}

// storeFromConfig builds an ephemeral store from the live connectors described
// in cfg (seeded services/owners/runbooks + local git + k8s snapshot).
func storeFromConfig(ctx context.Context, cfg *config.Config, configDir string, since time.Duration, now time.Time) (*loadedStore, error) {
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		return nil, err
	}
	lookback := ask.ChangeLookback(since)
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ingest.LiveIngest(ctx, s, cfg, configDir, now.Add(-lookback), now); err != nil {
		cleanup()
		return nil, err
	}
	return &loadedStore{store: s, now: now, cleanup: cleanup, source: "live"}, nil
}

// liveConnectorsEnabled reports whether config can scrape *usable* fresh
// incident signals. Git-alone is excluded. Enabled-but-empty k8s/prom/AM
// must not displace a populated state.db with an empty ephemeral scrape.
func liveConnectorsEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	c := cfg.Connectors
	if c.Kubernetes.Enabled && strings.TrimSpace(c.Kubernetes.Snapshot) != "" {
		return true
	}
	if c.Prometheus.Enabled && strings.TrimSpace(c.Prometheus.URL) != "" {
		return true
	}
	if c.Alertmanager.Enabled && strings.TrimSpace(c.Alertmanager.URL) != "" {
		return true
	}
	return false
}

// loadAskStore resolves fixture / data-dir / live-config into a store.
func loadAskStore(ctx context.Context, fixture, configPath, dataDirFlag string, cfg *config.Config, since time.Duration) (*loadedStore, error) {
	if fixture != "" {
		return storeFromFixtureDir(fixture)
	}
	if dataDirFlag != "" {
		ls, err := storeFromDataDir(dataDirFlag, nowUTC())
		if err != nil {
			return nil, err
		}
		counts, err := ls.store.Counts()
		if err != nil {
			ls.cleanup()
			return nil, err
		}
		if counts["services"] == 0 {
			ls.cleanup()
			return nil, fmt.Errorf("%w at %s: run `opsgraph ingest` first or pass `--fixture`", ErrEmptyStore, dataDirFlag)
		}
		return ls, nil
	}
	effPath := resolveConfigPath(configPath)
	configDir := "."
	if effPath != "" {
		configDir = dirOf(effPath)
	}
	dir := resolveDataDir("", cfg, configDir)
	fallbackPersisted := func(reason error) (*loadedStore, error) {
		if counts, peekErr := peekCounts(dir); peekErr == nil && counts["services"] > 0 {
			fmt.Fprintf(os.Stderr, "warning: live connectors %v; using persisted store at %s\n", reason, dir)
			return storeFromDataDir(dir, nowUTC())
		}
		return nil, reason
	}
	// Live connectors beat a stale state.db so ask/why/watch see fresh signals
	// when a config is present. Explicit --data-dir still forces the store.
	if effPath != "" && liveConnectorsEnabled(cfg) {
		ls, err := storeFromConfig(ctx, cfg, configDir, since, nowUTC())
		if err != nil {
			return fallbackPersisted(err)
		}
		counts, cerr := ls.store.Counts()
		if cerr != nil {
			ls.cleanup()
			return nil, cerr
		}
		// Enabled-but-empty live must not displace a populated state.db.
		if counts["services"] == 0 {
			ls.cleanup()
			if fb, fbErr := fallbackPersisted(fmt.Errorf("returned no services")); fbErr == nil {
				return fb, nil
			}
			return nil, fmt.Errorf("%w: live connectors returned no services", ErrEmptyStore)
		}
		// Config seed alone (empty k8s dir / no Prom-AM) is not a usable scrape.
		if !liveHasIncidentSignal(ls.store, cfg) {
			ls.cleanup()
			if fb, fbErr := fallbackPersisted(fmt.Errorf("returned no incident signals")); fbErr == nil {
				return fb, nil
			}
			return nil, fmt.Errorf("%w: live connectors returned no incident signals", ErrEmptyStore)
		}
		// Quiet Prom/AM (meta ok, no rows) must not hide a richer persisted incident.
		if liveIsQuietConnectorOnly(ls.store) {
			if fb, fbErr := fallbackPersisted(fmt.Errorf("returned quiet Prom/AM scrape")); fbErr == nil {
				ls.cleanup()
				return fb, nil
			}
		}
		return ls, nil
	}
	if counts, err := peekCounts(dir); err == nil {
		if counts["services"] > 0 {
			return storeFromDataDir(dir, nowUTC())
		}
		return nil, fmt.Errorf("%w at %s: run `opsgraph ingest` first or pass `--fixture`", ErrEmptyStore, dir)
	}
	if effPath == "" {
		if auto, autoDir, ok := detectCwdSource(); ok {
			ls, err := storeFromConfig(ctx, auto, autoDir, since, nowUTC())
			if err == nil {
				counts, cerr := ls.store.Counts()
				if cerr != nil {
					ls.cleanup()
					return nil, cerr
				}
				if counts["services"] > 0 {
					return ls, nil
				}
				ls.cleanup()
			}
		}
		return nil, fmt.Errorf("%w: pass --fixture <pack>, drop a k8s-snapshot.yaml here, run `opsgraph ingest`, or add a .opsgraph.yaml", ErrNoDataSource)
	}
	ls, err := storeFromConfig(ctx, cfg, configDir, since, nowUTC())
	if err != nil {
		return nil, err
	}
	counts, cerr := ls.store.Counts()
	if cerr != nil {
		ls.cleanup()
		return nil, cerr
	}
	if counts["services"] == 0 {
		ls.cleanup()
		return nil, fmt.Errorf("%w: live connectors returned no services", ErrEmptyStore)
	}
	return ls, nil
}

// liveIsQuietConnectorOnly reports a successful Prom/AM scrape that left no
// richer incident rows. Git-only commits still count as a live signal so we do
// not discard a fresh scrape for a stale state.db.
func liveIsQuietConnectorOnly(s *store.Store) bool {
	promOK, _, _ := s.GetMeta("connector:prometheus")
	amOK, _, _ := s.GetMeta("connector:alertmanager")
	if promOK != "ok" && amOK != "ok" {
		return false
	}
	if liveHasRichSignal(s) {
		return false
	}
	// Fresh git commits in this scrape are still useful on-call signal.
	if counts, err := s.Counts(); err == nil && counts["changes"] > 0 {
		if changes, cerr := s.ListAllChanges(); cerr == nil {
			for _, c := range changes {
				if c.Source == "git" {
					return false
				}
			}
		}
	}
	return true
}

// liveHasRichSignal reports k8s/helm/fixture/alert rows (not merely Prom meta).
func liveHasRichSignal(s *store.Store) bool {
	counts, err := s.Counts()
	if err == nil && counts["alerts"] > 0 {
		return true
	}
	if err == nil && counts["evidence"] > 0 {
		if evs, cerr := s.ListAllEvidence(); cerr == nil {
			for _, e := range evs {
				switch e.Source {
				case "kubernetes", "prometheus", "alertmanager", "helm", "fixture":
					return true
				}
			}
		}
	}
	if err == nil && counts["changes"] > 0 {
		if changes, cerr := s.ListAllChanges(); cerr == nil {
			for _, c := range changes {
				switch c.Source {
				case "kubernetes", "prometheus", "alertmanager", "helm", "fixture":
					return true
				}
			}
		}
	}
	svcs, err := s.ListServices()
	if err != nil {
		return false
	}
	for _, svc := range svcs {
		for _, src := range svc.Sources {
			switch src {
			case "kubernetes", "prometheus", "alertmanager", "helm", "fixture":
				return true
			}
		}
	}
	return false
}

// liveHasIncidentSignal reports whether a live scrape produced usable incident
// data beyond config seed. Successful Prom/AM scrapes set connector meta even
// with zero alerts; k8s/git/helm must leave connector sources or rows. Merely
// having a Prom/AM URL in config is not enough (avoids displacing state.db).
func liveHasIncidentSignal(s *store.Store, _ *config.Config) bool {
	if v, ok, err := s.GetMeta("connector:prometheus"); err == nil && ok && v == "ok" {
		return true
	}
	if v, ok, err := s.GetMeta("connector:alertmanager"); err == nil && ok && v == "ok" {
		return true
	}
	return liveHasRichSignal(s)
}

// requireArg returns exit 2 when a positional argument is blank.
func requireArg(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return fail(2, "%s is required", name)
	}
	return nil
}

func peekCounts(dataDir string) (map[string]int, error) {
	dbPath := filepath.Join(dataDir, "state.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}
	s, err := store.Open(dataDir)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.Counts()
}

func validFormat(f string) error {
	if f != "table" && f != "json" {
		return fmt.Errorf("invalid --format %q (want table or json)", f)
	}
	return nil
}

// validSince rejects negative lookback windows (a negative duration would
// invert the cutoff and silently drop all history).
func validSince(d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("%w %s (must be >= 0)", ErrInvalidSince, d)
	}
	return nil
}
