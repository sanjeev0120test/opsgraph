package main

import (
	"path/filepath"
	"testing"
)

// TestCoreExitCodeMatrix freezes the PLAN exit-code contract in one table so
// scattered smoke paths cannot silently drift.
func TestCoreExitCodeMatrix(t *testing.T) {
	t.Setenv("OPSGRAPH_FIXTURE", "")
	t.Setenv("OPSGRAPH_DATA_DIR", "")
	t.Setenv("OPSGRAPH_CONFIG", "")

	fx := fixtureDir(t)
	dir := t.TempDir()
	fleet := filepath.Join(repoRoot(t), "fixtures", "fleet_healthy")

	cases := []struct {
		name string
		args []string
		want int
	}{
		{"ask_ok", []string{"ask", "checkout", "--fixture", fx, "--format", "json"}, 0},
		{"ask_hottest", []string{"ask", "--fixture", fx, "--format", "json"}, 0},
		{"ask_unknown", []string{"ask", "nosuch", "--fixture", fx}, 1},
		{"ask_no_source", []string{"ask", "checkout"}, 2},
		{"ask_no_args_no_source", []string{"ask"}, 2},
		{"ask_fixture_data_dir", []string{"ask", "checkout", "--fixture", fx, "--data-dir", dir}, 2},
		{"ask_negative_since", []string{"ask", "checkout", "--fixture", fx, "--since", "-1m"}, 2},
		{"verify_stale", []string{"verify-runbook", "checkout", "--fixture", fx}, 1},
		{"verify_pass", []string{"verify-runbook", "auth", "--fixture", fx}, 0},
		{"test_ok", []string{"test", fx}, 0},
		{"pack_ok", []string{"pack", "--fixture", fx, "--out", filepath.Join(dir, "pack"), "--format", "json"}, 0},
		{"prove_ok", []string{"prove", "--format", "json"}, 0},
		{"root_start", []string{}, 0},
		{"receipt_ok", []string{"receipt", "--fixture", fx, "--format", "json"}, 0},
		{"open_ok", []string{"open", fx, "--format", "json"}, 0},
		{"delta_same", []string{"delta", fx, fx, "--format", "json"}, 0},
		{"delta_diff", []string{"delta", fx, fleet, "--format", "json"}, 1},
		{"demo_ok", []string{"demo"}, 0},
		{"status_empty", []string{"status", "--data-dir", dir}, 1},
		{"health_strict_hot", []string{"health", "--fixture", fx, "--strict"}, 1},
		{"health_strict_ok", []string{"health", "--fixture", fleet, "--strict"}, 0},
		{"watch_once_degraded", []string{"watch", "checkout", "--fixture", fx, "--once"}, 1},
		{"watch_once_healthy", []string{"watch", "order", "--fixture", fx, "--once"}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, code := runRoot(t, tc.args...)
			if code != tc.want {
				t.Fatalf("exit = %d, want %d (args=%v)", code, tc.want, tc.args)
			}
		})
	}
}
