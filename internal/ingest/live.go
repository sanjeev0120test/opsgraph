package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/sanjeev0120test/opsgraph/internal/store"
)

// openK8sSnapshotFS accepts either a directory (deployments.yaml inside) or a
// path to a YAML file (kubectl List/Deployment dump or opsgraph dialect).
// A native List file also contributes Events; sibling events.yaml is merged
// when present. File names returned for fs.FS always use forward slashes.
func openK8sSnapshotFS(snap string) (fsys fs.FS, depFile, evFile, relFile string, err error) {
	info, err := os.Stat(snap)
	if err != nil {
		return nil, "", "", "", err
	}
	if info.IsDir() {
		return os.DirFS(snap), "deployments.yaml", "events.yaml", "releases.yaml", nil
	}
	dir := filepath.Dir(snap)
	return os.DirFS(dir), filepath.ToSlash(filepath.Base(snap)), "events.yaml", "releases.yaml", nil
}

// LiveIngest seeds the store from config and runs the enabled live connectors
// (local git, kubernetes snapshot, optional prometheus/alertmanager). configDir
// is used to resolve relative runbook/snapshot paths. Missing git repo is skipped.
func LiveIngest(ctx context.Context, s *store.Store, cfg *config.Config, configDir string, since, now time.Time) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := seedFromConfig(s, cfg, configDir); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Connectors.Kubernetes.Enabled {
		if cfg.Connectors.Kubernetes.Snapshot == "" {
			fmt.Fprintln(os.Stderr, "warning: kubernetes connector enabled but snapshot path is empty; skipping")
		} else {
			snap := cfg.Connectors.Kubernetes.Snapshot
			if !filepath.IsAbs(snap) {
				snap = filepath.Join(configDir, snap)
			}
			fsys, depFile, evFile, relFile, err := openK8sSnapshotFS(snap)
			if err != nil {
				return fmt.Errorf("kubernetes snapshot: %w", err)
			}
			allow := k8sAllowFromConfig(cfg)
			if err := ingestK8sFiles(s, fsys, depFile, evFile, now, allow); err != nil {
				return fmt.Errorf("kubernetes snapshot: %w", err)
			}
			if err := ingestHelmReleases(s, fsys, relFile, now); err != nil {
				return fmt.Errorf("helm snapshot: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Connectors.Prometheus.Enabled {
		if cfg.Connectors.Prometheus.URL == "" {
			fmt.Fprintln(os.Stderr, "warning: prometheus connector enabled but url is empty; skipping")
		} else {
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := IngestPrometheus(pctx, s, cfg.Connectors.Prometheus.URL, nil, now)
			cancel()
			if err != nil {
				// Hard-fail: soft-success hid missing alerts and let live-prefer
				// displace a populated state.db / wipe alerts on ingest --replace.
				return fmt.Errorf("prometheus connector: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Connectors.Alertmanager.Enabled {
		if cfg.Connectors.Alertmanager.URL == "" {
			fmt.Fprintln(os.Stderr, "warning: alertmanager connector enabled but url is empty; skipping")
		} else {
			actx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := IngestAlertmanager(actx, s, cfg.Connectors.Alertmanager.URL, nil, now)
			cancel()
			if err != nil {
				return fmt.Errorf("alertmanager connector: %w", err)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if cfg.Connectors.Git.Enabled {
		if err := ingestGitAndRunbooks(s, cfg, configDir, since, now); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Plugins run last so site-specific data wins over inferred topology.
	return RunPlugins(ctx, s, cfg.Connectors.Plugins, resolvePluginWorkDir(configDir))
}

func ingestGitAndRunbooks(s *store.Store, cfg *config.Config, configDir string, since, now time.Time) error {
	repoPath := cfg.Connectors.Git.RepoPath
	if repoPath == "" {
		repoPath = "."
	}
	if !filepath.IsAbs(repoPath) {
		repoPath = filepath.Join(configDir, repoPath)
	}
	targets, err := gitTargets(s, cfg)
	if err != nil {
		return err
	}
	if err := IngestGit(s, repoPath, targets, since, now); err != nil {
		if !isMissingGit(err) {
			return fmt.Errorf("git connector: %w", err)
		}
		return nil
	}
	svcs, err := s.ListServices()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(svcs))
	for _, svc := range svcs {
		ids = append(ids, svc.ID)
	}
	sort.Strings(ids)
	if err := DiscoverRunbooks(s, repoPath, ids); err != nil {
		return fmt.Errorf("discover runbooks: %w", err)
	}
	return nil
}

func gitTargets(s *store.Store, cfg *config.Config) ([]ServicePaths, error) {
	byID := map[string]ServicePaths{}
	for _, sp := range servicePaths(cfg) {
		byID[sp.ServiceID] = sp
	}
	svcs, err := s.ListServices()
	if err != nil {
		return nil, err
	}
	for _, svc := range svcs {
		sp := byID[svc.ID]
		sp.ServiceID = svc.ID
		sp.Paths = append(sp.Paths, DefaultGitPaths(svc.ID)...)
		for _, a := range svc.Aliases {
			sp.Paths = append(sp.Paths, DefaultGitPaths(a)...)
		}
		byID[svc.ID] = sp
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ServicePaths, 0, len(ids))
	for _, id := range ids {
		sp := byID[id]
		sp.Paths = uniqueGitPaths(sp.Paths)
		out = append(out, sp)
	}
	return out, nil
}

func isMissingGit(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, git.ErrRepositoryNotExists) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "repository does not exist") ||
		strings.Contains(msg, "reference not found")
}

func seedFromConfig(s *store.Store, cfg *config.Config, configDir string) error {
	ownerIDs := make([]string, 0, len(cfg.Owners))
	for id := range cfg.Owners {
		ownerIDs = append(ownerIDs, id)
	}
	sort.Strings(ownerIDs)
	for _, id := range ownerIDs {
		o := cfg.Owners[id]
		if err := s.UpsertOwner(model.Owner{ID: id, Name: o.Name, Team: o.Team, Email: o.Email}); err != nil {
			return err
		}
	}
	svcIDs := make([]string, 0, len(cfg.Services))
	for id := range cfg.Services {
		svcIDs = append(svcIDs, id)
	}
	sort.Strings(svcIDs)
	for _, id := range svcIDs {
		sc := cfg.Services[id]
		health := model.HealthUnknown
		name := id
		sources := []string{"config"}
		if existing, err := s.GetService(id); err == nil {
			// Preserve connector-derived health/name; config seed must not wipe them.
			health = existing.Health
			if existing.Name != "" {
				name = existing.Name
			}
			sources = addSource(existing.Sources, "config")
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := s.UpsertService(model.Service{
			ID: id, Name: name, Aliases: sc.Aliases, OwnerID: sc.Owner,
			Health: health, Sources: sources,
		}); err != nil {
			return err
		}
		for _, dep := range sc.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" || dep == id {
				continue
			}
			if err := ensureServiceStub(s, dep); err != nil {
				return err
			}
			if err := s.UpsertDependency(model.Dependency{
				FromServiceID: id, ToServiceID: dep, Type: "depends_on", Source: "config",
			}); err != nil {
				return err
			}
		}
		if sc.Runbook != "" {
			if err := seedRunbook(s, sc.Runbook, configDir); err != nil {
				return err
			}
		}
	}
	// Marks this store as carrying a complete config topology for MergeFrom prune.
	return s.SetMeta("topology:seeded", "ok")
}

func seedRunbook(s *store.Store, rbPath, configDir string) error {
	p := rbPath
	if !filepath.IsAbs(p) {
		p = filepath.Join(configDir, p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing optional path must not abort Prom/k8s ingest.
			fmt.Fprintf(os.Stderr, "warning: configured runbook %q not found; skipping\n", p)
			return nil
		}
		return fmt.Errorf("read runbook %q: %w", p, err)
	}
	rb, _, err := runbook.ParseWithInfer(data, filepath.ToSlash(rbPath))
	if err != nil {
		return err
	}
	if rb.ServiceID == "" {
		fmt.Fprintf(os.Stderr, "warning: runbook %q has empty service in front matter; skipping\n", p)
		return nil
	}
	return s.UpsertRunbook(rb)
}

func k8sAllowFromConfig(cfg *config.Config) k8sAllow {
	a := k8sAllow{ByService: map[string]svcK8sAllow{}}
	if cfg == nil {
		return a
	}
	for id, sc := range cfg.Services {
		sa := svcK8sAllow{Deployments: map[string]bool{}, Namespaces: map[string]bool{}}
		for _, d := range sc.K8s.Deployments {
			d = strings.TrimSpace(d)
			if d != "" {
				sa.Deployments[d] = true
				sa.hasDeps = true
			}
		}
		for _, ns := range sc.K8s.Namespaces {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				sa.Namespaces[ns] = true
				sa.hasNS = true
			}
		}
		if sa.hasDeps || sa.hasNS {
			a.ByService[id] = sa
		}
	}
	return a
}

func servicePaths(cfg *config.Config) []ServicePaths {
	ids := make([]string, 0, len(cfg.Services))
	for id := range cfg.Services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []ServicePaths
	for _, id := range ids {
		sc := cfg.Services[id]
		if len(sc.GitPaths) == 0 {
			continue
		}
		out = append(out, ServicePaths{ServiceID: id, Paths: sc.GitPaths})
	}
	return out
}
