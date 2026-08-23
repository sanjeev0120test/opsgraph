package main

import (
	"reflect"
	"testing"
)

func TestDiffSorted(t *testing.T) {
	added, removed, shared := diffSorted([]string{"a", "c"}, []string{"b", "c", "d"})
	if !reflect.DeepEqual(added, []string{"b", "d"}) {
		t.Fatalf("added=%v", added)
	}
	if !reflect.DeepEqual(removed, []string{"a"}) {
		t.Fatalf("removed=%v", removed)
	}
	if !reflect.DeepEqual(shared, []string{"c"}) {
		t.Fatalf("shared=%v", shared)
	}
	added, removed, shared = diffSorted([]string{"a"}, []string{"a"})
	if len(added) != 0 || len(removed) != 0 || !reflect.DeepEqual(shared, []string{"a"}) {
		t.Fatalf("same: added=%v removed=%v shared=%v", added, removed, shared)
	}
}

func TestRewriteRootArgsOpensPack(t *testing.T) {
	root := newRootCmd()
	fx := fixtureDir(t)
	got := rewriteRootArgs(root, []string{fx, "--format", "json"})
	want := []string{"ask", "--fixture", fx, "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	got = rewriteRootArgs(root, []string{"ask", "checkout"})
	if !reflect.DeepEqual(got, []string{"ask", "checkout"}) {
		t.Fatalf("must not rewrite known commands: %v", got)
	}
	got = rewriteRootArgs(root, []string{"nosuch-command"})
	if !reflect.DeepEqual(got, []string{"nosuch-command"}) {
		t.Fatalf("unknown non-pack must stay: %v", got)
	}
}
