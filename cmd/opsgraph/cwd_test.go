package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectCwdK8sIgnoresEmptyDir(t *testing.T) {
	wd := t.TempDir()
	if err := os.Mkdir(filepath.Join(wd, "k8s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := detectCwdK8s(wd); ok {
		t.Fatal("empty k8s/ must not look like a dump")
	}
	if err := os.WriteFile(filepath.Join(wd, "k8s", "events.yaml"), []byte("kind: List\nitems: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := detectCwdK8s(wd)
	if !ok || got != filepath.Join(wd, "k8s") {
		t.Fatalf("yaml in k8s/ must detect: got %q ok=%v", got, ok)
	}
}

func TestDetectCwdK8sSnapshotFile(t *testing.T) {
	wd := t.TempDir()
	if err := os.WriteFile(filepath.Join(wd, "k8s-snapshot.yaml"), []byte("kind: List\nitems: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok := detectCwdK8s(wd)
	if !ok || got != filepath.Join(wd, "k8s-snapshot.yaml") {
		t.Fatalf("got %q ok=%v", got, ok)
	}
}
