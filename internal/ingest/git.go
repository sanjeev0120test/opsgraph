package ingest

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// ServicePaths maps a service id to the repo path prefixes that belong to it.
type ServicePaths struct {
	ServiceID string
	Paths     []string
}

// IngestGit scans a local git repository for commits at/after since and records
// a change per (commit, matching service). It is read-only and offline.
func IngestGit(s *store.Store, repoPath string, services []ServicePaths, since, now time.Time) error {
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("open git repo %q: %w", repoPath, err)
	}
	head, err := repo.Head()
	if err != nil {
		return fmt.Errorf("git head: %w", err)
	}
	sinceUTC := since.UTC()
	iter, err := repo.Log(&git.LogOptions{From: head.Hash(), Since: &sinceUTC})
	if err != nil {
		return fmt.Errorf("git log: %w", err)
	}
	defer iter.Close()

	nowUTC := now.UTC()
	return iter.ForEach(func(c *object.Commit) error {
		at := c.Author.When.UTC()
		if at.Before(sinceUTC) || at.After(nowUTC) {
			return nil
		}
		files, err := changedFiles(c)
		if err != nil {
			return err
		}
		targets := expandGitTargets(services, files)
		for _, sp := range targets {
			paths := sp.Paths
			if len(paths) == 0 {
				paths = DefaultGitPaths(sp.ServiceID)
			}
			if !matchesGit(files, sp.ServiceID, paths) {
				continue
			}
			if err := ensureGitService(s, sp.ServiceID); err != nil {
				return err
			}
			if err := recordCommit(s, c, sp.ServiceID); err != nil {
				return err
			}
		}
		return nil
	})
}

func recordCommit(s *store.Store, c *object.Commit, serviceID string) error {
	short := shortHash(c.Hash.String())
	evID := "ev-git-" + short + "-" + serviceID
	summary := firstLine(c.Message)
	at := c.Author.When.UTC()
	if err := s.UpsertChange(model.Change{
		ID: "git-" + short + "-" + serviceID, ServiceID: serviceID, At: at, Type: "commit",
		Summary: summary, Author: c.Author.Name, Revision: short, Source: "git", EvidenceID: evID,
	}); err != nil {
		return err
	}
	return s.UpsertEvidence(model.Evidence{
		ID: evID, ServiceID: serviceID, Source: "git", At: at, Kind: "commit", Summary: summary, RawRef: c.Hash.String(),
	})
}

// changedFiles returns the set of file paths touched by a commit (vs its first
// parent, or the full tree for a root commit). Uses tree diff so renames/copies
// still attribute to git_paths (unlike Stats()-only).
func changedFiles(c *object.Commit) ([]string, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("commit tree %s: %w", c.Hash, err)
	}
	seen := map[string]bool{}
	out := make([]string, 0)
	add := func(name string) {
		p := path.Clean(strings.ReplaceAll(name, "\\", "/"))
		if p == "." || p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	collect := func(changes object.Changes) {
		for _, ch := range changes {
			add(ch.From.Name)
			add(ch.To.Name)
		}
	}
	if c.NumParents() == 0 {
		changes, err := object.DiffTree(nil, tree)
		if err != nil {
			return nil, fmt.Errorf("commit diff %s: %w", c.Hash, err)
		}
		collect(changes)
	} else {
		// Union diffs against every parent so merge commits attribute both sides.
		for i := 0; i < c.NumParents(); i++ {
			parent, perr := c.Parent(i)
			if perr != nil {
				return nil, fmt.Errorf("commit parent %d %s: %w", i, c.Hash, perr)
			}
			ptree, perr := parent.Tree()
			if perr != nil {
				return nil, fmt.Errorf("parent tree %s: %w", parent.Hash, perr)
			}
			changes, derr := object.DiffTree(ptree, tree)
			if derr != nil {
				return nil, fmt.Errorf("commit diff %s parent %d: %w", c.Hash, i, derr)
			}
			collect(changes)
		}
	}
	sort.Strings(out)
	return out, nil
}

func ensureGitService(s *store.Store, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	svc, err := s.GetService(id)
	if err == nil {
		svc.Sources = addSource(svc.Sources, "git")
		return s.UpsertService(*svc)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return s.UpsertService(model.Service{
		ID: id, Name: id, Health: model.HealthUnknown, Sources: []string{"git"},
	})
}

func matchesAny(files, prefixes []string) bool {
	for _, f := range files {
		for _, p := range prefixes {
			p = strings.TrimSuffix(strings.ReplaceAll(p, "\\", "/"), "/")
			if p == "" {
				continue
			}
			if f == p || strings.HasPrefix(f, p+"/") {
				return true
			}
		}
	}
	return false
}

func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		return strings.TrimSpace(msg[:i])
	}
	return strings.TrimSpace(msg)
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
