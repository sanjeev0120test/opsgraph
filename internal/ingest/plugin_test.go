package ingest

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// The fake plugin is this test binary re-executed with a marker env var, so the
// tests need no shell and run identically on Windows, macOS and Linux.
const pluginHelperEnv = "OPSGRAPH_TEST_PLUGIN_MODE"

func TestMain(m *testing.M) {
	switch os.Getenv(pluginHelperEnv) {
	case "":
		os.Exit(m.Run())
	case "emit":
		os.Stdout.WriteString(os.Getenv("OPSGRAPH_TEST_PLUGIN_STDOUT"))
		os.Exit(0)
	case "fail":
		os.Stderr.WriteString("plugin exploded: upstream 503\n")
		os.Exit(3)
	case "hang":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	default:
		os.Exit(99)
	}
}

// helperPlugin builds a plugin config that re-executes this test binary.
func helperPlugin(t *testing.T, name, mode, stdout string) config.PluginConnector {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(pluginHelperEnv, mode)
	t.Setenv("OPSGRAPH_TEST_PLUGIN_STDOUT", stdout)
	return config.PluginConnector{Name: name, Command: []string{self}, Enabled: true}
}

const deployPluginOutput = `services:
  - id: billing
    name: billing
    owner_id: payments
    health: degraded
owners:
  - id: payments
    name: Payments
    team: payments
    email: payments@example.com
changes:
  - id: deploy-9001
    service_id: billing
    at: 2026-07-31T11:40:00Z
    type: deploy
    summary: "release 9001 to billing"
    evidence_id: ev-deploy-9001
alerts:
  - id: alert-billing-1
    service_id: billing
    at: 2026-07-31T11:45:00Z
    severity: critical
    name: BillingErrorRate
    status: firing
    summary: "billing error rate above 5%"
dependencies:
  - from: billing
    to: ledger
`

func TestRunPluginsIngestsEntities(t *testing.T) {
	p := helperPlugin(t, "deploys", "emit", deployPluginOutput)
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	if err := RunPlugins(context.Background(), s, []config.PluginConnector{p}, ""); err != nil {
		t.Fatalf("RunPlugins: %v", err)
	}

	svc, err := s.GetService("billing")
	if err != nil {
		t.Fatalf("plugin service: %v", err)
	}
	if svc.Health != "degraded" {
		t.Fatalf("health = %q, want degraded", svc.Health)
	}
	// Rows must be attributable to the plugin that produced them.
	if !hasSource(svc.Sources, "plugin:deploys") {
		t.Fatalf("sources = %v, want plugin:deploys", svc.Sources)
	}
	changes, err := s.ListChanges("billing", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].ID != "deploy-9001" {
		t.Fatalf("changes: %+v", changes)
	}
	if changes[0].Source != "plugin:deploys" {
		t.Fatalf("change source = %q", changes[0].Source)
	}
	alerts, err := s.ListAlerts("billing", time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(alerts) != 1 || alerts[0].Name != "BillingErrorRate" {
		t.Fatalf("alerts: %+v", alerts)
	}
	// A dependency target with no service row is synthesized, as for fixtures.
	if _, err := s.GetService("ledger"); err != nil {
		t.Fatalf("dependency target not synthesized: %v", err)
	}
	owner, err := s.GetOwner("payments")
	if err != nil {
		t.Fatalf("owner: %v", err)
	}
	if owner.Team != "payments" {
		t.Fatalf("owner: %+v", owner)
	}
}

func TestRunPluginsSkipsDisabled(t *testing.T) {
	p := helperPlugin(t, "deploys", "emit", deployPluginOutput)
	p.Enabled = false
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := RunPlugins(context.Background(), s, []config.PluginConnector{p}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetService("billing"); err == nil {
		t.Fatal("disabled plugin must not run")
	}
}

func TestRunPluginsEmptyOutputIsNotAnError(t *testing.T) {
	p := helperPlugin(t, "quiet", "emit", "")
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := RunPlugins(context.Background(), s, []config.PluginConnector{p}, ""); err != nil {
		t.Fatalf("a plugin with nothing to report must succeed: %v", err)
	}
}

func TestRunPluginsFailureIsHardAndQuotesStderr(t *testing.T) {
	p := helperPlugin(t, "broken", "fail", "")
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	err = RunPlugins(context.Background(), s, []config.PluginConnector{p}, "")
	if err == nil {
		t.Fatal("a failing plugin must fail the run, not answer with missing signals")
	}
	if !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "upstream 503") {
		t.Fatalf("error should name the plugin and quote its stderr: %v", err)
	}
}

func TestRunPluginsTimesOut(t *testing.T) {
	p := helperPlugin(t, "slow", "hang", "")
	p.Timeout = "200ms"
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	start := time.Now()
	err = RunPlugins(context.Background(), s, []config.PluginConnector{p}, "")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("want timeout error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("timeout not enforced: took %s", elapsed)
	}
}

func TestRunPluginsRejectsUnknownSection(t *testing.T) {
	p := helperPlugin(t, "typo", "emit", "servicez:\n  - id: x\n")
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	err = RunPlugins(context.Background(), s, []config.PluginConnector{p}, "")
	if err == nil || !strings.Contains(err.Error(), "unknown section") {
		t.Fatalf("a typo must fail loudly, got %v", err)
	}
	if !strings.Contains(err.Error(), "services") {
		t.Fatalf("error should list the accepted sections: %v", err)
	}
}

func TestRunPluginsRejectsMalformedYAML(t *testing.T) {
	p := helperPlugin(t, "garbage", "emit", "services: [oops\n")
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := RunPlugins(context.Background(), s, []config.PluginConnector{p}, ""); err == nil {
		t.Fatal("malformed YAML must fail")
	}
}

func TestPluginSectionsExcludeKubernetesWorkloads(t *testing.T) {
	// k8s health carries scrape/prune semantics only a real snapshot should
	// trigger; plugins report health through services[].health instead.
	for _, key := range PluginSections() {
		if key == "deployments" || key == "events" {
			t.Fatalf("plugin sections must not include %q", key)
		}
	}
	if got := strings.Join(PluginSections(), ","); got != "alerts,changes,dependencies,owners,services" {
		t.Fatalf("sections = %q", got)
	}
}
