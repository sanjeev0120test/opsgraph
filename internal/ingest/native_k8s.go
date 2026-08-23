package ingest

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Native Kubernetes API YAML (kubectl get -o yaml) is accepted in addition to
// the opsgraph dialect (top-level deployments:/events:). No k8s.io / client-go.

type nativeObject struct {
	Kind              string           `yaml:"kind"`
	APIVersion        string           `yaml:"apiVersion"`
	Metadata          nativeObjectMeta `yaml:"metadata"`
	Spec              nativeDepSpec    `yaml:"spec"`
	Status            nativeDepStatus  `yaml:"status"`
	InvolvedObject    nativeObjectRef  `yaml:"involvedObject"`
	Reason            string           `yaml:"reason"`
	Message           string           `yaml:"message"`
	Type              string           `yaml:"type"`
	LastTimestamp     any              `yaml:"lastTimestamp"`
	FirstTimestamp    any              `yaml:"firstTimestamp"`
	EventTime         any              `yaml:"eventTime"`
	CreationTimestamp any              `yaml:"creationTimestamp"`
	Items             []nativeObject   `yaml:"items"`
}

type nativeObjectMeta struct {
	Name              string            `yaml:"name"`
	Namespace         string            `yaml:"namespace"`
	Labels            map[string]string `yaml:"labels"`
	CreationTimestamp any               `yaml:"creationTimestamp"`
}

type nativeDepSpec struct {
	Replicas *int `yaml:"replicas"`
}

type nativeDepStatus struct {
	Replicas      int               `yaml:"replicas"`
	ReadyReplicas int               `yaml:"readyReplicas"`
	Conditions    []nativeCondition `yaml:"conditions"`
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

func loadK8sSnapshot(fsys fs.FS, depFile, evFile string) (k8sDeployments, k8sEvents, error) {
	var deps k8sDeployments
	var evs k8sEvents
	seen := map[string]bool{}
	appendFile := func(name string) error {
		if name == "" || seen[name] {
			return nil
		}
		seen[name] = true
		d, e, found, err := loadK8sFile(fsys, name)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		deps.Deployments = append(deps.Deployments, d.Deployments...)
		evs.Events = append(evs.Events, e.Events...)
		return nil
	}
	if err := appendFile(depFile); err != nil {
		return k8sDeployments{}, k8sEvents{}, err
	}
	if err := appendFile(evFile); err != nil {
		return k8sDeployments{}, k8sEvents{}, err
	}
	fillMissingDeploymentServiceIDs(&deps)
	return deps, evs, nil
}

func loadK8sFile(fsys fs.FS, name string) (k8sDeployments, k8sEvents, bool, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return k8sDeployments{}, k8sEvents{}, false, nil
		}
		return k8sDeployments{}, k8sEvents{}, false, fmt.Errorf("read %s: %w", name, err)
	}
	if looksNativeK8s(data) {
		d, e, err := parseNativeK8s(data)
		if err != nil {
			return k8sDeployments{}, k8sEvents{}, false, fmt.Errorf("parse native k8s %s: %w", name, err)
		}
		return d, e, true, nil
	}
	var deps k8sDeployments
	if err := yaml.Unmarshal(data, &deps); err != nil {
		return k8sDeployments{}, k8sEvents{}, false, fmt.Errorf("parse %s: %w", name, err)
	}
	var evs k8sEvents
	if err := yaml.Unmarshal(data, &evs); err != nil {
		return k8sDeployments{}, k8sEvents{}, false, fmt.Errorf("parse %s: %w", name, err)
	}
	return deps, evs, true, nil
}

func fillMissingDeploymentServiceIDs(deps *k8sDeployments) {
	if deps == nil {
		return
	}
	for i := range deps.Deployments {
		if strings.TrimSpace(deps.Deployments[i].ServiceID) != "" {
			continue
		}
		deps.Deployments[i].ServiceID = inferServiceID("Deployment", deps.Deployments[i].Name, nil)
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
		switch strings.ToLower(strings.TrimSpace(probe.Kind)) {
		case "list", "deployment", "event":
			sawNative = true
		}
	}
	return sawNative
}

func parseNativeK8s(data []byte) (k8sDeployments, k8sEvents, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var deps k8sDeployments
	var evs k8sEvents
	for {
		var obj nativeObject
		if err := dec.Decode(&obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return k8sDeployments{}, k8sEvents{}, err
		}
		collectNative(&deps, &evs, obj)
	}
	return deps, evs, nil
}

func collectNative(deps *k8sDeployments, evs *k8sEvents, obj nativeObject) {
	switch strings.ToLower(strings.TrimSpace(obj.Kind)) {
	case "list":
		for _, item := range obj.Items {
			collectNative(deps, evs, item)
		}
	case "deployment":
		if d, ok := nativeToDeployment(obj); ok {
			deps.Deployments = append(deps.Deployments, d)
		}
	case "event":
		if e, ok := nativeToEvent(obj); ok {
			evs.Events = append(evs.Events, e)
		}
	}
}

func nativeToDeployment(o nativeObject) (k8sDeployment, bool) {
	name := strings.TrimSpace(o.Metadata.Name)
	if name == "" {
		return k8sDeployment{}, false
	}
	desired := 1
	if o.Spec.Replicas != nil {
		desired = *o.Spec.Replicas
	}
	ns := strings.TrimSpace(o.Metadata.Namespace)
	if ns == "" {
		ns = "default"
	}
	return k8sDeployment{
		Name:      name,
		Namespace: ns,
		ServiceID: inferServiceID("Deployment", name, o.Metadata.Labels),
		Desired:   desired,
		Ready:     o.Status.ReadyReplicas,
		UpdatedAt: deploymentUpdatedAt(o),
	}, true
}

func nativeToEvent(o nativeObject) (k8sEvent, bool) {
	kind := o.InvolvedObject.Kind
	name := o.InvolvedObject.Name
	if strings.TrimSpace(name) == "" {
		name = o.Metadata.Name
		kind = o.Kind
	}
	sid := inferServiceID(kind, name, o.Metadata.Labels)
	if sid == "" {
		return k8sEvent{}, false
	}
	ns := strings.TrimSpace(o.Metadata.Namespace)
	if ns == "" {
		ns = strings.TrimSpace(o.InvolvedObject.Namespace)
	}
	at := firstK8sTime(o.LastTimestamp, o.EventTime, o.FirstTimestamp, o.Metadata.CreationTimestamp, o.CreationTimestamp)
	return k8sEvent{
		ServiceID: sid,
		Namespace: ns,
		At:        at,
		Reason:    o.Reason,
		Message:   o.Message,
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
	for _, k := range []string{"app.kubernetes.io/name", "app"} {
		if v := strings.TrimSpace(labels[k]); v != "" {
			return v
		}
	}
	name = strings.TrimSpace(name)
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "pod":
		name = stripK8sHashSuffix(stripK8sHashSuffix(name))
	case "replicaset":
		name = stripK8sHashSuffix(name)
	}
	return name
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
