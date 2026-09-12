package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	// Tests run with CWD = package dir (cmd/opsgraph); fixtures live at repo root.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "fixtures", "incident_checkout")); err != nil {
		t.Fatalf("repo root %q missing fixtures: %v", root, err)
	}
	return root
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "fixtures", "incident_checkout")
}

func runRoot(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	root := newRootCmd()
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs(rewriteRootArgs(root, args))
	err := root.Execute()
	if err != nil {
		fmt.Fprintln(&errBuf, "opsgraph:", err)
	}
	return out.String(), errBuf.String(), exitCodeFor(err)
}

func TestCLIExitAskCheckoutOK(t *testing.T) {
	_, _, code := runRoot(t, "ask", "checkout", "--fixture", fixtureDir(t), "--format", "json")
	if code != 0 {
		t.Fatalf("ask checkout exit = %d, want 0 (stale runbook must not fail ask)", code)
	}
}

func TestCLIExitAskUnknownService(t *testing.T) {
	_, errOut, code := runRoot(t, "ask", "nosuch", "--fixture", fixtureDir(t))
	if code != 1 {
		t.Fatalf("ask nosuch exit = %d, want 1", code)
	}
	if !strings.Contains(errOut, "try: opsgraph services") {
		t.Fatalf("unknown service must hint services:\n%s", errOut)
	}
}

func TestCLIAskDidYouMean(t *testing.T) {
	_, errOut, code := runRoot(t, "ask", "chekout", "--fixture", fixtureDir(t))
	if code != 1 {
		t.Fatalf("typo exit = %d, want 1 stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "did you mean") || !strings.Contains(errOut, "checkout") {
		t.Fatalf("typo must suggest checkout:\n%s", errOut)
	}
}

func TestCLIExitAskNoSource(t *testing.T) {
	// No --fixture and no .opsgraph.yaml in the test CWD (cmd/opsgraph).
	_, _, code := runRoot(t, "ask", "checkout")
	if code != 2 {
		t.Fatalf("ask without source exit = %d, want 2", code)
	}
}

func TestCLIExitVerifyStale(t *testing.T) {
	_, _, code := runRoot(t, "verify-runbook", "checkout", "--fixture", fixtureDir(t))
	if code != 1 {
		t.Fatalf("verify checkout exit = %d, want 1", code)
	}
}

func TestCLIExitVerifyPass(t *testing.T) {
	_, _, code := runRoot(t, "verify-runbook", "auth", "--fixture", fixtureDir(t))
	if code != 0 {
		t.Fatalf("verify auth exit = %d, want 0", code)
	}
}

func TestCLIExitVerifyMissing(t *testing.T) {
	_, _, code := runRoot(t, "verify-runbook", "order", "--fixture", fixtureDir(t))
	if code != 1 {
		t.Fatalf("verify order (missing runbook) exit = %d, want 1", code)
	}
}

func TestCLIExitIngestAndAskDataDir(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runRoot(t, "ingest", "--fixture", fixtureDir(t), "--data-dir", dir)
	if code != 0 {
		t.Fatalf("ingest exit = %d, want 0", code)
	}
	_, _, code = runRoot(t, "ask", "checkout", "--data-dir", dir, "--format", "json")
	if code != 0 {
		t.Fatalf("ask --data-dir exit = %d, want 0", code)
	}
}

func TestCLIExitEmptyDataDir(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runRoot(t, "ask", "checkout", "--data-dir", dir)
	if code != 1 {
		t.Fatalf("ask empty data-dir exit = %d, want 1", code)
	}
	_, _, code = runRoot(t, "status", "--data-dir", dir)
	if code != 1 {
		t.Fatalf("status empty data-dir exit = %d, want 1", code)
	}
	out, _, code := runRoot(t, "status", "--data-dir", dir, "--format", "json")
	if code != 1 {
		t.Fatalf("status empty json exit = %d, want 1", code)
	}
	if !strings.Contains(out, `"ok": false`) || !strings.Contains(out, `"has_data": false`) || !strings.Contains(out, `"connectors"`) {
		t.Fatalf("empty status json missing machine fields: %s", out)
	}
}

func TestCLIFixtureDataDirClockParity(t *testing.T) {
	fx := fixtureDir(t)
	dir := t.TempDir()
	outFix, _, code := runRoot(t, "ask", "checkout", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("ask --fixture exit = %d", code)
	}
	_, _, code = runRoot(t, "ingest", "--fixture", fx, "--data-dir", dir)
	if code != 0 {
		t.Fatalf("ingest exit = %d", code)
	}
	outDir, _, code := runRoot(t, "ask", "checkout", "--data-dir", dir, "--format", "json")
	if code != 0 {
		t.Fatalf("ask --data-dir exit = %d", code)
	}
	const want = `"generated_at": "2026-07-31T12:00:00Z"`
	if !strings.Contains(outFix, want) || !strings.Contains(outDir, want) {
		t.Fatalf("clock parity failed\nfixture=%s\ndata-dir=%s", outFix, outDir)
	}
}

func TestCLIRejectBadLimitAndWatchFlags(t *testing.T) {
	fx := fixtureDir(t)
	_, _, code := runRoot(t, "top", "--fixture", fx, "--limit", "0")
	if code != 0 {
		t.Fatalf("top --limit 0 exit = %d, want 0 (0 = unlimited)", code)
	}
	_, _, code = runRoot(t, "top", "--fixture", fx, "--limit", "-1")
	if code != 2 {
		t.Fatalf("top --limit -1 exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "changes", "--fixture", fx, "--limit", "-1")
	if code != 2 {
		t.Fatalf("changes --limit -1 exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "watch", "checkout", "--fixture", fx, "--timeout", "0")
	if code != 2 {
		t.Fatalf("watch --timeout 0 exit = %d, want 2", code)
	}
}

func TestCLIRejectConflictingSourceFlags(t *testing.T) {
	fx := fixtureDir(t)
	dir := t.TempDir()
	_, _, code := runRoot(t, "ask", "checkout", "--fixture", fx, "--data-dir", dir)
	if code != 2 {
		t.Fatalf("ask fixture+data-dir exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "status", "--fixture", fx, "--data-dir", dir)
	if code != 2 {
		t.Fatalf("status fixture+data-dir exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "verify-runbook", "checkout", "--fixture", fx, "--data-dir", dir)
	if code != 2 {
		t.Fatalf("verify-runbook fixture+data-dir exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "ingest", "--fixture", fx, "--merge", "--data-dir", dir)
	if code != 2 {
		t.Fatalf("ingest fixture+merge exit = %d, want 2", code)
	}
}

func TestCLIIngestReportsMode(t *testing.T) {
	fx := fixtureDir(t)
	dir := t.TempDir()
	out, _, code := runRoot(t, "ingest", "--fixture", fx, "--data-dir", dir)
	if code != 0 {
		t.Fatalf("ingest exit = %d", code)
	}
	if !strings.Contains(out, "mode fixture") {
		t.Fatalf("ingest should report mode fixture:\n%s", out)
	}
}

func TestCLIStatusDoctorIngestJSON(t *testing.T) {
	fx := fixtureDir(t)
	dir := t.TempDir()
	out, _, code := runRoot(t, "ingest", "--fixture", fx, "--data-dir", dir, "--format", "json")
	if code != 0 {
		t.Fatalf("ingest json exit = %d", code)
	}
	if !strings.Contains(out, `"mode": "fixture"`) || !strings.Contains(out, `"counts"`) {
		t.Fatalf("ingest json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "status", "--data-dir", dir, "--format", "json")
	if code != 0 {
		t.Fatalf("status json exit = %d", code)
	}
	if !strings.Contains(out, `"active_source"`) || !strings.Contains(out, `"connectors"`) {
		t.Fatalf("status json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "doctor", "--data-dir", dir, "--format", "json")
	if code != 0 {
		t.Fatalf("doctor json exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, `"checks"`) || !strings.Contains(out, `"ok"`) || !strings.Contains(out, `"pass": true`) {
		t.Fatalf("doctor json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "version", "--format", "json")
	if code != 0 {
		t.Fatalf("version json exit = %d", code)
	}
	if !strings.Contains(out, `"version"`) || !strings.Contains(out, `"commit"`) {
		t.Fatalf("version json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "graph", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("graph json exit = %d", code)
	}
	if !strings.Contains(out, `"nodes"`) || !strings.Contains(out, `"edges"`) {
		t.Fatalf("graph json missing fields: %s", out)
	}
}

func TestCLIServicesOwnersJSONEnvelope(t *testing.T) {
	fx := fixtureDir(t)
	out, _, code := runRoot(t, "services", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("services json exit = %d", code)
	}
	if !strings.Contains(out, `"services"`) || !strings.Contains(out, `"total"`) {
		t.Fatalf("services json missing envelope: %s", out)
	}
	out, _, code = runRoot(t, "owners", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("owners json exit = %d", code)
	}
	if !strings.Contains(out, `"owners"`) || !strings.Contains(out, `"total"`) {
		t.Fatalf("owners json missing envelope: %s", out)
	}
}

func TestCLIEnvFixtureAndWatchHealthy(t *testing.T) {
	fx := fixtureDir(t)
	t.Setenv("OPSGRAPH_FIXTURE", fx)
	out, _, code := runRoot(t, "ask", "checkout", "--format", "json")
	if code != 0 {
		t.Fatalf("ask via OPSGRAPH_FIXTURE exit = %d", code)
	}
	if !strings.Contains(out, "checkout") {
		t.Fatalf("ask env fixture missing checkout: %s", out)
	}
	_, _, code = runRoot(t, "watch", "order", "--fixture", fx, "--interval", "1ms", "--timeout", "2s")
	if code != 0 {
		t.Fatalf("watch healthy order exit = %d, want 0", code)
	}
}

func TestCLITimelineLimit(t *testing.T) {
	fx := fixtureDir(t)
	out, _, code := runRoot(t, "timeline", "checkout", "--fixture", fx, "--limit", "1")
	if code != 0 {
		t.Fatalf("timeline exit = %d", code)
	}
	if !strings.Contains(out, "... +") {
		t.Fatalf("timeline --limit should note truncated events:\n%s", out)
	}
	out, _, code = runRoot(t, "timeline", "checkout", "--fixture", fx, "--limit", "1", "--format", "json")
	if code != 0 {
		t.Fatalf("timeline json exit = %d", code)
	}
	if !strings.Contains(out, `"truncated": true`) || !strings.Contains(out, `"events"`) {
		t.Fatalf("timeline json should report truncated envelope:\n%s", out)
	}
}

func TestCLIHealthStrictAndWatchOnce(t *testing.T) {
	fx := fixtureDir(t)
	out, _, code := runRoot(t, "health", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("health json exit = %d", code)
	}
	if !strings.Contains(out, `"ok": false`) || !strings.Contains(out, `"counts"`) {
		t.Fatalf("health json missing ok/counts on hot fixture: %s", out)
	}
	table, _, code := runRoot(t, "health", "--fixture", fx)
	if code != 0 {
		t.Fatalf("health table exit = %d", code)
	}
	if !strings.Contains(table, "not_ok") {
		t.Fatalf("health table missing not_ok status:\n%s", table)
	}
	_, _, code = runRoot(t, "health", "--fixture", fx, "--strict")
	if code != 1 {
		t.Fatalf("health --strict on hot fixture exit = %d, want 1", code)
	}
	_, _, code = runRoot(t, "watch", "order", "--fixture", fx, "--once")
	if code != 0 {
		t.Fatalf("watch --once healthy exit = %d, want 0", code)
	}
	_, _, code = runRoot(t, "watch", "checkout", "--fixture", fx, "--once")
	if code != 1 {
		t.Fatalf("watch --once degraded exit = %d, want 1", code)
	}
	out, _, code = runRoot(t, "validate-fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("validate-fixture json exit = %d", code)
	}
	if !strings.Contains(out, `"ok": true`) || !strings.Contains(out, `"warnings"`) {
		t.Fatalf("validate-fixture json missing fields: %s", out)
	}
}

func TestCLIWhyHandoffJSON(t *testing.T) {
	fx := fixtureDir(t)
	out, _, code := runRoot(t, "why", "checkout", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("why json exit = %d", code)
	}
	if !strings.Contains(out, `"why"`) || !strings.Contains(out, `"health"`) || !strings.Contains(out, "checkout") {
		t.Fatalf("why json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "handoff", "checkout", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("handoff json exit = %d", code)
	}
	if !strings.Contains(out, `"note"`) || !strings.Contains(out, `"fingerprint"`) || !strings.Contains(out, `"score"`) {
		t.Fatalf("handoff json missing fields: %s", out)
	}
	out, _, code = runRoot(t, "explain", "checkout", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("explain json exit = %d", code)
	}
	if !strings.Contains(out, `"narrative"`) {
		t.Fatalf("explain json missing narrative: %s", out)
	}
	out, _, code = runRoot(t, "report", "checkout", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("report json exit = %d", code)
	}
	if !strings.Contains(out, `"markdown"`) {
		t.Fatalf("report json missing markdown: %s", out)
	}
}

func TestCLIImpactTreeUsesBranchChars(t *testing.T) {
	fx := fixtureDir(t)
	out, _, code := runRoot(t, "impact", "auth", "--fixture", fx)
	if code != 0 {
		t.Fatalf("impact exit = %d", code)
	}
	if !strings.Contains(out, "└─") && !strings.Contains(out, "├─") {
		t.Fatalf("impact tree missing branch chars:\n%s", out)
	}
}

func TestCLIRootHelpGroups(t *testing.T) {
	out, _, code := runRoot(t, "--help")
	if code != 0 {
		t.Fatalf("help exit = %d", code)
	}
	for _, want := range []string{"Core Incident Commands", "Fleet & Topology", "Signals & Evidence", "Ops & Tooling"} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing group %q:\n%s", want, out)
		}
	}
}

func TestCLICompareAndPathRejectSameService(t *testing.T) {
	fx := fixtureDir(t)
	_, _, code := runRoot(t, "compare", "checkout", "checkout-api", "--fixture", fx)
	if code != 2 {
		t.Fatalf("compare alias-collapse exit = %d, want 2", code)
	}
	_, _, code = runRoot(t, "path", "checkout", "checkout-api", "--fixture", fx)
	if code != 2 {
		t.Fatalf("path alias-collapse exit = %d, want 2", code)
	}
}

func TestCLILiveFailFallsBackToPersistedStore(t *testing.T) {
	fx := fixtureDir(t)
	root := t.TempDir()
	data := filepath.Join(root, "data")
	_, _, code := runRoot(t, "ingest", "--fixture", fx, "--data-dir", data)
	if code != 0 {
		t.Fatalf("ingest exit = %d", code)
	}
	cfgPath := filepath.Join(root, ".opsgraph.yaml")
	cfg := "version: 1\n" +
		"data_dir: data\n" +
		"connectors:\n" +
		"  kubernetes:\n" +
		"    enabled: true\n" +
		"    snapshot: missing-k8s-snapshot\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runRoot(t, "ask", "checkout", "--config", cfgPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask fallback exit = %d err=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, `"generated_at": "2026-07-31T12:00:00Z"`) {
		t.Fatalf("expected persisted fixture clock after live fail, got %s", out)
	}
}

func TestCLIPromFailFallsBackToPersistedStore(t *testing.T) {
	fx := fixtureDir(t)
	root := t.TempDir()
	data := filepath.Join(root, "data")
	_, _, code := runRoot(t, "ingest", "--fixture", fx, "--data-dir", data)
	if code != 0 {
		t.Fatalf("ingest exit = %d", code)
	}
	cfgPath := filepath.Join(root, ".opsgraph.yaml")
	// Unreachable Prom must hard-fail live and fall back (not empty alerts).
	cfg := "version: 1\n" +
		"data_dir: data\n" +
		"connectors:\n" +
		"  prometheus:\n" +
		"    enabled: true\n" +
		"    url: http://127.0.0.1:1\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runRoot(t, "ask", "checkout", "--config", cfgPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask prom-fallback exit = %d err=%q out=%q", code, errOut, out)
	}
	if !strings.Contains(out, `"CheckoutErrorRateHigh"`) {
		t.Fatalf("expected persisted alerts after prom fail, got %s (err=%s)", out, errOut)
	}
}

func TestCLIExitVerifyUnknownService(t *testing.T) {
	_, _, code := runRoot(t, "verify-runbook", "nosuch", "--fixture", fixtureDir(t))
	if code != 1 {
		t.Fatalf("verify nosuch exit = %d, want 1", code)
	}
}

func TestCLIExitVerifyRunbookFile(t *testing.T) {
	fx := fixtureDir(t)
	path := filepath.Join(fx, "runbooks", "auth.md")
	_, _, code := runRoot(t, "verify-runbook", path, "--fixture", fx)
	if code != 0 {
		t.Fatalf("verify file %s exit = %d, want 0", path, code)
	}
}

func TestCLIExitDemo(t *testing.T) {
	_, _, code := runRoot(t, "demo", "--format", "json")
	if code != 0 {
		t.Fatalf("demo exit = %d, want 0", code)
	}
}

func TestCLIExitStatusNoData(t *testing.T) {
	// No fixture/config/data-dir in package CWD → config-only path now exits 1.
	_, _, code := runRoot(t, "status")
	if code != 1 {
		t.Fatalf("status without data exit = %d, want 1", code)
	}
}

func TestCLIExitBlankServiceArg(t *testing.T) {
	_, _, code := runRoot(t, "ask", " ", "--fixture", fixtureDir(t))
	if code != 2 {
		t.Fatalf("ask blank service exit = %d, want 2", code)
	}
}

func TestCLIExitValidateMissingFiles(t *testing.T) {
	dir := t.TempDir()
	_, _, code := runRoot(t, "validate-fixture", dir)
	if code != 2 {
		t.Fatalf("validate missing files exit = %d, want 2", code)
	}
}

func TestCLIJSONEmptySlicesNotNull(t *testing.T) {
	out, _, code := runRoot(t, "blast", "auth", "--fixture", fixtureDir(t), "--format", "json")
	if code != 0 {
		t.Fatalf("blast exit = %d", code)
	}
	if strings.Contains(out, `"upstream": null`) || strings.Contains(out, `"downstream": null`) {
		t.Fatalf("blast JSON has null slices: %s", out)
	}
	out, _, code = runRoot(t, "ask", "auth", "--fixture", fixtureDir(t), "--format", "json")
	if code != 0 {
		t.Fatalf("ask exit = %d", code)
	}
	if strings.Contains(out, `"correlations": null`) {
		t.Fatalf("ask JSON has null correlations: %s", out)
	}
}
