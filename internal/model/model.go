// Package model holds the core domain types shared across opsgraph.
package model

import (
	"strings"
	"time"
)

// Health values.
const (
	HealthHealthy   = "healthy"
	HealthDegraded  = "degraded"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// Service is a logical service in the incident graph.
type Service struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Aliases []string          `json:"aliases,omitempty"`
	OwnerID string            `json:"owner_id,omitempty"`
	Health  string            `json:"health"` // healthy|degraded|unhealthy|unknown
	Labels  map[string]string `json:"labels,omitempty"`
	Sources []string          `json:"sources,omitempty"`
}

// IsDependencyStub reports a synthesized edge endpoint with no real connector
// (fixture/k8s/git/plugin). Hottest/health --strict must not treat it as a
// paging service.
func IsDependencyStub(s Service) bool {
	if len(s.Sources) == 0 {
		return false
	}
	for _, src := range s.Sources {
		if src != "dependency" {
			return false
		}
	}
	return true
}

// IsGitOnlyUnknown is a folder inferred from git with no k8s/fixture health.
// Hottest must not prefer it over a healthy Deployment (unknown scores 10).
func IsGitOnlyUnknown(s Service) bool {
	if s.Health != HealthUnknown && s.Health != "" {
		return false
	}
	if len(s.Sources) == 0 {
		return false
	}
	for _, src := range s.Sources {
		if src != "git" {
			return false
		}
	}
	return true
}

// IsSystemAgent is a node agent / kube-system style name. Hottest prefers
// app services when any exist so `kubectl get -A` does not auto-select Fluent Bit.
func IsSystemAgent(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return false
	}
	switch id {
	case "fluentbit", "fluent-bit", "fluentd", "coredns", "kube-proxy",
		"calico-node", "cilium", "cilium-agent", "aws-node", "ebs-csi-node",
		"efs-csi-node", "nvidia-device-plugin", "datadog-agent", "node-exporter",
		"prometheus-node-exporter", "metrics-server":
		return true
	}
	for _, p := range []string{"calico-", "cilium-", "fluent-bit", "ebs-csi-", "aws-node"} {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// IsNoiseForPaging is a stub or git folder with no real health. Hottest, top,
// and health --strict must not treat it as a paging service.
func IsNoiseForPaging(s Service) bool {
	return IsDependencyStub(s) || IsGitOnlyUnknown(s)
}

// Owner is a team or person responsible for services.
type Owner struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Team  string `json:"team,omitempty"`
	Email string `json:"email,omitempty"`
}

// Change is something that happened to a service (commit, deploy, rollout).
type Change struct {
	ID         string    `json:"id"`
	ServiceID  string    `json:"service_id"`
	At         time.Time `json:"at"`
	Type       string    `json:"type"` // commit|deploy|rollout
	Summary    string    `json:"summary"`
	Author     string    `json:"author,omitempty"`
	Revision   string    `json:"revision,omitempty"`
	Source     string    `json:"source"` // git|kubernetes|fixture
	EvidenceID string    `json:"evidence_id"`
}

// Dependency is a directed edge: From depends on To (From calls To).
type Dependency struct {
	FromServiceID string `json:"from_service_id"`
	ToServiceID   string `json:"to_service_id"`
	Type          string `json:"type"` // http|grpc|queue|db|depends_on|unknown
	Source        string `json:"source"`
}

// AlertActive reports whether an alert status is actionable (firing or pending).
func AlertActive(status string) bool {
	return status == "firing" || status == "pending"
}

// AlertLive reports whether an alert is still live in the paging pipeline
// (actionable or silenced). Live alerts ignore --since / future-At filters.
func AlertLive(status string) bool {
	return AlertActive(status) || status == "suppressed"
}

// Alert is a firing, pending, or resolved alert linked to a service.
type Alert struct {
	ID         string    `json:"id"`
	ServiceID  string    `json:"service_id"`
	At         time.Time `json:"at"`
	Severity   string    `json:"severity"` // critical|warning|info
	Name       string    `json:"name"`
	Status     string    `json:"status"` // firing|pending|suppressed|resolved
	Summary    string    `json:"summary"`
	Source     string    `json:"source"`
	EvidenceID string    `json:"evidence_id"`
}

// Runbook is a parsed operational runbook for a service.
type Runbook struct {
	ID        string        `json:"id"`
	ServiceID string        `json:"service_id"`
	Path      string        `json:"path"`
	OwnerID   string        `json:"owner_id,omitempty"`
	Steps     []RunbookStep `json:"steps"`
}

// RunbookStep is a single numbered step with an optional check expression.
type RunbookStep struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
	Check  string `json:"check"` // raw check expression
}

// Evidence is a referenceable fact backing a change/alert/timeline entry.
type Evidence struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	At        time.Time `json:"at"`
	Kind      string    `json:"kind"`
	Summary   string    `json:"summary"`
	RawRef    string    `json:"raw_ref,omitempty"`
	ServiceID string    `json:"service_id,omitempty"`
}

// TimelineEvent is a point on the incident timeline.
type TimelineEvent struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"` // change|alert|health|runbook|k8s-event
	Summary    string    `json:"summary"`
	ServiceID  string    `json:"service_id,omitempty"`
	EvidenceID string    `json:"evidence_id,omitempty"`
	Severity   string    `json:"severity,omitempty"`
}

// Correlation links a preceding change to a later active alert when the gap
// is short enough to be causally plausible (deterministic, evidence-backed).
type Correlation struct {
	Kind           string `json:"kind"` // change_then_alert
	Summary        string `json:"summary"`
	ChangeID       string `json:"change_id,omitempty"`
	ChangeEvidence string `json:"change_evidence_id,omitempty"`
	AlertID        string `json:"alert_id,omitempty"`
	AlertEvidence  string `json:"alert_evidence_id,omitempty"`
	Gap            string `json:"gap"` // e.g. "7m"
}

// AskResult is the full answer for `opsgraph ask [service]`.
type AskResult struct {
	Service         Service         `json:"service"`
	Owner           *Owner          `json:"owner,omitempty"`
	Window          string          `json:"window"`
	GeneratedAt     time.Time       `json:"generated_at"`
	Changes         []Change        `json:"changes"`
	Alerts          []Alert         `json:"alerts"`
	Upstream        []Service       `json:"upstream"`
	Downstream      []Service       `json:"downstream"`
	RunbookResult   *VerifyResult   `json:"runbook,omitempty"`
	Timeline        []TimelineEvent `json:"timeline"`
	Correlations    []Correlation   `json:"correlations"`
	Recommendations []string        `json:"recommendations"`
	Evidence        []Evidence      `json:"evidence"`
	AISummary       string          `json:"ai_summary,omitempty"`
}

// VerifyResult is the result of verifying a runbook.
type VerifyResult struct {
	ServiceID string             `json:"service_id"`
	Path      string             `json:"path"`
	Status    string             `json:"status"` // pass|fail|stale|missing|manual
	Steps     []StepVerifyResult `json:"steps"`
}

// StepVerifyResult is the result of evaluating one runbook step.
type StepVerifyResult struct {
	Number     int    `json:"number"`
	Text       string `json:"text"`
	Check      string `json:"check"`
	Status     string `json:"status"` // pass|fail|stale|manual|error
	Message    string `json:"message"`
	EvidenceID string `json:"evidence_id,omitempty"`
}

// Verify status constants.
const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusStale   = "stale"
	StatusManual  = "manual"
	StatusMissing = "missing"
	StatusError   = "error"
)
