package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Native Kubernetes API YAML (kubectl get -o yaml) is accepted in addition to
// the opsgraph dialect (top-level deployments:/events:). No k8s.io / client-go.

type nativeObject struct {
	Kind                     string           `yaml:"kind"`
	APIVersion               string           `yaml:"apiVersion"`
	Metadata                 nativeObjectMeta `yaml:"metadata"`
	Spec                     nativeDepSpec    `yaml:"spec"`
	Status                   nativeDepStatus  `yaml:"status"`
	InvolvedObject           nativeObjectRef  `yaml:"involvedObject"`
	Regarding                nativeObjectRef  `yaml:"regarding"`
	Reason                   string           `yaml:"reason"`
	Message                  string           `yaml:"message"`
	Note                     string           `yaml:"note"`
	Type                     string           `yaml:"type"`
	LastTimestamp            any              `yaml:"lastTimestamp"`
	FirstTimestamp           any              `yaml:"firstTimestamp"`
	DeprecatedLastTimestamp  any              `yaml:"deprecatedLastTimestamp"`
	DeprecatedFirstTimestamp any              `yaml:"deprecatedFirstTimestamp"`
	EventTime                any              `yaml:"eventTime"`
	CreationTimestamp        any              `yaml:"creationTimestamp"`
	Items                    []nativeObject   `yaml:"items"`
}

type nativeObjectMeta struct {
	Name              string            `yaml:"name"`
	Namespace         string            `yaml:"namespace"`
	Labels            map[string]string `yaml:"labels"`
	CreationTimestamp any               `yaml:"creationTimestamp"`
}

type nativeDepSpec struct {
	Replicas    *int `yaml:"replicas"`
	Completions *int `yaml:"completions"`
}

type nativeDepStatus struct {
	Replicas      int `yaml:"replicas"`
	ReadyReplicas int `yaml:"readyReplicas"`
	// DaemonSets have no spec.replicas; readiness is counted per scheduled node.
	DesiredNumberScheduled int               `yaml:"desiredNumberScheduled"`
	NumberReady            int               `yaml:"numberReady"`
	Succeeded              int               `yaml:"succeeded"`
	Failed                 int               `yaml:"failed"`
	Active                 int               `yaml:"active"`
	Conditions             []nativeCondition `yaml:"conditions"`
}

type nativeCondition struct {
	Type               string `yaml:"type"`
	Status             string `yaml:"status"`
	LastUpdateTime     any    `yaml:"lastUpdateTime"`
	LastTransitionTime any    `yaml:"lastTransitionTime"`
}

type nativeObjectRef struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

func normalizeKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

// isWorkloadKind reports kinds that carry replica health. Deployments cover
// stateless apps, StatefulSets cover datastores and queues, DaemonSets cover
// per-node agents. Dropping any of them hides an outage behind "service not found".
func isWorkloadKind(kind string) bool {
	switch normalizeKind(kind) {
	case "deployment", "statefulset", "daemonset", "job", "cronjob":
		return true
	}
	return false
}

func eventObjectKindAllowed(kind string) bool {
	switch normalizeKind(kind) {
	case "pod", "replicaset", "deployment", "statefulset", "daemonset", "service", "job", "cronjob":
		return true
	}
	return false
}

func hasAppLabel(labels map[string]string) bool {
	for _, k := range []string{"app.kubernetes.io/name", "app", "app.kubernetes.io/component"} {
		if strings.TrimSpace(labels[k]) != "" {
			return true
		}
	}
	return false
}

// k8sSnapshotStats records what a native dump actually contained, so a snapshot
// that yields no workloads can explain itself instead of looking like a healthy fleet.
type k8sSnapshotStats struct {
	Native bool
	Kinds  map[string]int
}

func (s *k8sSnapshotStats) observe(kind string) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return
	}
	if s.Kinds == nil {
		s.Kinds = map[string]int{}
	}
	s.Kinds[kind]++
}

func (s *k8sSnapshotStats) merge(other k8sSnapshotStats) {
	if other.Native {
		s.Native = true
	}
	for kind, n := range other.Kinds {
		if s.Kinds == nil {
			s.Kinds = map[string]int{}
		}
		s.Kinds[kind] += n
	}
}

// summary renders "CronJob x2, Service x3", sorted so diagnostics stay stable.
func (s k8sSnapshotStats) summary() string {
	if len(s.Kinds) == 0 {
		return "no objects"
	}
	names := make([]string, 0, len(s.Kinds))
	for kind := range s.Kinds {
		names = append(names, kind)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, kind := range names {
		parts = append(parts, fmt.Sprintf("%s x%d", kind, s.Kinds[kind]))
	}
	return strings.Join(parts, ", ")
}

func loadK8sSnapshot(fsys fs.FS, depFile, evFile string) (k8sDeployments, k8sEvents, k8sSnapshotStats, error) {
	var deps k8sDeployments
	var evs k8sEvents
	var stats k8sSnapshotStats
	seen := map[string]bool{}
	appendFile := func(name string) error {
		if name == "" || seen[name] {
			return nil
		}
		seen[name] = true
		d, e, st, found, err := loadK8sFile(fsys, name)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		deps.Deployments = append(deps.Deployments, d.Deployments...)
		deps.leftoverRS = append(deps.leftoverRS, d.leftoverRS...)
		evs.Events = append(evs.Events, e.Events...)
		stats.merge(st)
		return nil
	}
	if err := appendFile(depFile); err != nil {
		return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, err
	}
	if err := appendFile(evFile); err != nil {
		return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, err
	}
	fillMissingDeploymentServiceIDs(&deps)
	return deps, evs, stats, nil
}

func loadK8sFile(fsys fs.FS, name string) (k8sDeployments, k8sEvents, k8sSnapshotStats, bool, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, false, nil
		}
		return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, false, fmt.Errorf("read %s: %w", name, err)
	}
	if looksNativeK8s(data) {
		d, e, st, err := parseNativeK8s(data)
		if err != nil {
			return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, false, fmt.Errorf("parse native k8s %s: %w", name, err)
		}
		return d, e, st, true, nil
	}
	var deps k8sDeployments
	if err := yaml.Unmarshal(data, &deps); err != nil {
		return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, false, fmt.Errorf("parse %s: %w", name, err)
	}
	var evs k8sEvents
	if err := yaml.Unmarshal(data, &evs); err != nil {
		return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, false, fmt.Errorf("parse %s: %w", name, err)
	}
	return deps, evs, k8sSnapshotStats{}, true, nil
}

func fillMissingDeploymentServiceIDs(deps *k8sDeployments) {
	if deps == nil {
		return
	}
	fill := func(list []k8sDeployment) {
		for i := range list {
			if strings.TrimSpace(list[i].ServiceID) != "" {
				continue
			}
			list[i].ServiceID = inferServiceID(list[i].Kind, list[i].Name, nil)
		}
	}
	fill(deps.Deployments)
	fill(deps.leftoverRS)
	if len(deps.Deployments) == 0 && len(deps.leftoverRS) > 0 {
		deps.Deployments = deps.leftoverRS
	}
}

// looksNativeK8s reports kubectl/API YAML (kind List/Deployment/Event). The
// opsgraph dialect (top-level deployments: / events:) always wins.
func looksNativeK8s(data []byte) bool {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	sawNative := false
	for {
		var probe struct {
			Kind        string `yaml:"kind"`
			Deployments any    `yaml:"deployments"`
			Events      any    `yaml:"events"`
		}
		if err := dec.Decode(&probe); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return false
		}
		if probe.Deployments != nil || probe.Events != nil {
			return false
		}
		kind := normalizeKind(probe.Kind)
		if kind == "list" || kind == "event" || kind == "replicaset" || isWorkloadKind(kind) {
			sawNative = true
		}
	}
	return sawNative
}

func parseNativeK8s(data []byte) (k8sDeployments, k8sEvents, k8sSnapshotStats, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var deps k8sDeployments
	var evs k8sEvents
	stats := k8sSnapshotStats{Native: true}
	for {
		var obj nativeObject
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return k8sDeployments{}, k8sEvents{}, k8sSnapshotStats{}, err
		}
		collectNative(&deps, &evs, &stats, obj)
	}
	return deps, evs, stats, nil
}

func collectNative(deps *k8sDeployments, evs *k8sEvents, stats *k8sSnapshotStats, obj nativeObject) {
	kind := normalizeKind(obj.Kind)
	if kind != "list" {
		stats.observe(strings.TrimSpace(obj.Kind))
	}
	switch {
	case kind == "list":
		for _, item := range obj.Items {
			collectNative(deps, evs, stats, item)
		}
	case isWorkloadKind(kind):
		if d, ok := nativeToWorkload(obj); ok {
			deps.Deployments = append(deps.Deployments, d)
		}
	case kind == "replicaset":
		if d, ok := nativeToWorkload(obj); ok {
			deps.leftoverRS = append(deps.leftoverRS, d)
		}
	case kind == "event":
		if e, ok := nativeToEvent(obj); ok {
			evs.Events = append(evs.Events, e)
		}
	}
}

func nativeToWorkload(o nativeObject) (k8sDeployment, bool) {
	name := strings.TrimSpace(o.Metadata.Name)
	if name == "" {
		return k8sDeployment{}, false
	}
	kind := normalizeKind(o.Kind)
	desired, ready := workloadReplicas(o, kind)
	ns := strings.TrimSpace(o.Metadata.Namespace)
	if ns == "" {
		ns = "default"
	}
	return k8sDeployment{
		Kind:      kind,
		Name:      name,
		Namespace: ns,
		ServiceID: inferServiceID(o.Kind, name, o.Metadata.Labels),
		Desired:   desired,
		Ready:     ready,
		UpdatedAt: deploymentUpdatedAt(o),
	}, true
}

// workloadReplicas reads per-kind replica counters. DaemonSets have no
// spec.replicas: readiness is counted against the nodes they schedule onto.
func workloadReplicas(o nativeObject, kind string) (desired, ready int) {
	if kind == "daemonset" {
		return o.Status.DesiredNumberScheduled, o.Status.NumberReady
	}
	if kind == "cronjob" {
		// Spec-only: last run is unknown until a Job/Event is in the dump.
		return 0, 0
	}
	if kind == "job" {
		desired = 1
		if o.Spec.Completions != nil {
			desired = *o.Spec.Completions
		}
		if desired <= 0 {
			desired = 1
		}
		if o.Status.Succeeded >= desired {
			return desired, desired
		}
		if o.Status.Failed > 0 {
			return desired, 0
		}
		// In-progress or empty status: do not page.
		return desired, desired
	}
	desired = 1
	if o.Spec.Replicas != nil {
		desired = *o.Spec.Replicas
	}
	return desired, o.Status.ReadyReplicas
}

func nativeToEvent(o nativeObject) (k8sEvent, bool) {
	// core/v1 uses involvedObject + message; events.k8s.io/v1 uses regarding + note.
	// kubectl get event on current clusters often emits the latter. Falling back
	// to metadata.name would invent a fake service (checkout.17f8c) and drop the text.
	ref := o.InvolvedObject
	if strings.TrimSpace(ref.Name) == "" {
		ref = o.Regarding
	}
	// Labels may still map an event with no object ref. Event metadata.name
	// (checkout.17f8c) must never become a service id.
	sid := inferServiceID(ref.Kind, ref.Name, o.Metadata.Labels)
	if sid == "" {
		return k8sEvent{}, false
	}
	// Node/ConfigMap/Lease events are common in `kubectl get event` and must
	// not become fleet services. Labels still map; workload/Service/Job/CronJob refs too.
	if !eventObjectKindAllowed(ref.Kind) && !hasAppLabel(o.Metadata.Labels) {
		return k8sEvent{}, false
	}
	ns := strings.TrimSpace(o.Metadata.Namespace)
	if ns == "" {
		ns = strings.TrimSpace(ref.Namespace)
	}
	msg := strings.TrimSpace(o.Message)
	if msg == "" {
		msg = strings.TrimSpace(o.Note)
	}
	at := firstK8sTime(o.LastTimestamp, o.DeprecatedLastTimestamp, o.EventTime, o.FirstTimestamp, o.DeprecatedFirstTimestamp, o.Metadata.CreationTimestamp, o.CreationTimestamp)
	return k8sEvent{
		ServiceID: sid,
		Namespace: ns,
		At:        at,
		Reason:    o.Reason,
		Message:   msg,
		Type:      o.Type,
	}, true
}

func deploymentUpdatedAt(o nativeObject) time.Time {
	var progressing, newest time.Time
	for _, c := range o.Status.Conditions {
		t := firstK8sTime(c.LastUpdateTime, c.LastTransitionTime)
		if t.IsZero() {
			continue
		}
		if strings.EqualFold(c.Type, "Progressing") {
			progressing = t
		}
		if newest.IsZero() || t.After(newest) {
			newest = t
		}
	}
	if !progressing.IsZero() {
		return progressing
	}
	if !newest.IsZero() {
		return newest
	}
	return firstK8sTime(o.Metadata.CreationTimestamp, o.CreationTimestamp)
}

// inferServiceID maps a Kubernetes object onto an opsgraph service id.
// Labels win; ReplicaSet/Pod names strip controller hashes that contain a digit.
func inferServiceID(kind, name string, labels map[string]string) string {
	for _, k := range []string{"app.kubernetes.io/name", "app", "app.kubernetes.io/component"} {
		if v := strings.TrimSpace(labels[k]); v != "" {
			return v
		}
	}
	name = strings.TrimSpace(name)
	switch normalizeKind(kind) {
	case "pod":
		stripped := stripK8sHashSuffix(stripK8sHashSuffix(name))
		if stripped == name {
			// No controller hash: StatefulSet pods are <name>-<ordinal>.
			stripped = stripOrdinalSuffix(name)
		}
		name = stripped
	case "replicaset":
		name = stripK8sHashSuffix(name)
	case "job":
		// CronJob-created Jobs are <cronjob>-<epoch-minutes> (8+ digits).
		name = stripCronJobTimestamp(name)
	}
	return name
}

// stripCronJobTimestamp drops the scheduled-time suffix CronJob adds to Jobs
// (billing-settle-28654321 → billing-settle). Short numeric tails like
// migrate-1 stay put so a hand-named Job is not renamed.
func stripCronJobTimestamp(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 || i == len(name)-1 {
		return name
	}
	suf := name[i+1:]
	if len(suf) < 8 {
		return name
	}
	for _, r := range suf {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:i]
}

// stripOrdinalSuffix drops the StatefulSet pod ordinal (postgres-0 → postgres).
func stripOrdinalSuffix(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 || i == len(name)-1 {
		return name
	}
	for _, r := range name[i+1:] {
		if r < '0' || r > '9' {
			return name
		}
	}
	return name[:i]
}

func stripK8sHashSuffix(name string) string {
	i := strings.LastIndex(name, "-")
	if i <= 0 {
		return name
	}
	if isK8sControllerHash(name[i+1:]) {
		return name[:i]
	}
	return name
}

func isK8sControllerHash(s string) bool {
	if len(s) < 5 || len(s) > 10 {
		return false
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		if r >= 'a' && r <= 'z' {
			continue
		}
		return false
	}
	return hasDigit
}

func firstK8sTime(vals ...any) time.Time {
	for _, v := range vals {
		if t := parseFlexibleTime(v); !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func parseFlexibleTime(v any) time.Time {
	switch t := v.(type) {
	case time.Time:
		if t.IsZero() {
			return time.Time{}
		}
		return t.UTC()
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return time.Time{}
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, s); err == nil {
				return parsed.UTC()
			}
		}
	}
	return time.Time{}
}
