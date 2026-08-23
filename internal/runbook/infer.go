package runbook

import (
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/model"
)

// ParseWithInfer parses a runbook then fills unmarked steps from wording.
// Explicit opsgraph:check= annotations always win.
func ParseWithInfer(data []byte, path string) (model.Runbook, FrontMatter, error) {
	rb, fm, err := Parse(data, path)
	if err != nil {
		return rb, fm, err
	}
	InferChecks(&rb)
	return rb, fm, nil
}

// InferChecks fills empty step checks from step text. Annotated steps are left
// alone so fixture goldens stay stable.
func InferChecks(rb *model.Runbook) {
	if rb == nil {
		return
	}
	for i := range rb.Steps {
		if strings.TrimSpace(rb.Steps[i].Check) != "" {
			continue
		}
		rb.Steps[i].Check = inferCheck(rb.ServiceID, rb.Steps[i].Text)
	}
}

func inferCheck(serviceID, text string) string {
	t := strings.ToLower(text)
	svc := strings.TrimSpace(serviceID)
	switch {
	case containsWordish(t, "unhealthy", "not healthy", "still down"):
		if svc != "" {
			return "service_unhealthy:" + svc
		}
	case containsWordish(t, "healthy", "healthz", "recovered", "recover"):
		if svc != "" {
			return "service_healthy:" + svc
		}
	case containsWordish(t, "deploy", "rollout", "release"):
		return "deploy_age_lt:60m"
	}
	return "manual"
}

func containsWordish(haystack string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}
