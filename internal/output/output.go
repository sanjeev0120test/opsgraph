// Package output renders ask/verify results as deterministic JSON or a
// human-readable table. Only JSON is used for golden comparisons.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// JSON writes res as indented, deterministic JSON with a trailing newline.
// HTML characters (<, >, &) are left literal so goldens stay human-readable.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table writes a compact human-readable summary of an AskResult.
func Table(w io.Writer, res model.AskResult) error {
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	p("SERVICE   %s (%s)", res.Service.ID, res.Service.Health)
	if res.Owner != nil {
		p("   owner: %s", ownerLine(res.Owner))
	}
	p("\n")
	p("WINDOW    last %s (as of %s; times are UTC)\n", res.Window, res.GeneratedAt.UTC().Format(time.RFC3339))

	if len(res.Changes) == 0 {
		p("CHANGES   none in window\n")
	}
	for i, c := range res.Changes {
		label := "CHANGES"
		if i > 0 {
			label = "       "
		}
		rev := c.Revision
		if rev == "" {
			rev = c.ID
		}
		p("%s   %s  %s ago  %s  %q  [%s]\n", label, c.Type, ago(res.GeneratedAt, c.At), rev, c.Summary, c.EvidenceID)
	}

	if len(res.Alerts) == 0 {
		p("ALERTS    none in window\n")
	}
	for i, a := range res.Alerts {
		label := "ALERTS"
		if i > 0 {
			label = "      "
		}
		p("%s    %s (%s, %s)  [%s]\n", label, a.Name, a.Severity, a.Status, a.EvidenceID)
	}

	p("BLAST     upstream: %s   downstream: %s\n", svcList(res.Upstream), svcList(res.Downstream))

	if len(res.Correlations) > 0 {
		for i, c := range res.Correlations {
			label := "LINKED"
			if i > 0 {
				label = "      "
			}
			p("%s    %s", label, c.Summary)
			if c.ChangeEvidence != "" {
				p(" [%s]", c.ChangeEvidence)
			}
			if c.AlertEvidence != "" {
				p(" [%s]", c.AlertEvidence)
			}
			p("\n")
		}
	}

	if res.RunbookResult != nil {
		rb := res.RunbookResult
		p("RUNBOOK   %s -> %s (%s)\n", rb.Path, strings.ToUpper(rb.Status), stepTally(rb.Steps))
		for _, s := range rb.Steps {
			p("          %d. [%-6s] %s\n", s.Number, s.Status, s.Text)
		}
	} else {
		p("RUNBOOK   (none)\n")
	}

	if len(res.Timeline) == 0 {
		p("TIMELINE  (none)\n")
	} else {
		p("TIMELINE\n")
		limit := len(res.Timeline)
		truncated := false
		if limit > 8 {
			limit = 8
			truncated = true
		}
		for _, e := range res.Timeline[:limit] {
			p("          %s  %-8s %s", e.At.Format(time.RFC3339), e.Kind, e.Summary)
			if e.EvidenceID != "" {
				p(" [%s]", e.EvidenceID)
			}
			p("\n")
		}
		if truncated {
			p("          ... +%d more\n", len(res.Timeline)-limit)
		}
	}
	if len(res.Evidence) > 0 {
		p("EVIDENCE\n")
		for _, e := range res.Evidence {
			p("          %s  %s\n", e.ID, e.Summary)
		}
	}

	p("NEXT\n")
	if len(res.Recommendations) == 0 {
		p("          (none)\n")
	}
	for i, r := range res.Recommendations {
		p("          %d. %s\n", i+1, r)
	}
	if res.Service.ID != "" {
		p("          cmds: opsgraph blast %s · opsgraph impact %s · opsgraph handoff %s\n",
			res.Service.ID, res.Service.ID, res.Service.ID)
	}

	if res.AISummary != "" {
		p("AI\n          %s\n", indentLines(res.AISummary, "          "))
	}
	return nil
}

// VerifyTable writes a human-readable runbook verification result.
func VerifyTable(w io.Writer, vr model.VerifyResult) error {
	fmt.Fprintf(w, "RUNBOOK %s (service %s) -> %s\n", vr.Path, vr.ServiceID, strings.ToUpper(vr.Status))
	for _, s := range vr.Steps {
		fmt.Fprintf(w, "  %d. [%-6s] %s\n", s.Number, s.Status, s.Text)
		if s.Message != "" {
			fmt.Fprintf(w, "        %s\n", s.Message)
		}
	}
	return nil
}

func ownerLine(o *model.Owner) string {
	s := o.Name
	if s == "" {
		s = o.ID
	}
	if o.Email != "" {
		s += " <" + o.Email + ">"
	}
	return s
}

func svcList(svcs []model.Service) string {
	if len(svcs) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(svcs))
	for _, s := range svcs {
		parts = append(parts, s.ID+" ("+s.Health+")")
	}
	return strings.Join(parts, ", ")
}

func stepTally(steps []model.StepVerifyResult) string {
	counts := map[string]int{}
	for _, s := range steps {
		counts[s.Status]++
	}
	order := []string{model.StatusPass, model.StatusFail, model.StatusStale, model.StatusManual, model.StatusError}
	var parts []string
	for _, k := range order {
		if counts[k] > 0 {
			parts = append(parts, strconv.Itoa(counts[k])+" "+k)
		}
	}
	return strings.Join(parts, ", ")
}

func ago(now, then time.Time) string {
	if then.IsZero() || now.IsZero() {
		return "?"
	}
	d := now.Sub(then)
	if d < 0 {
		return "0s"
	}
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	return strconv.Itoa(int(d.Hours())) + "h"
}

func indentLines(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return strings.Join(lines, "\n"+prefix)
}
