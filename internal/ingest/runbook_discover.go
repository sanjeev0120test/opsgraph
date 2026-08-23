package ingest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// DiscoverRunbooks loads conventional Markdown runbooks from a git checkout
// without a service catalog. Existing store runbooks are left alone.
func DiscoverRunbooks(s *store.Store, repoPath string, serviceIDs []string) error {
	if s == nil || strings.TrimSpace(repoPath) == "" {
		return nil
	}
	dirs := []string{
		filepath.Join(repoPath, "runbooks"),
		filepath.Join(repoPath, "docs", "runbooks"),
		filepath.Join(repoPath, "configs", "runbooks"),
	}
	for _, dir := range dirs {
		if err := discoverRunbookDir(s, dir); err != nil {
			return err
		}
	}
	for _, id := range serviceIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		rels := []string{
			filepath.Join("services", id, "runbook.md"),
			filepath.Join("apps", id, "runbook.md"),
			filepath.Join("cmd", id, "runbook.md"),
			filepath.Join("docs", id, "runbook.md"),
		}
		for _, rel := range rels {
			if err := discoverRunbookFile(s, filepath.Join(repoPath, rel), rel, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func discoverRunbookDir(s *store.Store, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		abs := filepath.Join(dir, e.Name())
		guess := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if err := discoverRunbookFile(s, abs, filepath.ToSlash(abs), guess); err != nil {
			return err
		}
	}
	return nil
}

func discoverRunbookFile(s *store.Store, abs, storePath, fallbackService string) error {
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	rb, _, err := runbook.ParseWithInfer(data, filepath.ToSlash(storePath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: skip runbook %q: %v\n", abs, err)
		return nil
	}
	if rb.ServiceID == "" {
		rb.ServiceID = strings.TrimSpace(fallbackService)
		if rb.ServiceID != "" {
			rb.ID = "runbook-" + rb.ServiceID
		}
	}
	if rb.ServiceID == "" {
		return nil
	}
	if _, err := s.GetRunbook(rb.ServiceID); err == nil {
		return nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return s.UpsertRunbook(rb)
}
