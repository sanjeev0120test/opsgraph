package ask

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// SuspectChangeWindow is the fixed lookback for R1 / explain / why / score /
// fingerprint. Independent of a *wider* --since so an old change is not
// falsely elevated; ChangeLookback still floors narrow --since.
const SuspectChangeWindow = 30 * time.Minute

// Kept for call sites inside this package.
const r1ChangeWindow = SuspectChangeWindow

// ChangeLookback returns the effective change query window: at least
// correlateWindow (60m) so R1 and change→alert links stay accurate when
// callers pass a narrower --since.
func ChangeLookback(since time.Duration) time.Duration {
	if since <= 0 {
		since = time.Hour
	}
	if since < correlateWindow {
		return correlateWindow
	}
	return since
}

const r6Handoff = "Write a short handoff note with evidence IDs before closing the incident."

// RecentSuspectChange returns the newest change within SuspectChangeWindow of
// GeneratedAt, or false when none qualify. Used by explain/why/score/fingerprint
// so they agree with recommendation R1.
func RecentSuspectChange(res model.AskResult) (model.Change, bool) {
	return recentChange(res)
}

// recommend produces deterministic, ordered next-step recommendations (R1–R6+).
func recommend(res model.AskResult) []string {
	var recs []string

	// R1: most recent change within a fixed 30m threshold is the prime suspect.
	r1Fired := false
	if c, ok := recentChange(res); ok {
		r1Fired = true
		ref := c.Revision
		if ref == "" {
			ref = c.ID
		}
		recs = append(recs, fmt.Sprintf("Inspect the most recent %s first: %q (%s).", changeNoun(c.Type), c.Summary, ref))
	}
	if len(res.Correlations) > 0 {
		c := res.Correlations[0]
		recs = append(recs, fmt.Sprintf("Correlate change→alert: %s.", c.Summary))
	}

	// R1b: queried service itself is unhealthy/degraded.
	if res.Service.Health == model.HealthDegraded || res.Service.Health == model.HealthUnhealthy {
		recs = append(recs, healthRecommendation(res.Service))
	}

	// R2: unhealthy upstreams block safe changes (id order from Upstream).
	for _, u := range res.Upstream {
		if u.Health == model.HealthDegraded || u.Health == model.HealthUnhealthy {
			recs = append(recs, fmt.Sprintf("Check upstream %s (%s) before changing %s.", u.ID, u.Health, res.Service.ID))
		}
	}

	// R3: firing/pending alerts need acknowledgement.
	if fa := firstActiveAlert(res.Alerts); fa != nil {
		label := fa.Status
		if label == "" {
			label = "firing"
		}
		if r1Fired {
			recs = append(recs, fmt.Sprintf("Acknowledge %s alert %s and correlate it with the recent change.", label, fa.Name))
		} else {
			recs = append(recs, fmt.Sprintf("Acknowledge %s alert %s and investigate related signals.", label, fa.Name))
		}
	}

	// R4: a stale/failed runbook must be fixed.
	if rb := res.RunbookResult; rb != nil && (rb.Status == model.StatusStale || rb.Status == model.StatusFail) {
		steps := offendingSteps(rb.Steps)
		recs = append(recs, fmt.Sprintf("Runbook %s is %s - review step(s) %s.", rb.Path, rb.Status, steps))
	}

	// R5: loop in the owner when known; otherwise ask to assign one.
	if res.Owner != nil {
		who := res.Owner.Name
		if who == "" {
			who = res.Owner.ID
		}
		if res.Owner.Email != "" {
			who += " <" + res.Owner.Email + ">"
		}
		recs = append(recs, fmt.Sprintf("Notify owner %s.", who))
	} else if res.Service.ID != "" {
		recs = append(recs, fmt.Sprintf("Assign an owner for %s.", res.Service.ID))
	}

	// R6: always appended so every answer ends with a stable handoff step.
	recs = append(recs, r6Handoff)
	return recs
}

func healthRecommendation(svc model.Service) string {
	if note := model.RolloutNote(svc); note != "" {
		return fmt.Sprintf("Investigate %s health (%s; %s) and stabilize before further changes.", svc.ID, svc.Health, note)
	}
	return fmt.Sprintf("Investigate %s health (%s) and stabilize before further changes.", svc.ID, svc.Health)
}

func recentChange(res model.AskResult) (model.Change, bool) {
	if len(res.Changes) == 0 || res.GeneratedAt.IsZero() {
		return model.Change{}, false
	}
	// Changes are newest-first; skip future/zero/stale entries so a skew row
	// cannot hide a valid suspect further down the list.
	for _, c := range res.Changes {
		if c.At.IsZero() || c.At.After(res.GeneratedAt) {
			continue
		}
		if res.GeneratedAt.Sub(c.At) > r1ChangeWindow {
			continue
		}
		return c, true
	}
	return model.Change{}, false
}

func changeNoun(t string) string {
	switch t {
	case "deploy":
		return "deploy"
	case "rollout":
		return "rollout"
	case "commit":
		return "commit"
	default:
		return "change"
	}
}

func firstActiveAlert(alerts []model.Alert) *model.Alert {
	for i := range alerts {
		switch alerts[i].Status {
		case "firing", "pending":
			return &alerts[i]
		}
	}
	return nil
}

func offendingSteps(steps []model.StepVerifyResult) string {
	var nums []string
	for _, s := range steps {
		if s.Status == model.StatusStale || s.Status == model.StatusFail || s.Status == model.StatusError {
			nums = append(nums, strconv.Itoa(s.Number))
		}
	}
	if len(nums) == 0 {
		return "-"
	}
	return strings.Join(nums, ", ")
}
