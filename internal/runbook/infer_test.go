package runbook_test

import (
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/runbook"
)

func TestInferChecksFromWording(t *testing.T) {
	md := []byte(`---
service: checkout
---

1. A deploy in the last hour is the most likely trigger.
2. Confirm checkout has recovered and is healthy before closing.
3. Verify the service is currently unhealthy.
4. Page the on-call and open a channel.
`)
	rb, _, err := runbook.ParseWithInfer(md, "runbooks/checkout.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.Steps) != 4 {
		t.Fatalf("steps = %d", len(rb.Steps))
	}
	want := []string{
		"deploy_age_lt:60m",
		"service_healthy:checkout",
		"service_unhealthy:checkout",
		"manual",
	}
	for i, w := range want {
		if rb.Steps[i].Check != w {
			t.Errorf("step %d check = %q, want %q", i+1, rb.Steps[i].Check, w)
		}
	}
}

func TestInferChecksDoesNotOverrideAnnotations(t *testing.T) {
	md := []byte(`---
service: auth
---

1. Verify auth is currently unhealthy (this is the upstream cause).
<!-- opsgraph:check=service_unhealthy:auth -->

2. Escalate.
<!-- opsgraph:check=manual -->
`)
	rb, _, err := runbook.ParseWithInfer(md, "runbooks/auth.md")
	if err != nil {
		t.Fatal(err)
	}
	if rb.Steps[0].Check != "service_unhealthy:auth" || rb.Steps[1].Check != "manual" {
		t.Fatalf("overrode annotations: %+v", rb.Steps)
	}
}

func TestParseStaysLiteralWithoutInfer(t *testing.T) {
	md := []byte("---\nservice: x\n---\n\n1. Confirm a recent deploy.\n")
	rb, _, err := runbook.Parse(md, "r.md")
	if err != nil {
		t.Fatal(err)
	}
	if rb.Steps[0].Check != "" {
		t.Fatalf("Parse must not infer, got %q", rb.Steps[0].Check)
	}
}
