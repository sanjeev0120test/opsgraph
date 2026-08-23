package ingest_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

func TestIngestGit(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}

	commit := func(rel, content, msg string, when time.Time) {
		abs := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := wt.Add(rel); err != nil {
			t.Fatalf("add %s: %v", rel, err)
		}
		if _, err := wt.Commit(msg, &git.CommitOptions{
			Author: &object.Signature{Name: "Dev", Email: "dev@example.com", When: when},
		}); err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	now := time.Now().UTC()
	commit("services/checkout/app.go", "package main\n", "bump checkout to v1.4.2", now.Add(-10*time.Minute))
	commit("services/other/x.go", "package other\n", "unrelated change", now.Add(-5*time.Minute))

	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	err = ingest.IngestGit(s, dir, []ingest.ServicePaths{
		{ServiceID: "checkout", Paths: []string{"services/checkout"}},
	}, now.Add(-time.Hour), now)
	if err != nil {
		t.Fatalf("ingest git: %v", err)
	}

	changes, err := s.ListChanges("checkout", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 {
		t.Fatalf("want 1 checkout commit, got %d: %+v", len(changes), changes)
	}
	c := changes[0]
	if c.Type != "commit" || c.Source != "git" || c.Summary != "bump checkout to v1.4.2" {
		t.Fatalf("unexpected change: %+v", c)
	}
	if c.EvidenceID == "" {
		t.Fatal("commit change should reference evidence")
	}
}

func TestIngestGitDiscoversWithoutCatalog(t *testing.T) {
	dir := t.TempDir()
	repo, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	abs := filepath.Join(dir, "services", "payments", "main.go")
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("services/payments/main.go"); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Commit("fix payments timeout", &git.CommitOptions{
		Author: &object.Signature{Name: "Dev", Email: "dev@example.com", When: now.Add(-5 * time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}

	s, cleanup, err := store.OpenTemp()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if err := ingest.IngestGit(s, dir, nil, now.Add(-time.Hour), now); err != nil {
		t.Fatal(err)
	}
	svc, err := s.GetService("payments")
	if err != nil {
		t.Fatalf("discovered service: %v", err)
	}
	if svc.Health != "unknown" {
		t.Fatalf("health=%q", svc.Health)
	}
	changes, err := s.ListChanges("payments", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Summary != "fix payments timeout" {
		t.Fatalf("changes: %+v", changes)
	}
}

func TestDefaultGitPaths(t *testing.T) {
	got := ingest.DefaultGitPaths("checkout")
	joined := strings.Join(got, ",")
	for _, want := range []string{"services/checkout", "apps/checkout", "checkout"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, got)
		}
	}
	for _, p := range ingest.DefaultGitPaths("docs") {
		if p == "docs" {
			t.Fatalf("reserved name leaked bare path: %v", ingest.DefaultGitPaths("docs"))
		}
	}
}
