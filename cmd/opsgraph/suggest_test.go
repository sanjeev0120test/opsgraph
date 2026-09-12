package main

import "testing"

func TestSuggestServices(t *testing.T) {
	names := []string{"checkout", "auth", "order"}
	got := suggestServices("checkout-api", names)
	if len(got) == 0 || got[0] != "checkout" {
		t.Fatalf("pager typo should suggest checkout: %v", got)
	}
	got = suggestServices("chekout", names)
	if len(got) == 0 || got[0] != "checkout" {
		t.Fatalf("edit distance should suggest checkout: %v", got)
	}
	if got := suggestServices("payments", names); len(got) != 0 {
		t.Fatalf("unrelated must not suggest: %v", got)
	}
}

func TestIsBrokenK8sSnapshot(t *testing.T) {
	if !isBrokenK8sSnapshot(errString("kubernetes snapshot: no such file")) {
		t.Fatal("missing dump must be flagged")
	}
	if isBrokenK8sSnapshot(errString("prometheus connector: timeout")) {
		t.Fatal("prom fail is not a broken dump")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
