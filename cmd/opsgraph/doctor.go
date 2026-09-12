package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sanjeev0120test/opsgraph/fixtures"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/store"
	"github.com/sanjeev0120test/opsgraph/internal/version"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var configPath string
	var dataDir string
	var format string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Run offline health checks for the local opsgraph environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			type checkRow struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Detail string `json:"detail"`
			}
			type doctorOut struct {
				Checks []checkRow `json:"checks"`
				Pass   bool       `json:"pass"`
				OK     int        `json:"ok"`
				Warn   int        `json:"warn"`
				Fail   int        `json:"fail"`
			}
			out := doctorOut{Checks: []checkRow{}}
			record := func(name, status, detail string) {
				out.Checks = append(out.Checks, checkRow{Name: name, Status: status, Detail: detail})
				switch status {
				case "ok":
					out.OK++
				case "warn":
					out.Warn++
				case "fail":
					out.Fail++
				}
				if format != "json" {
					label := status
					if status == "fail" {
						label = "FAIL"
					} else if status == "warn" {
						label = "WARN"
					}
					cmd.Printf("[%s] %-18s %s\n", label, name, detail)
				}
			}
			check := func(name string, good bool, detail string) {
				if good {
					record(name, "ok", detail)
				} else {
					record(name, "fail", detail)
				}
			}
			warnf := func(name, detail string) {
				record(name, "warn", detail)
			}

			check("binary", true, fmt.Sprintf("opsgraph %s (%s/%s)", version.String(), runtime.GOOS, runtime.GOARCH))
			check("cwd", true, mustGetwd())
			if v := strings.TrimSpace(os.Getenv("OPSGRAPH_CONFIG")); v != "" {
				check("env_OPSGRAPH_CONFIG", true, v)
			}
			if v := strings.TrimSpace(os.Getenv("OPSGRAPH_DATA_DIR")); v != "" {
				check("env_OPSGRAPH_DATA_DIR", true, v)
			}
			if v := strings.TrimSpace(os.Getenv("OPSGRAPH_FIXTURE")); v != "" {
				check("env_OPSGRAPH_FIXTURE", true, v)
			}

			cfgPath := configPathOrEnv(configPath)
			cfg, err := config.Load(cfgPath)
			check("config_load", err == nil, errOrOK(err))
			if err == nil {
				if cfgPath == "" && resolveConfigPath("") == "" {
					if _, _, ok := detectCwdSource(); ok {
						check("cwd_source", true, "k8s dump or git layout in cwd (`opsgraph ask` works without a config file)")
					}
					warnf("config_file", "no .opsgraph.yaml (using built-in defaults; run `opsgraph init` to write one)")
				} else if eff := resolveConfigPath(cfgPath); eff != "" {
					check("config_file", true, eff)
				}
				cfgDir := "."
				if eff := resolveConfigPath(cfgPath); eff != "" {
					cfgDir = dirOf(eff)
				}
				dir := resolveDataDir(dataDir, cfg, cfgDir)
				db := filepath.Join(dir, "state.db")
				if pathExists(db) {
					s, openErr := store.Open(dir)
					if openErr != nil {
						check("persistent_db", false, openErr.Error())
					} else {
						ver, verErr := s.UserVersion()
						_ = s.Close()
						if verErr != nil {
							check("persistent_db", false, verErr.Error())
						} else if ver != store.SchemaVersion {
							check("persistent_db", false, fmt.Sprintf("%s schema v%d (want v%d)", db, ver, store.SchemaVersion))
						} else {
							check("persistent_db", true, fmt.Sprintf("%s (schema v%d)", db, ver))
						}
					}
				} else {
					warnf("persistent_db", "no state.db yet (run ingest)")
				}
				if !cfg.Connectors.Git.Enabled {
					warnf("git_repo", "connector disabled")
				} else {
					gitPath := resolveGitRepoPath(cfg.Connectors.Git.RepoPath, cfgDir)
					gitDir := filepath.Join(gitPath, ".git")
					gitOK := pathExists(gitDir)
					if gitOK {
						check("git_repo", true, gitPath)
						if !liveConnectorsEnabled(cfg) && pathExists(db) {
							warnf("git_refresh", "ask uses persisted store; run `opsgraph ingest` to pick up new commits")
						}
					} else if pathExists(gitPath) {
						warnf("git_repo", gitPath+" exists but has no .git (not a repository)")
					} else {
						warnf("git_repo", gitPath+" missing (optional)")
					}
				}
				if !cfg.Connectors.Kubernetes.Enabled {
					warnf("k8s_snapshot", "connector disabled")
				} else if cfg.Connectors.Kubernetes.Snapshot == "" {
					check("k8s_snapshot", false, "enabled but snapshot path is empty")
				} else {
					snap := cfg.Connectors.Kubernetes.Snapshot
					if !filepath.IsAbs(snap) {
						snap = filepath.Join(cfgDir, snap)
					}
					if pathExists(snap) {
						check("k8s_snapshot", true, snap)
					} else {
						check("k8s_snapshot", false, snap+" missing")
					}
				}
				if !cfg.Connectors.Prometheus.Enabled {
					warnf("prometheus", "connector disabled")
				} else if strings.TrimSpace(cfg.Connectors.Prometheus.URL) == "" {
					check("prometheus", false, "enabled but url is empty")
				} else if probeHTTP(cmd.Context(), cfg.Connectors.Prometheus.URL, "/api/v1/alerts") {
					check("prometheus", true, cfg.Connectors.Prometheus.URL)
				} else {
					check("prometheus", false, cfg.Connectors.Prometheus.URL+" unreachable")
				}
				if !cfg.Connectors.Alertmanager.Enabled {
					warnf("alertmanager", "connector disabled")
				} else if strings.TrimSpace(cfg.Connectors.Alertmanager.URL) == "" {
					check("alertmanager", false, "enabled but url is empty")
				} else if probeHTTP(cmd.Context(), cfg.Connectors.Alertmanager.URL, "/api/v2/alerts") {
					check("alertmanager", true, cfg.Connectors.Alertmanager.URL)
				} else {
					check("alertmanager", false, cfg.Connectors.Alertmanager.URL+" unreachable")
				}
				if n := pluginEnabledCount(cfg.Connectors.Plugins); n == 0 {
					if len(cfg.Connectors.Plugins) == 0 {
						warnf("plugins", "none configured (optional)")
					} else {
						warnf("plugins", "configured but all disabled")
					}
				} else {
					check("plugins", true, pluginEnabledNames(cfg.Connectors.Plugins))
				}
				if probeOllama(cmd.Context(), cfg.AI.OllamaURL) {
					check("ollama", true, cfg.AI.OllamaURL)
				} else {
					warnf("ollama", "unreachable at "+cfg.AI.OllamaURL+" (optional)")
				}
			}

			_, embErr := fixtures.CheckoutFS()
			check("embedded_fixture", embErr == nil, "fixtures.CheckoutFS")

			out.Pass = out.Fail == 0
			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), out); err != nil {
					return fail(2, "%v", err)
				}
			} else {
				cmd.Printf("\nsummary: %d ok, %d warn, %d fail\n", out.OK, out.Warn, out.Fail)
			}
			if out.Fail > 0 {
				if format != "json" {
					for _, c := range out.Checks {
						if c.Name == "k8s_snapshot" && c.Status == "fail" {
							cmd.Printf("next: %s\n     then opsgraph init --k8s k8s-snapshot.yaml --force && opsgraph ask\n", k8sDumpCmd)
							break
						}
					}
				}
				return fail(1, "doctor found %d failure(s)", out.Fail)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to .opsgraph.yaml (or OPSGRAPH_CONFIG)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "persistent store directory to validate (or OPSGRAPH_DATA_DIR)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func mustGetwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "(unknown)"
	}
	return wd
}

func errOrOK(err error) string {
	if err != nil {
		return err.Error()
	}
	return "ok"
}
