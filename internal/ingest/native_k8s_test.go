package ingest

import (
	"os"
	"path/filepath"
	"strings"
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
	if got := inferServiceID("Deployment", "payments-worker", map[string]string{"app.kubernetes.io/component": "billing"}); got != "billing" {
		t.Fatalf("component label: got %q", got)
	}
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
	deps, evs, _, err := parseNativeK8s(data)
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

// events.k8s.io/v1 is what `kubectl get event` emits on current clusters.
// regarding + note replace involvedObject + message; lastTimestamp is gone.
const eventsV1Snapshot = `apiVersion: v1
kind: List
items:
  - apiVersion: apps/v1
    kind: Deployment
    metadata:
      name: checkout
      labels:
        app: checkout
    spec:
      replicas: 3
    status:
      readyReplicas: 1
  - apiVersion: events.k8s.io/v1
    kind: Event
    metadata:
      name: checkout.17f8c
      namespace: shop
    regarding:
      kind: Pod
      name: checkout-7d9f8c4b5d-xk2n1
      namespace: shop
    reason: Unhealthy
    note: "Readiness probe failed: HTTP 503 from /healthz"
    type: Warning
    eventTime: "2026-07-31T11:50:00.123456Z"
    deprecatedLastTimestamp: "2026-07-31T11:50:00Z"
`

func TestParseEventsK8sV1RegardingAndNote(t *testing.T) {
	if !looksNativeK8s([]byte(eventsV1Snapshot)) {
		t.Fatal("events.k8s.io/v1 List should look native")
	}
	deps, evs, _, err := parseNativeK8s([]byte(eventsV1Snapshot))
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Deployments) != 1 || deps.Deployments[0].ServiceID != "checkout" {
		t.Fatalf("deps: %+v", deps.Deployments)
	}
	if len(evs.Events) != 1 {
		t.Fatalf("events = %d, want 1 (must not invent service checkout.17f8c): %+v", len(evs.Events), evs.Events)
	}
	ev := evs.Events[0]
	if ev.ServiceID != "checkout" {
		t.Fatalf("regarding pod hash should strip to checkout, got %q", ev.ServiceID)
	}
	if ev.Message != "Readiness probe failed: HTTP 503 from /healthz" {
		t.Fatalf("note should become message, got %q", ev.Message)
	}
	if ev.Reason != "Unhealthy" || ev.Type != "Warning" {
		t.Fatalf("event fields: %+v", ev)
	}
	wantAt := time.Date(2026, 7, 31, 11, 50, 0, 0, time.UTC)
	if !ev.At.Equal(wantAt) {
		t.Fatalf("at = %v, want %v (deprecatedLastTimestamp or eventTime)", ev.At, wantAt)
	}
	if ev.Namespace != "shop" {
		t.Fatalf("namespace = %q", ev.Namespace)
	}
}

func TestIngestEventsK8sV1EvidenceOnService(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "deployments.yaml"), []byte(eventsV1Snapshot), 0o644); err != nil {
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
	evs, err := s.ListAllEvidence()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == "k8s-event" && e.ServiceID == "checkout" && strings.Contains(e.Summary, "HTTP 503") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("events.k8s.io/v1 note never became checkout evidence: %+v", evs)
	}
}

func TestParseNodeEventDoesNotInventService(t *testing.T) {
	node := []byte("" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: ip-10-0-1-5.17f8c\n" +
		"involvedObject:\n  kind: Node\n  name: ip-10-0-1-5\nreason: NodeReady\nmessage: node is ready\n")
	_, evs, _, err := parseNativeK8s(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 0 {
		t.Fatalf("Node events must not become services: %+v", evs.Events)
	}

	svc := []byte("" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: checkout.svc\n" +
		"involvedObject:\n  kind: Service\n  name: checkout\nreason: Updated\nmessage: endpoints\n")
	_, evs, _, err = parseNativeK8s(svc)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "checkout" {
		t.Fatalf("Service events should map: %+v", evs.Events)
	}
}

func TestParseCronJobAndJobEventsMapToService(t *testing.T) {
	cron := []byte("" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: billing.17f8c\n" +
		"involvedObject:\n  kind: CronJob\n  name: billing-settle\nreason: SawMiss\nmessage: missed run\n")
	_, evs, _, err := parseNativeK8s(cron)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "billing-settle" {
		t.Fatalf("CronJob events should map: %+v", evs.Events)
	}

	job := []byte("" +
		"apiVersion: events.k8s.io/v1\nkind: Event\nmetadata:\n  name: billing.job\n" +
		"regarding:\n  kind: Job\n  name: billing-settle-28654321\nnote: backoff limit exceeded\n")
	_, evs, _, err = parseNativeK8s(job)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "billing-settle" {
		t.Fatalf("CronJob Job timestamp should strip: %+v", evs.Events)
	}

	named := []byte("" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: migrate.1\n" +
		"involvedObject:\n  kind: Job\n  name: migrate-1\nreason: Completed\nmessage: done\n")
	_, evs, _, err = parseNativeK8s(named)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "migrate-1" {
		t.Fatalf("hand-named Job must keep short suffix: %+v", evs.Events)
	}
}

func TestParseEventWithoutObjectRefDoesNotInventService(t *testing.T) {
	orphan := []byte("" +
		"apiVersion: events.k8s.io/v1\nkind: Event\nmetadata:\n  name: checkout.17f8c\n  namespace: shop\nreason: Unhealthy\nnote: dropped\n")
	_, evs, _, err := parseNativeK8s(orphan)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 0 {
		t.Fatalf("metadata.name must not become a service: %+v", evs.Events)
	}

	labeled := []byte("" +
		"apiVersion: events.k8s.io/v1\nkind: Event\nmetadata:\n  name: checkout.17f8c\n  labels:\n    app: checkout\nnote: labeled\n")
	_, evs, _, err = parseNativeK8s(labeled)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "checkout" {
		t.Fatalf("label-only event should map to checkout: %+v", evs.Events)
	}
}

func TestParseNativeMultiDoc(t *testing.T) {
	data := []byte("" +
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: a\n  labels:\n    app: a\nspec:\n  replicas: 1\nstatus:\n  readyReplicas: 1\n" +
		"---\n" +
		"apiVersion: v1\nkind: Event\nmetadata:\n  name: e1\ninvolvedObject:\n  kind: Deployment\n  name: a\nreason: Pulled\nmessage: ok\nlastTimestamp: \"2026-07-31T11:00:00Z\"\n")
	deps, evs, _, err := parseNativeK8s(data)
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

// Real clusters run datastores as StatefulSets and agents as DaemonSets; both
// must produce health, not silence.
const workloadSnapshot = `apiVersion: v1
kind: List
items:
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
  - apiVersion: apps/v1
    kind: DaemonSet
    metadata:
      name: fluentbit
      namespace: kube-system
      creationTimestamp: "2026-07-31T09:00:00Z"
    status:
      desiredNumberScheduled: 6
      numberReady: 2
  - apiVersion: v1
    kind: Event
    metadata:
      name: postgres.17f8c
      namespace: shop
    involvedObject:
      kind: Pod
      name: postgres-0
      namespace: shop
    reason: Unhealthy
    message: "Readiness probe failed: connection refused"
    type: Warning
    lastTimestamp: "2026-07-31T11:50:00Z"
`

func TestParseNativeStatefulSetAndDaemonSet(t *testing.T) {
	if !looksNativeK8s([]byte("apiVersion: apps/v1\nkind: StatefulSet\nmetadata:\n  name: db\n")) {
		t.Fatal("StatefulSet-only dump should look native")
	}
	if !looksNativeK8s([]byte("apiVersion: apps/v1\nkind: DaemonSet\nmetadata:\n  name: agent\n")) {
		t.Fatal("DaemonSet-only dump should look native")
	}
	deps, evs, stats, err := parseNativeK8s([]byte(workloadSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Deployments) != 2 {
		t.Fatalf("workloads = %d, want 2: %+v", len(deps.Deployments), deps.Deployments)
	}
	byName := map[string]k8sDeployment{}
	for _, d := range deps.Deployments {
		byName[d.Name] = d
	}
	pg := byName["postgres"]
	if pg.Kind != "statefulset" || pg.Desired != 3 || pg.Ready != 0 {
		t.Fatalf("statefulset: %+v", pg)
	}
	// DaemonSets have no spec.replicas: counts come from scheduling status.
	fb := byName["fluentbit"]
	if fb.Kind != "daemonset" || fb.Desired != 6 || fb.Ready != 2 {
		t.Fatalf("daemonset: %+v", fb)
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "postgres" {
		t.Fatalf("statefulset pod ordinal should map to postgres: %+v", evs.Events)
	}
	if stats.Kinds["StatefulSet"] != 1 || stats.Kinds["DaemonSet"] != 1 {
		t.Fatalf("stats kinds: %+v", stats.Kinds)
	}
}

func TestInferServiceIDStatefulSetPodOrdinal(t *testing.T) {
	if got := inferServiceID("Pod", "postgres-0", nil); got != "postgres" {
		t.Fatalf("ordinal strip: got %q", got)
	}
	if got := inferServiceID("Pod", "kafka-12", nil); got != "kafka" {
		t.Fatalf("multi-digit ordinal: got %q", got)
	}
	// A Deployment pod keeps its numeric name segment; only the hashes go.
	if got := inferServiceID("Pod", "web-2-7d9f8c4b5d-xk2n1", nil); got != "web-2" {
		t.Fatalf("deployment pod must keep name digits: got %q", got)
	}
	if got := inferServiceID("Job", "billing-settle-28654321", nil); got != "billing-settle" {
		t.Fatalf("cronjob job timestamp: got %q", got)
	}
	if got := inferServiceID("Job", "migrate-1", nil); got != "migrate-1" {
		t.Fatalf("short job suffix must stay: got %q", got)
	}
	if got := inferServiceID("CronJob", "billing-settle", nil); got != "billing-settle" {
		t.Fatalf("cronjob name: got %q", got)
	}
}

func TestIngestStatefulSetAndDaemonSetHealth(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "deployments.yaml"), []byte(workloadSnapshot), 0o644); err != nil {
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
	pg, err := s.GetService("postgres")
	if err != nil {
		t.Fatalf("statefulset must produce a service: %v", err)
	}
	if pg.Health != "unhealthy" {
		t.Fatalf("postgres health = %q, want unhealthy", pg.Health)
	}
	fb, err := s.GetService("fluentbit")
	if err != nil {
		t.Fatalf("daemonset must produce a service: %v", err)
	}
	if fb.Health != "degraded" {
		t.Fatalf("fluentbit health = %q, want degraded", fb.Health)
	}
	evs, err := s.ListAllEvidence()
	if err != nil {
		t.Fatal(err)
	}
	// Kind is part of the id so a Deployment and StatefulSet of one name cannot collide.
	want := map[string]bool{"ev-k8s-rollout-shop-statefulset-postgres": false, "ev-k8s-rollout-kube-system-daemonset-fluentbit": false}
	for _, e := range evs {
		if _, ok := want[e.ID]; ok {
			want[e.ID] = true
		}
	}
	for id, found := range want {
		if !found {
			t.Fatalf("missing rollout evidence %q in %+v", id, evs)
		}
	}
}

func TestDeploymentRolloutIDsUnchanged(t *testing.T) {
	// Deployments must keep their legacy ids so pack hashes and goldens hold.
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
	if err := ingestK8sFiles(s, os.DirFS(root), "deployments.yaml", "events.yaml",
		time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), k8sAllow{}); err != nil {
		t.Fatal(err)
	}
	evs, err := s.ListAllEvidence()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.ID == "ev-k8s-rollout-checkout" {
			found = true
			if e.Summary != "rollout checkout (1/3 ready)" {
				t.Fatalf("deployment summary drifted: %q", e.Summary)
			}
		}
	}
	if !found {
		t.Fatalf("legacy deployment rollout id missing: %+v", evs)
	}
}

const jobFailedYAML = `
apiVersion: batch/v1
kind: Job
metadata:
  name: billing-settle-28654321
  namespace: shop
  labels:
    app.kubernetes.io/name: billing-settle
spec:
  completions: 1
status:
  succeeded: 0
  failed: 1
---
apiVersion: v1
kind: Event
metadata:
  name: billing-settle-28654321.17f8c
  namespace: shop
involvedObject:
  kind: Job
  name: billing-settle-28654321
  namespace: shop
reason: BackoffLimitExceeded
message: Job has reached the specified backoff limit
lastTimestamp: "2026-07-31T11:50:00Z"
`

func TestParseJobFailedIsUnhealthy(t *testing.T) {
	if !looksNativeK8s([]byte(jobFailedYAML)) {
		t.Fatal("Job YAML must look native")
	}
	deps, evs, _, err := parseNativeK8s([]byte(jobFailedYAML))
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Deployments) != 1 {
		t.Fatalf("jobs = %d want 1: %+v", len(deps.Deployments), deps.Deployments)
	}
	j := deps.Deployments[0]
	if j.Kind != "job" || j.ServiceID != "billing-settle" || j.Desired != 1 || j.Ready != 0 {
		t.Fatalf("failed job: %+v", j)
	}
	if deploymentHealth(j.Desired, j.Ready) != "unhealthy" {
		t.Fatalf("failed job health = %q", deploymentHealth(j.Desired, j.Ready))
	}
	if len(evs.Events) != 1 || evs.Events[0].ServiceID != "billing-settle" {
		t.Fatalf("job event: %+v", evs.Events)
	}
}

func TestJobInProgressDoesNotPage(t *testing.T) {
	desired, ready := workloadReplicas(nativeObject{
		Spec:   nativeDepSpec{Completions: intPtr(1)},
		Status: nativeDepStatus{Active: 1},
	}, "job")
	if desired != 1 || ready != 1 {
		t.Fatalf("in-progress job must look ready: desired=%d ready=%d", desired, ready)
	}
	desired, ready = workloadReplicas(nativeObject{
		Spec:   nativeDepSpec{Completions: intPtr(3)},
		Status: nativeDepStatus{Succeeded: 3},
	}, "job")
	if desired != 3 || ready != 3 {
		t.Fatalf("completed job: desired=%d ready=%d", desired, ready)
	}
}

func intPtr(n int) *int { return &n }

func TestEventOnlyJobIsAskable(t *testing.T) {
	root := t.TempDir()
	body := `
apiVersion: v1
kind: Event
metadata:
  name: billing-settle-28654321.abc
  namespace: shop
involvedObject:
  kind: Job
  name: billing-settle-28654321
reason: Completed
message: Job completed
lastTimestamp: "2026-07-31T11:50:00Z"
`
	if err := os.WriteFile(filepath.Join(root, "events.yaml"), []byte(body), 0o644); err != nil {
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
	svc, err := s.GetService("billing-settle")
	if err != nil {
		t.Fatalf("event-only Job must be askable: %v", err)
	}
	if svc.Health != "unknown" {
		t.Fatalf("event-only health = %q", svc.Health)
	}
	if !hasSource(svc.Sources, "kubernetes") {
		t.Fatalf("sources = %v", svc.Sources)
	}
}

func TestSnapshotStatsSummary(t *testing.T) {
	stats := k8sSnapshotStats{}
	stats.observe("CronJob")
	stats.observe("Service")
	stats.observe("CronJob")
	if got := stats.summary(); got != "CronJob x2, Service x1" {
		t.Fatalf("summary = %q", got)
	}
	if got := (k8sSnapshotStats{}).summary(); got != "no objects" {
		t.Fatalf("empty summary = %q", got)
	}
}

func FuzzParseNativeK8s(f *testing.F) {
	f.Add([]byte("kind: List\nitems: []\n"))
	f.Add([]byte("kind: Deployment\nmetadata:\n  name: x\n"))
	f.Add([]byte("deployments:\n  - name: x\n"))
	f.Add([]byte("kind: Event\napiVersion: events.k8s.io/v1\nregarding:\n  kind: Pod\n  name: x-0\nnote: n\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		if !looksNativeK8s(data) {
			return
		}
		_, _, _, _ = parseNativeK8s(data)
	})
}
