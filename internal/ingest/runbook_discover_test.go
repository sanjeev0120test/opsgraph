package ingest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestDiscoverRunbooks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "runbooks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nservice: checkout\n---\n\n1. Confirm a recent deploy.\n")
	if err := os.WriteFile(filepath.Join(dir, "checkout.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := ingest.DiscoverRunbooks(s, root, []string{"checkout"}); err != nil {
		t.Fatal(err)
	}
	rb, err := s.GetRunbook("checkout")
	if err != nil {
		t.Fatal(err)
	}
	if len(rb.Steps) != 1 || rb.Steps[0].Check != "deploy_age_lt:60m" {
		t.Fatalf("inferred check missing: %+v", rb.Steps)
	}
}
