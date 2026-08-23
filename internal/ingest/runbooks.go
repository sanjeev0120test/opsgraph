package ingest

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// ingestRunbooks parses every runbooks/*.md file and upserts it, keyed by the
// service named in its front matter.
func ingestRunbooks(s *store.Store, fsys fs.FS) error {
	entries, err := fs.ReadDir(fsys, "runbooks")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read runbooks dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		name := "runbooks/" + e.Name()
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		rb, _, err := runbook.ParseWithInfer(data, name)
		if err != nil {
			return err
		}
		if rb.ServiceID == "" {
			continue
		}
		if err := s.UpsertRunbook(rb); err != nil {
			return err
		}
	}
	return nil
}
