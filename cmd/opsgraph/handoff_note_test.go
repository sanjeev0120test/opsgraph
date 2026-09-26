package main

import (
	"strings"
	"testing"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

func TestHandoffNamesRolloutWhenNoRecentChange(t *testing.T) {
	res := model.AskResult{
		Service: model.Service{
			ID:     "checkout",
			Health: model.HealthDegraded,
			Labels: map[string]string{
				"opsgraph_rollout": "rollout deadline exceeded\nsecond line",
			},
		},
		GeneratedAt:     time.Date(2026, 9, 26, 17, 0, 0, 0, time.UTC),
		Window:          "60m",
		Recommendations: []string{"Investigate checkout health (degraded; rollout deadline exceeded second line) and stabilize before further changes."},
	}
	note := handoffNote(res)
	if !strings.Contains(note, "Health: **degraded** (rollout deadline exceeded second line)") {
		t.Fatalf("health line:\n%s", note)
	}
	if !strings.Contains(note, "- Rollout: rollout deadline exceeded second line") {
		t.Fatalf("what happened:\n%s", note)
	}
	if strings.Contains(note, "\nsecond line") {
		t.Fatalf("note must stay one line:\n%s", note)
	}
	if !strings.Contains(note, "No change inside the 30m suspect window.") {
		t.Fatalf("missing empty-change line:\n%s", note)
	}
}
