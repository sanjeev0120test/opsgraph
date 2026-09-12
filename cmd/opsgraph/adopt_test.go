package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/config"
)

func TestAskHottestSelectsCheckout(t *testing.T) {
	fx := fixtureDir(t)
	out, errOut, code := runRoot(t, "ask", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(errOut, "auto-selected hottest service: checkout") {
		t.Fatalf("stderr missing auto-select:\n%s", errOut)
	}
	var payload struct {
		Service struct {
			ID string `json:"id"`
		} `json:"service"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if payload.Service.ID != "checkout" {
		t.Fatalf("hottest = %q, want checkout", payload.Service.ID)
	}
}

func TestInitWritesConfig(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, ".opsgraph.yaml")
	snap := filepath.Join(dir, "k8s-snapshot.yaml")
	stdout, stderr, code := runRoot(t, "init", "--out", out, "--k8s", snap, "--git", ".")
	if code != 0 {
		t.Fatalf("init exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "wrote") {
		t.Fatalf("stdout=%s", stdout)
	}
	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Connectors.Kubernetes.Enabled || cfg.Connectors.Kubernetes.Snapshot != snap {
		t.Fatalf("k8s connector: %+v", cfg.Connectors.Kubernetes)
	}
	if !cfg.Connectors.Git.Enabled || cfg.Connectors.Git.RepoPath != "." {
		t.Fatalf("git connector: %+v", cfg.Connectors.Git)
	}
	_, _, code = runRoot(t, "init", "--out", out, "--k8s", snap)
	if code != 1 {
		t.Fatalf("overwrite without --force exit=%d, want 1", code)
	}
	_, _, code = runRoot(t, "init", "--out", out, "--k8s", snap, "--force")
	if code != 0 {
		t.Fatalf("force overwrite exit=%d", code)
	}
}

func TestAskNativeKubectlSnapshot(t *testing.T) {
	snap := filepath.Join(repoRoot(t), "internal", "ingest", "testdata", "kubectl-list.yaml")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".opsgraph.yaml")
	body := "version: 1\nconnectors:\n  git:\n    enabled: false\n  kubernetes:\n    enabled: true\n    snapshot: " +
		strconvQuoteForTest(snap) + "\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runRoot(t, "ask", "checkout", "--config", cfgPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask checkout native exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"id": "checkout"`) {
		t.Fatalf("missing checkout:\n%s", out)
	}
	if !strings.Contains(out, `"health": "degraded"`) {
		t.Fatalf("checkout should be degraded from native readyReplicas:\n%s", out)
	}
}

func strconvQuoteForTest(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAskEventsK8sV1Dump(t *testing.T) {
	if testing.Short() {
		// Parser coverage lives in internal/ingest. Another OpenTemp+ask here
		// is what pushed Windows cmd/opsgraph over the 4m -short budget.
		t.Skip("skip extra SQLite walk under -short")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "k8s-snapshot.yaml"), []byte(eventsV1DumpForAsk), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("OPSGRAPH_FIXTURE", "")
	t.Setenv("OPSGRAPH_CONFIG", "")
	t.Setenv("OPSGRAPH_DATA_DIR", "")
	out, errOut, code := runRoot(t, "ask", "checkout", "--format", "json")
	if code != 0 {
		t.Fatalf("events.k8s.io/v1 ask exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"health": "degraded"`) {
		t.Fatalf("expected degraded checkout:\n%s", out)
	}
	if !strings.Contains(out, "HTTP 503") {
		t.Fatalf("events.k8s.io/v1 note never reached timeline:\n%s", out)
	}
}

const eventsV1DumpForAsk = `apiVersion: v1
kind: List
items:
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: checkout
      labels:
        app: checkout
      creationTimestamp: "2026-07-31T11:00:00Z"
    spec:
      replicas: 3
    status:
      readyReplicas: 1
  - apiVersion: events.k8s.io/v1
    kind: Event
    metadata:
      name: checkout.17f8c
      namespace: default
    regarding:
      kind: Pod
      name: checkout-7d9f8c4b5d-xk2n1
    reason: Unhealthy
    note: "Readiness probe failed: HTTP 503 from /healthz"
    type: Warning
    eventTime: "2026-07-31T11:50:00.123456Z"
`

func TestAskAutoDetectsCwdSnapshot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(repoRoot(t), "internal", "ingest", "testdata", "kubectl-list.yaml")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "k8s-snapshot.yaml"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("OPSGRAPH_FIXTURE", "")
	t.Setenv("OPSGRAPH_CONFIG", "")
	t.Setenv("OPSGRAPH_DATA_DIR", "")
	out, errOut, code := runRoot(t, "ask", "checkout", "--format", "json")
	if code != 0 {
		t.Fatalf("cwd snapshot ask exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"health": "degraded"`) {
		t.Fatalf("expected degraded checkout:\n%s", out)
	}
}

func TestPackArchiveThenTest(t *testing.T) {
	fx := fixtureDir(t)
	zipPath := filepath.Join(t.TempDir(), "incident.opsgraph")
	_, errOut, code := runRoot(t, "pack", "--fixture", fx, "--out", zipPath, "--format", "json")
	if code != 0 {
		t.Fatalf("pack zip exit=%d stderr=%s", code, errOut)
	}
	_, errOut, code = runRoot(t, "test", zipPath)
	if code != 0 {
		t.Fatalf("test zip exit=%d stderr=%s", code, errOut)
	}
	out, errOut, code := runRoot(t, "ask", "--fixture", zipPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask zip exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"id": "checkout"`) {
		t.Fatalf("ask zip missing checkout:\n%s", out)
	}
}

func TestProveJSON(t *testing.T) {
	out, errOut, code := runRoot(t, "prove", "--format", "json")
	if code != 0 {
		t.Fatalf("prove exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	var payload struct {
		OK         bool   `json:"ok"`
		PackReplay bool   `json:"pack_replay"`
		ZipReplay  bool   `json:"zip_replay"`
		SHA256     string `json:"sha256"`
		Service    string `json:"service"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !payload.OK || !payload.PackReplay || !payload.ZipReplay {
		t.Fatalf("prove envelope: %+v", payload)
	}
	if payload.Service != "checkout" {
		t.Fatalf("service=%q", payload.Service)
	}
	// Frozen so ubuntu/macOS/windows CI prove the unique claim: same .opsgraph bytes.
	const wantSHA = "76f3b7052e9ee7c06084ca894a884b9b3b62030478cffcc1d4aefd27bfebb442"
	if payload.SHA256 != wantSHA {
		t.Fatalf("prove pack sha256 = %s want %s (portable hash drifted)", payload.SHA256, wantSHA)
	}
}

func TestProveReceiptOpenStableFromScratch(t *testing.T) {
	// Full 21-loop burn lives on ubuntu CI (no -short). Windows Defender +
	// SQLite temp files make the same loop the 5-minute wall-clock hog.
	// -short still replays prove/receipt/open twice so OS hash drift fails.
	n := 21
	if testing.Short() {
		n = 2
	}
	fx := fixtureDir(t)
	var proveSHA, receiptIDs, openBody string
	for i := 0; i < n; i++ {
		out, errOut, code := runRoot(t, "prove", "--format", "json")
		if code != 0 {
			t.Fatalf("prove run %d exit=%d stderr=%s stdout=%s", i+1, code, errOut, out)
		}
		var prove struct {
			OK         bool   `json:"ok"`
			PackReplay bool   `json:"pack_replay"`
			ZipReplay  bool   `json:"zip_replay"`
			SHA256     string `json:"sha256"`
			Service    string `json:"service"`
			Evidence   int    `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(out), &prove); err != nil {
			t.Fatalf("prove run %d json: %v\n%s", i+1, err, out)
		}
		if !prove.OK || !prove.PackReplay || !prove.ZipReplay || prove.Service != "checkout" || prove.Evidence == 0 {
			t.Fatalf("prove run %d envelope: %+v", i+1, prove)
		}
		if i == 0 {
			proveSHA = prove.SHA256
			if proveSHA != "76f3b7052e9ee7c06084ca894a884b9b3b62030478cffcc1d4aefd27bfebb442" {
				t.Fatalf("prove sha256 = %s (portable hash drifted)", proveSHA)
			}
		} else if prove.SHA256 != proveSHA {
			t.Fatalf("prove sha256 drifted on from-scratch run %d\n first %s\n got   %s", i+1, proveSHA, prove.SHA256)
		}

		out, errOut, code = runRoot(t, "receipt", fx, "--format", "json")
		if code != 0 {
			t.Fatalf("receipt run %d exit=%d stderr=%s stdout=%s", i+1, code, errOut, out)
		}
		if i == 0 {
			receiptIDs = out
		} else if out != receiptIDs {
			t.Fatalf("receipt drifted on from-scratch run %d", i+1)
		}

		out, errOut, code = runRoot(t, fx, "--format", "json")
		if code != 0 {
			t.Fatalf("open-path run %d exit=%d stderr=%s stdout=%s", i+1, code, errOut, out)
		}
		if i == 0 {
			openBody = out
		} else if out != openBody {
			t.Fatalf("open-path ask JSON drifted on from-scratch run %d", i+1)
		}
	}
}

func TestRootStartHere(t *testing.T) {
	out, errOut, code := runRoot(t)
	if code != 0 {
		t.Fatalf("root exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	for _, want := range []string{"opsgraph prove", "opsgraph ask", "opsgraph pack", "incident.opsgraph", "opsgraph delta", "opsgraph receipt"} {
		if !strings.Contains(out, want) {
			t.Fatalf("start-here missing %q:\n%s", want, out)
		}
	}
}

func TestOpenPackPathAsRootArg(t *testing.T) {
	fx := fixtureDir(t)
	out, errOut, code := runRoot(t, fx, "--format", "json")
	if code != 0 {
		t.Fatalf("root pack path exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"id": "checkout"`) {
		t.Fatalf("expected checkout from dropped pack path:\n%s", out)
	}
}

func TestReceiptJSON(t *testing.T) {
	fx := fixtureDir(t)
	out, errOut, code := runRoot(t, "receipt", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("receipt exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	var rec struct {
		OK          bool     `json:"ok"`
		Service     string   `json:"service"`
		Health      string   `json:"health"`
		EvidenceIDs []string `json:"evidence_ids"`
	}
	if err := json.Unmarshal([]byte(out), &rec); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !rec.OK || rec.Service != "checkout" || rec.Health != "degraded" {
		t.Fatalf("receipt: %+v", rec)
	}
	if len(rec.EvidenceIDs) == 0 {
		t.Fatal("receipt missing evidence ids")
	}
}

func TestDeltaSameAndDifferent(t *testing.T) {
	fx := fixtureDir(t)
	fleet := filepath.Join(repoRoot(t), "fixtures", "fleet_healthy")
	out, errOut, code := runRoot(t, "delta", fx, fx, "--format", "json")
	if code != 0 {
		t.Fatalf("delta same exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"identical": true`) {
		t.Fatalf("expected identical:\n%s", out)
	}
	out, errOut, code = runRoot(t, "delta", fx, fleet, "--format", "json")
	if code != 1 {
		t.Fatalf("delta diff exit=%d want 1 stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"identical": false`) {
		t.Fatalf("expected not identical:\n%s", out)
	}
}

// A real cluster dump has no fixture clock and rarely lives in the default
// namespace. Both broke `pack`: the wall clock goldened a sub-second
// generated_at that its own replay truncated, and WritePack collapses
// namespaces so source-derived rollout ids never matched the replay.
const namespacedDumpForPack = `apiVersion: v1
kind: List
items:
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: checkout
      namespace: shop
      labels:
        app: checkout
      creationTimestamp: "2026-07-31T11:00:00Z"
    spec:
      replicas: 3
    status:
      readyReplicas: 1
  - apiVersion: apps/v1
    kind: StatefulSet
    metadata:
      name: postgres
      namespace: shop
      creationTimestamp: "2026-07-31T09:00:00Z"
    spec:
      replicas: 3
    status:
      readyReplicas: 0
`

func TestPackFromCwdSnapshotReplays(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "k8s-snapshot.yaml"), []byte(namespacedDumpForPack), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("OPSGRAPH_FIXTURE", "")
	t.Setenv("OPSGRAPH_CONFIG", "")
	t.Setenv("OPSGRAPH_DATA_DIR", "")

	out, errOut, code := runRoot(t, "pack", "--format", "json")
	if code != 0 {
		t.Fatalf("pack from cwd snapshot exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	packPath := filepath.Join(dir, "incident.opsgraph")
	if _, err := os.Stat(packPath); err != nil {
		t.Fatalf("expected default pack: %v", err)
	}
	if _, errOut, code = runRoot(t, "test", packPath); code != 0 {
		t.Fatalf("replay of cwd pack exit=%d stderr=%s", code, errOut)
	}
	// The StatefulSet must survive the round trip, not just the Deployment.
	out, errOut, code = runRoot(t, "ask", "postgres", "--fixture", packPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask postgres from pack exit=%d stderr=%s stdout=%s", code, errOut, out)
	}
	if !strings.Contains(out, `"health": "unhealthy"`) {
		t.Fatalf("statefulset health lost in pack:\n%s", out)
	}
}

func TestPackDefaultOut(t *testing.T) {
	fx := fixtureDir(t)
	dir := t.TempDir()
	t.Chdir(dir)
	_, errOut, code := runRoot(t, "pack", "--fixture", fx, "--format", "json")
	if code != 0 {
		t.Fatalf("pack default out exit=%d stderr=%s", code, errOut)
	}
	zipPath := filepath.Join(dir, "incident.opsgraph")
	if _, err := os.Stat(zipPath); err != nil {
		t.Fatalf("expected default pack: %v", err)
	}
	_, errOut, code = runRoot(t, "test", zipPath)
	if code != 0 {
		t.Fatalf("test default pack exit=%d stderr=%s", code, errOut)
	}
}
