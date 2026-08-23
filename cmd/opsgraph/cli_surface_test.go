package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestCLICommandSurfaceFrozen(t *testing.T) {
	root := newRootCmd()
	got := commandNames(root)
	want := []string{
		"alerts", "ask", "blast", "changes", "compare", "completion", "demo",
		"doctor", "evidence", "explain", "export", "fingerprint", "graph",
		"handoff", "health", "impact", "ingest", "init", "owners", "pack", "path", "prove", "report",
		"resolve", "score", "services", "status", "test", "timeline", "top",
		"validate-fixture", "verify-runbook", "version", "watch", "who", "why",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CLI command surface drifted\n got: %v\nwant: %v", got, want)
	}
}

func TestCLICriticalFlagsFrozen(t *testing.T) {
	root := newRootCmd()
	cases := map[string][]string{
		"ask":    {"ai", "config", "data-dir", "fixture", "format", "since"},
		"watch":  {"config", "data-dir", "fixture", "format", "interval", "once", "timeout"},
		"export": {"config", "data-dir", "fixture", "format", "meta", "out"},
		"health": {"config", "data-dir", "fixture", "format", "strict"},
		"ingest": {"config", "data-dir", "fixture", "format", "merge", "replace"},
		"init":   {"force", "git", "k8s", "out"},
		"pack":   {"config", "data-dir", "fixture", "force", "format", "out", "since"},
		"prove":  {"format"},
	}
	for name, want := range cases {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		for _, f := range want {
			if cmd.Flags().Lookup(f) == nil {
				t.Fatalf("%s missing required flag %q", name, f)
			}
		}
	}
}

func commandNames(root *cobra.Command) []string {
	var out []string
	for _, c := range root.Commands() {
		if c.Hidden || c.Name() == "help" {
			continue
		}
		name := strings.Fields(c.Use)[0]
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
