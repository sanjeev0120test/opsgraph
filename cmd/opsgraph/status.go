package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	var (
		fixture    string
		configPath string
		dataDir    string
		format     string
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show connector configuration and ingested data counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			var err error
			fixture, err = resolveFixtureExclusive(fixture, dataDir)
			if err != nil {
				return err
			}
			cfgPath := configPathOrEnv(configPath)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fail(2, "%v", err)
			}
			effPath := resolveConfigPath(cfgPath)
			configDir := "."
			if effPath != "" {
				configDir = dirOf(effPath)
			}
			dir := resolveDataDir(dataDir, cfg, configDir)
			dbPath := filepath.Join(dir, "state.db")

			gitPath := resolveGitRepoPath(cfg.Connectors.Git.RepoPath, configDir)
			gitOK := pathExists(filepath.Join(gitPath, ".git"))
			k8sSnap := cfg.Connectors.Kubernetes.Snapshot
			k8sExists := false
			if cfg.Connectors.Kubernetes.Enabled && strings.TrimSpace(k8sSnap) != "" {
				snapPath := k8sSnap
				if !filepath.IsAbs(snapPath) {
					snapPath = filepath.Join(configDir, snapPath)
				}
				k8sExists = pathExists(snapPath)
			}
			promOK := false
			if cfg.Connectors.Prometheus.Enabled {
				promOK = probeHTTP(cmd.Context(), cfg.Connectors.Prometheus.URL, "/api/v1/alerts")
			}
			amOK := false
			if cfg.Connectors.Alertmanager.Enabled {
				amOK = probeHTTP(cmd.Context(), cfg.Connectors.Alertmanager.URL, "/api/v2/alerts")
			}
			ollamaOK := probeOllama(cmd.Context(), cfg.AI.OllamaURL)

			type connectorStatus struct {
				Enabled   bool   `json:"enabled"`
				Detail    string `json:"detail,omitempty"`
				Reachable *bool  `json:"reachable,omitempty"`
				Exists    *bool  `json:"exists,omitempty"`
				HasGit    *bool  `json:"has_git,omitempty"`
			}
			boolPtr := func(b bool) *bool { return &b }
			type statusOut struct {
				OK           bool                       `json:"ok"`
				HasData      bool                       `json:"has_data"`
				DataDir      string                     `json:"data_dir"`
				DB           string                     `json:"db"`
				Connectors   map[string]connectorStatus `json:"connectors"`
				AI           map[string]any             `json:"ai"`
				ActiveSource string                     `json:"active_source,omitempty"`
				SourceNote   string                     `json:"source_note,omitempty"`
				Schema       int                        `json:"schema_version,omitempty"`
				Counts       map[string]int             `json:"counts,omitempty"`
				Services     []map[string]string        `json:"services,omitempty"`
			}
			out := statusOut{
				OK:      false,
				HasData: false,
				DataDir: dir,
				DB:      dbPath,
				Connectors: map[string]connectorStatus{
					"fixtures": {Enabled: cfg.Connectors.Fixtures.Enabled},
					"git": {
						Enabled: cfg.Connectors.Git.Enabled,
						Detail:  gitPath,
						HasGit:  boolPtr(gitOK),
					},
					"kubernetes": {
						Enabled: cfg.Connectors.Kubernetes.Enabled,
						Detail:  k8sSnap,
						Exists:  boolPtr(k8sExists),
					},
					"prometheus": {
						Enabled:   cfg.Connectors.Prometheus.Enabled,
						Detail:    cfg.Connectors.Prometheus.URL,
						Reachable: boolPtr(promOK),
					},
					"alertmanager": {
						Enabled:   cfg.Connectors.Alertmanager.Enabled,
						Detail:    cfg.Connectors.Alertmanager.URL,
						Reachable: boolPtr(amOK),
					},
					"plugins": {
						Enabled: pluginEnabledCount(cfg.Connectors.Plugins) > 0,
						Detail:  pluginEnabledNames(cfg.Connectors.Plugins),
					},
				},
				AI: map[string]any{
					"enabled":   cfg.AI.Enabled,
					"model":     cfg.AI.Model,
					"embed":     cfg.AI.EmbedModel,
					"url":       cfg.AI.OllamaURL,
					"reachable": ollamaOK,
				},
			}

			if format != "json" {
				cmd.Println("STORE")
				cmd.Printf("  data_dir: %s\n", dir)
				cmd.Printf("  db:       %s\n", dbPath)

				cmd.Println("CONNECTORS")
				cmd.Printf("  fixtures:     %v\n", cfg.Connectors.Fixtures.Enabled)
				cmd.Printf("  git:          %v (repo %q has_git=%v)\n", cfg.Connectors.Git.Enabled, gitPath, gitOK)
				if cfg.Connectors.Git.Enabled && !liveConnectorsEnabled(cfg) {
					cmd.Println("  git_note:     ask/who/watch use persisted store; run `opsgraph ingest` to refresh git changes")
				}
				if cfg.Connectors.Kubernetes.Enabled && strings.TrimSpace(k8sSnap) != "" {
					cmd.Printf("  kubernetes:   %v (snapshot %q exists=%v)\n", cfg.Connectors.Kubernetes.Enabled, k8sSnap, k8sExists)
				} else {
					cmd.Printf("  kubernetes:   %v (snapshot %q)\n", cfg.Connectors.Kubernetes.Enabled, k8sSnap)
				}
				cmd.Printf("  prometheus:   %v (url %q", cfg.Connectors.Prometheus.Enabled, cfg.Connectors.Prometheus.URL)
				if cfg.Connectors.Prometheus.Enabled {
					cmd.Printf(" reachable=%v", promOK)
				}
				cmd.Printf(")\n")
				cmd.Printf("  alertmanager: %v (url %q", cfg.Connectors.Alertmanager.Enabled, cfg.Connectors.Alertmanager.URL)
				if cfg.Connectors.Alertmanager.Enabled {
					cmd.Printf(" reachable=%v", amOK)
				}
				cmd.Printf(")\n")
				if n := pluginEnabledCount(cfg.Connectors.Plugins); n > 0 {
					cmd.Printf("  plugins:      %d enabled (%s)\n", n, pluginEnabledNames(cfg.Connectors.Plugins))
				} else {
					cmd.Printf("  plugins:      0 enabled\n")
				}
				cmd.Printf("AI\n  enabled: %v  model: %s  embed: %s  url: %s  reachable: %v\n",
					cfg.AI.Enabled, cfg.AI.Model, cfg.AI.EmbedModel, cfg.AI.OllamaURL, ollamaOK)
			}

			ls, err := loadAskStore(cmd.Context(), fixture, cfgPath, dataDir, cfg, cfg.Since())
			if err != nil {
				emptyish := errors.Is(err, ErrEmptyStore) || isNoDataSource(err)
				if emptyish && fixture == "" {
					if format == "json" {
						_ = output.JSON(cmd.OutOrStdout(), out)
						return fail(1, "no ingested data")
					}
					if dataDir == "" {
						cmd.Println("\nNo data source (pass --fixture <pack>, run `opsgraph ingest`, or add .opsgraph.yaml) - showing config only.")
					}
					return fail(1, "no ingested data")
				}
				return failSource(err)
			}
			defer ls.cleanup()

			counts, err := ls.store.Counts()
			if err != nil {
				return fail(2, "%v", err)
			}
			ver, err := ls.store.UserVersion()
			if err != nil {
				return fail(2, "schema version: %v", err)
			}
			active := ls.source
			if active == "" {
				active = "unknown"
			}
			out.ActiveSource = active
			out.Schema = ver
			out.Counts = counts
			out.HasData = true
			out.OK = true
			switch active {
			case "live":
				out.SourceNote = "ephemeral live scrape (may differ from on-disk db above)"
			case "fixture":
				out.SourceNote = "fixture pack (ephemeral; not written to data_dir)"
			case "persisted":
				out.SourceNote = "reading on-disk state.db"
			}

			services, err := ls.store.ListServices()
			if err != nil {
				return fail(2, "%v", err)
			}
			if len(services) > 0 {
				out.Services = make([]map[string]string, 0, len(services))
				for _, s := range services {
					out.Services = append(out.Services, map[string]string{"id": s.ID, "health": s.Health})
				}
			}

			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), out)
			}

			cmd.Printf("\nACTIVE SOURCE  %s\n", active)
			if out.SourceNote != "" {
				cmd.Printf("  note: %s\n", out.SourceNote)
			}
			cmd.Printf("\nINGESTED (schema v%d)\n", ver)
			for _, k := range sortedKeys(counts) {
				cmd.Printf("  %-13s %d\n", k+":", counts[k])
			}
			if len(services) > 0 {
				cmd.Println("SERVICES")
				for _, s := range services {
					cmd.Printf("  %-16s %s\n", s.ID, s.Health)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "path to a fixture pack directory (or OPSGRAPH_FIXTURE)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to .opsgraph.yaml (or OPSGRAPH_CONFIG)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "persistent store directory (or OPSGRAPH_DATA_DIR)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dirOf(p string) string {
	d := filepath.Dir(p)
	if d == "" {
		return "."
	}
	return d
}

func pathExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
}

func probeOllama(ctx context.Context, url string) bool {
	return probeHTTP(ctx, url, "/api/tags")
}

// probeHTTP GETs baseURL+path with a short timeout. Empty URL is unreachable.
func probeHTTP(ctx context.Context, baseURL, path string) bool {
	if strings.TrimSpace(baseURL) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, trimSlash(baseURL)+path, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "opsgraph/1.0")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Cap body so a misconfigured host cannot stall conn reuse / memory.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	// Ollama historically omits Content-Type; Prom/AM should speak JSON.
	if ct == "" {
		return strings.Contains(path, "/api/tags")
	}
	return strings.Contains(ct, "json") || strings.Contains(ct, "javascript")
}

// resolveGitRepoPath mirrors LiveIngest: relative repo_path is config-dir based.
func resolveGitRepoPath(repoPath, configDir string) string {
	if repoPath == "" {
		repoPath = "."
	}
	if filepath.IsAbs(repoPath) {
		return repoPath
	}
	if configDir == "" {
		configDir = "."
	}
	return filepath.Join(configDir, repoPath)
}

func isNoDataSource(err error) bool {
	return errors.Is(err, ErrNoDataSource) ||
		(err != nil && strings.Contains(err.Error(), "no data source"))
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
