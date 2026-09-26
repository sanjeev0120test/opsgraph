package output_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
)

func TestTableEmptyStates(t *testing.T) {
	var buf bytes.Buffer
	res := model.AskResult{
		Service:     model.Service{ID: "x", Health: model.HealthHealthy},
		Window:      "60m",
		GeneratedAt: mustParseTime(t, "2026-07-31T12:00:00Z"),
	}
	if err := output.Table(&buf, res); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"RUNBOOK   (none)", "TIMELINE  (none)", "NEXT\n          (none)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestTableShowsRolloutNote(t *testing.T) {
	var buf bytes.Buffer
	res := model.AskResult{
		Service: model.Service{
			ID:     "checkout",
			Health: model.HealthDegraded,
			Labels: map[string]string{"opsgraph_rollout": "rollout deadline exceeded"},
		},
		Window:      "60m",
		GeneratedAt: mustParseTime(t, "2026-07-31T12:00:00Z"),
	}
	if err := output.Table(&buf, res); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "SERVICE   checkout (degraded; rollout deadline exceeded)") {
		t.Fatalf("table = %s", buf.String())
	}
}

func TestJSONDoesNotEscapeHTML(t *testing.T) {
	var buf bytes.Buffer
	v := model.AskResult{
		Service: model.Service{ID: "x", Name: "x"},
		Owner:   &model.Owner{ID: "o", Name: "Team", Email: "a@b.com"},
		Window:  "60m",
		Recommendations: []string{
			"Notify owner Team <a@b.com>.",
		},
	}
	if err := output.JSON(&buf, v); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u003e`) {
		t.Fatalf("HTML should not be escaped: %s", got)
	}
	if !strings.Contains(got, "<a@b.com>") {
		t.Fatalf("expected literal angle brackets: %s", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Fatal("JSON output must end with newline")
	}
}

func TestJSONDeterministic(t *testing.T) {
	v := model.AskResult{
		Service:         model.Service{ID: "checkout", Health: model.HealthDegraded},
		Window:          "60m",
		GeneratedAt:     mustParseTime(t, "2026-07-31T12:00:00Z"),
		Recommendations: []string{"a", "b"},
	}
	var a, b bytes.Buffer
	if err := output.JSON(&a, v); err != nil {
		t.Fatal(err)
	}
	if err := output.JSON(&b, v); err != nil {
		t.Fatal(err)
	}
	if a.String() != b.String() {
		t.Fatalf("JSON not deterministic:\n%s\n---\n%s", a.String(), b.String())
	}
}
