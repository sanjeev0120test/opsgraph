// Package score computes a deterministic incident severity score from AskResult.
package score

import (
	"sort"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// Result is a 0–100 severity score with a stable breakdown.
type Result struct {
	Score      int            `json:"score"`
	Level      string         `json:"level"` // low|medium|high|critical
	Breakdown  map[string]int `json:"breakdown"`
	Highlights []string       `json:"highlights"`
}

// Compute returns a deterministic severity score for an ask result.
func Compute(res model.AskResult) Result {
	b := map[string]int{}
	highlights := []string{}

	switch res.Service.Health {
	case model.HealthUnhealthy:
		b["service_health"] = 40
		highlights = append(highlights, "service is unhealthy")
	case model.HealthDegraded:
		b["service_health"] = 25
		highlights = append(highlights, "service is degraded")
	case model.HealthUnknown:
		b["service_health"] = 10
	}

	seenAlert := map[string]bool{}
	for _, a := range res.Alerts {
		if !model.AlertActive(a.Status) {
			continue
		}
		key := a.Name + ":" + a.Severity
		if seenAlert[key] {
			continue // distinct alert names — series fan-out must not inflate score
		}
		seenAlert[key] = true
		pts := 15
		if a.Severity == "critical" {
			pts = 25
		} else if a.Severity == "warning" {
			pts = 18
		}
		if a.Status == "pending" {
			pts = pts * 2 / 3 // pending is not yet paging
		}
		b["firing_alerts"] += pts
		highlights = append(highlights, a.Status+" alert "+a.Name)
	}
	if b["firing_alerts"] > 35 {
		b["firing_alerts"] = 35
	}

	if _, ok := ask.RecentSuspectChange(res); ok {
		b["recent_change"] = 15
		highlights = append(highlights, "recent change in suspect window")
	}
	if len(res.Correlations) > 0 {
		b["change_alert_link"] = 10
		highlights = append(highlights, "change preceded firing alert")
	}

	unhealthyUp := 0
	for _, u := range res.Upstream {
		if u.Health == model.HealthUnhealthy || u.Health == model.HealthDegraded {
			unhealthyUp++
		}
	}
	if unhealthyUp > 0 {
		b["unhealthy_upstream"] = min(20, unhealthyUp*10)
		highlights = append(highlights, "unhealthy upstream dependency")
	}
	badDown := 0
	for _, d := range res.Downstream {
		if d.Health == model.HealthUnhealthy || d.Health == model.HealthDegraded {
			badDown++
		}
	}
	if badDown > 0 {
		b["downstream_impact"] = min(15, badDown*5)
		highlights = append(highlights, "unhealthy downstream dependent")
	} else if n := len(res.Downstream); n >= 3 {
		// Wide blast still matters even when neighbors look healthy.
		b["downstream_impact"] = min(10, n)
		highlights = append(highlights, "wide downstream blast radius")
	}

	if res.RunbookResult != nil {
		switch res.RunbookResult.Status {
		case model.StatusFail:
			b["runbook"] = 10
			highlights = append(highlights, "runbook failing")
		case model.StatusStale:
			b["runbook"] = 5
			highlights = append(highlights, "runbook stale")
		}
	}

	total := 0
	for _, v := range b {
		total += v
	}
	if total > 100 {
		total = 100
	}
	sort.Strings(highlights)
	return Result{Score: total, Level: level(total), Breakdown: b, Highlights: highlights}
}

func level(score int) string {
	switch {
	case score >= 75:
		return "critical"
	case score >= 50:
		return "high"
	case score >= 25:
		return "medium"
	default:
		return "low"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
