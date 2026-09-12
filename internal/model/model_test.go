package model

import "testing"

func TestAlertActive(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"firing", true},
		{"pending", true},
		{"resolved", false},
		{"suppressed", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := AlertActive(tc.status); got != tc.want {
			t.Fatalf("AlertActive(%q)=%v want %v", tc.status, got, tc.want)
		}
	}
}

func TestAlertLive(t *testing.T) {
	cases := []struct {
		status string
		want   bool
	}{
		{"firing", true},
		{"pending", true},
		{"suppressed", true},
		{"resolved", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := AlertLive(tc.status); got != tc.want {
			t.Fatalf("AlertLive(%q)=%v want %v", tc.status, got, tc.want)
		}
	}
}

func TestIsDependencyStub(t *testing.T) {
	if !IsDependencyStub(Service{ID: "redis", Sources: []string{"dependency"}}) {
		t.Fatal("dependency-only must be a stub")
	}
	if IsDependencyStub(Service{ID: "checkout", Sources: []string{"kubernetes", "dependency"}}) {
		t.Fatal("real workload with a stub source is not a stub")
	}
	if IsDependencyStub(Service{ID: "api", Sources: []string{"fixture"}}) {
		t.Fatal("fixture service is not a stub")
	}
	if IsDependencyStub(Service{ID: "x"}) {
		t.Fatal("empty sources is not a stub")
	}
}

func TestHealthConstants(t *testing.T) {
	for _, h := range []string{HealthHealthy, HealthDegraded, HealthUnhealthy, HealthUnknown} {
		if h == "" {
			t.Fatal("empty health constant")
		}
	}
}
