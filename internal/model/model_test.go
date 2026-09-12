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

func TestIsGitOnlyUnknown(t *testing.T) {
	if !IsGitOnlyUnknown(Service{ID: "foo", Health: HealthUnknown, Sources: []string{"git"}}) {
		t.Fatal("git-only unknown must be skipped by hottest")
	}
	if IsGitOnlyUnknown(Service{ID: "co", Health: HealthDegraded, Sources: []string{"git", "kubernetes"}}) {
		t.Fatal("real k8s+git service is not git-only")
	}
}

func TestIsNoiseForPaging(t *testing.T) {
	if !IsNoiseForPaging(Service{ID: "redis", Health: HealthUnknown, Sources: []string{"dependency"}}) {
		t.Fatal("stub is noise")
	}
	if !IsNoiseForPaging(Service{ID: "internal", Health: HealthUnknown, Sources: []string{"git"}}) {
		t.Fatal("git-only unknown is noise")
	}
	if IsNoiseForPaging(Service{ID: "checkout", Health: HealthHealthy, Sources: []string{"kubernetes"}}) {
		t.Fatal("app is not noise")
	}
}

func TestIsSystemAgent(t *testing.T) {
	if !IsSystemAgent("fluentbit") || !IsSystemAgent("calico-node") {
		t.Fatal("agents")
	}
	if IsSystemAgent("checkout") {
		t.Fatal("checkout is an app")
	}
}

func TestHealthConstants(t *testing.T) {
	for _, h := range []string{HealthHealthy, HealthDegraded, HealthUnhealthy, HealthUnknown} {
		if h == "" {
			t.Fatal("empty health constant")
		}
	}
}
