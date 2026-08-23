package ingest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestLooksNativeK8s(t *testing.T) {
	if !looksNativeK8s([]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: x\n")) {
		t.Fatal("Deployment should look native")
	}
	if looksNativeK8s([]byte("deployments:\n  - name: x\n    service_id: x\n")) {
		t.Fatal("opsgraph dialect must not look native")
	}
	hybrid := []byte("kind: List\ndeployments:\n  - name: x\n    service_id: x\n")
	if looksNativeK8s(hybrid) {
		t.Fatal("custom deployments: key must win over kind")
	}
}

func TestInferServiceID(t *testing.T) {
	if got := inferServiceID("Deployment", "checkout-api", map[string]string{"app": "checkout"}); got != "checkout" {
		t.Fatalf("label app: got %q", got)
	}
	if got := inferServiceID("Deployment", "checkout", nil); got != "checkout" {
		t.Fatalf("deploy name: got %q", got)
	}
	if got := inferServiceID("ReplicaSet", "checkout-7d9f8c4b5d", nil); got != "checkout" {
		t.Fatalf("rs hash: got %q", got)
	}
	if got := inferServiceID("Pod", "checkout-7d9f8c4b5d-xk2n1", nil); got != "checkout" {
		t.Fatalf("pod hash: got %q", got)
	}
	if got := inferServiceID("Deployment", "service-admin", nil); got != "service-admin" {
		t.Fatalf("must not strip non-hash suffix: got %q", got)
	}
}

func TestParseNativeKubectlList(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "kubectl-list.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !looksNativeK8s(data) {
		t.Fatal("kubectl list should look native")
	}
	deps, evs, err := parseNativeK8s(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Deployments) != 2 {
		t.Fatalf("deployments = %d, want 2: %+v", len(deps.Deployments), deps.Deployments)
	}
	byName := map[string]k8sDeployment{}
	for _, d := range deps.Deployments {
		byName[d.Name] = d
	}
	co := byName["checkout"]
	if co.ServiceID != "checkout" || co.Desired != 3 || co.Ready != 1 {
		t.Fatalf("checkout deploy: %+v", co)
	}
	wantAt := time.Date(2026, 7, 31, 11, 38, 0, 0, time.UTC)
	if !co.UpdatedAt.Equal(wantAt) {
		t.Fatalf("checkout updated_at = %v, want %v", co.UpdatedAt, wantAt)
	}
	auth := byName["auth"]
	if auth.ServiceID != "auth" || auth.Ready != 0 {
		t.Fatalf("auth deploy: %+v", auth)
	}
	if len(evs.Events) != 1 {
		t.Fatalf("events = %d, want 1 (ConfigMap ignored): %+v", len(evs.Events), evs.Events)
	}
	if evs.Events[0].ServiceID != "checkout" {
		t.Fatalf("event service_id = %q (pod hash should strip to checkout)", evs.Events[0].ServiceID)
	}
	if evs.Events[0].Reason != "Unhealthy" {
		t.Fatalf("event reason = %q", evs.Events[0].Reason)
	}
}

func TestNativeKubectlListIngest(t *testing.T) {
	root := t.TempDir()
	src, err := os.ReadFile(filepath.Join("testdata", "kubectl-list.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deployments.yaml"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	if err := ingestK8sFiles(s, os.DirFS(root), "deployments.yaml", "events.yaml", now, k8sAllow{}); err != nil {
		t.Fatal(err)
	}
	co, err := s.GetService("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if co.Health != "degraded" {
		t.Fatalf("checkout health = %q, want degraded", co.Health)
	}
	auth, err := s.GetService("auth")
	if err != nil {
		t.Fatal(err)
	}
	if auth.Health != "unhealthy" {
		t.Fatalf("auth health = %q, want unhealthy", auth.Health)
	}
	evs, err := s.ListAllEvidence()
	if err != nil {
		t.Fatal(err)
	}
	foundEvent := false
	for _, e := range evs {
		if e.Kind == "k8s-event" && e.ServiceID == "checkout" {
			foundEvent = true
			break
		}
	}
	if !foundEvent {
		t.Fatalf("missing k8s-event evidence for checkout: %+v", evs)
	}
}

func TestCustomDialectInfersServiceIDFromName(t *testing.T) {
	root := t.TempDir()
	body := "deployments:\n  - name: payments-api\n    desired: 1\n    ready: 1\n    updated_at: 2026-07-31T11:00:00Z\n"
	if err := os.WriteFile(filepath.Join(root, "deployments.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := ingestK8sFiles(s, os.DirFS(root), "deployments.yaml", "events.yaml",
		time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), k8sAllow{}); err != nil {
		t.Fatal(err)
	}
	svc, err := s.GetService("payments-api")
	if err != nil {
		t.Fatal(err)
	}
	if svc.Health != "healthy" {
		t.Fatalf("health = %q", svc.Health)
	}
}

func TestParseNativeMultiDoc(t *testing.T) {
	data := []byte("" +
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: a\n  labels:\n    app: a\nspec:\n  replicas: 1\nstatus:\n  readyReplicas: 1\n" +
		"---\n" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: e1\ninvolvedObject:\n  kind: Deployment\n  name: a\nreason: Pulled\nmessage: ok\nlastTimestamp: \"2026-07-31T11:00:00Z\"\n")
	deps, evs, err := parseNativeK8s(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Deployments) != 1 || deps.Deployments[0].ServiceID != "a" {
		t.Fatalf("deps: %+v", deps.Deployments)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "a" {
		t.Fatalf("evs: %+v", evs.Events)
	}
}

func FuzzParseNativeK8s(f *testing.F) {
	f.Add([]byte("kind: List\nitems: []\n"))
	f.Add([]byte("kind: Deployment\nmetadata:\n  name: x\n"))
	f.Add([]byte("deployments:\n  - name: x\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !looksNativeK8s(data) {
			return
		}
		_, _, _ = parseNativeK8s(data)
	})
}
