package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load empty: %v", err)
	}
	if cfg.DataDir != DefaultDataDir {
		t.Fatalf("default data dir = %q", cfg.DataDir)
	}
	if cfg.Since() != time.Hour {
		t.Fatalf("default since = %v", cfg.Since())
	}
}

func TestLoadExplicitMissingIsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("expected error for explicit missing config")
	}
}

func TestLoadMalformedIsError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(p, []byte("version: [this is not: valid"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestLoadImplicitOpsgraphYAML(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	content := `version: 1
default_since: 15m
services:
  checkout:
    aliases: ["checkout-api"]
`
	if err := os.WriteFile(".opsgraph.yaml", []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Since() != 15*time.Minute {
		t.Fatalf("since = %v, want 15m", cfg.Since())
	}
	if _, ok := cfg.Services["checkout"]; !ok {
		t.Fatalf("expected checkout service from .opsgraph.yaml: %+v", cfg.Services)
	}
}

func TestLoadDefaultOpsgraphYAML(t *testing.T) {
	dir := t.TempDir()
	cwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	_ = os.Chdir(dir)

	if err := os.WriteFile(".opsgraph.yaml", []byte("default_since: 10m\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Since() != 10*time.Minute {
		t.Fatalf("since = %v, want 10m from .opsgraph.yaml", cfg.Since())
	}
}

func TestLoadRejectsInvalidDefaultSince(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad-since.yaml")
	if err := os.WriteFile(p, []byte("default_since: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("expected invalid default_since error")
	}
}

func TestLoadValid(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ok.yaml")
	content := `version: 1
default_since: 30m
services:
  checkout:
    aliases: ["checkout-api"]
    owner: payments
    depends_on: ["auth"]
owners:
  payments:
    name: Payments Team
`
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Since() != 30*time.Minute {
		t.Fatalf("since = %v", cfg.Since())
	}
	svc, ok := cfg.Services["checkout"]
	if !ok || svc.Owner != "payments" || len(svc.DependsOn) != 1 {
		t.Fatalf("service parse: %+v", cfg.Services)
	}
	if cfg.Owners["payments"].Name != "Payments Team" {
		t.Fatalf("owner parse: %+v", cfg.Owners)
	}
	if cfg.AI.Model == "" || cfg.AI.OllamaURL == "" {
		t.Fatalf("ai defaults missing: %+v", cfg.AI)
	}
}

func TestLoadRejectsPluginWithoutName(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plugin.yaml")
	body := "connectors:\n  plugins:\n    - command: [echo]\n      enabled: true\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("plugin without a name must fail at load")
	}
}

func TestLoadRejectsEnabledPluginWithoutCommand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plugin.yaml")
	body := "connectors:\n  plugins:\n    - name: deploys\n      enabled: true\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("enabled plugin without a command must fail at load")
	}
}

func TestLoadAcceptsDisabledPluginWithoutCommand(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plugin.yaml")
	body := "connectors:\n  plugins:\n    - name: deploys\n      enabled: false\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("disabled plugin is a stub, not an error: %v", err)
	}
	if len(cfg.Connectors.Plugins) != 1 || cfg.Connectors.Plugins[0].Name != "deploys" {
		t.Fatalf("plugins: %+v", cfg.Connectors.Plugins)
	}
}

func TestPluginTimeoutDefault(t *testing.T) {
	p := PluginConnector{}
	if p.PluginTimeout() != DefaultPluginTimeout {
		t.Fatalf("default = %v", p.PluginTimeout())
	}
	p.Timeout = "5s"
	if p.PluginTimeout() != 5*time.Second {
		t.Fatalf("override = %v", p.PluginTimeout())
	}
}

func TestAITimeoutDefaultsAndOverride(t *testing.T) {
	cfg := Default()
	if cfg.AITimeout() != 20*time.Second {
		t.Fatalf("default AITimeout=%v", cfg.AITimeout())
	}
	cfg.AI.Timeout = "5s"
	if cfg.AITimeout() != 5*time.Second {
		t.Fatalf("override AITimeout=%v", cfg.AITimeout())
	}
	cfg.AI.Timeout = "nope"
	if cfg.AITimeout() != 20*time.Second {
		t.Fatalf("invalid timeout should fall back, got %v", cfg.AITimeout())
	}
}
