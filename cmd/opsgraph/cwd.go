package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/config"
)

// detectCwdSource finds a usable incident source in the working directory so
// `opsgraph ask` works with no flags: a kubectl dump sitting on disk, or a
// services/ / apps/ git monorepo. Tests run from cmd/opsgraph (no those dirs)
// and still get "no data source".
func detectCwdSource() (cfg *config.Config, configDir string, ok bool) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", false
	}
	cfg = config.Default()
	cfg.Connectors.Git.Enabled = false
	cfg.Connectors.Kubernetes.Enabled = false

	k8sPath, k8sOK := detectCwdK8s(wd)
	gitOK := detectCwdGitLayout(wd)
	if !k8sOK && !gitOK {
		return nil, "", false
	}
	if gitOK {
		cfg.Connectors.Git.Enabled = true
		cfg.Connectors.Git.RepoPath = wd
	} else if k8sOK && (cwdDir(filepath.Join(wd, ".git")) || cwdFile(filepath.Join(wd, ".git"))) {
		cfg.Connectors.Git.Enabled = true
		cfg.Connectors.Git.RepoPath = wd
	}
	if k8sOK {
		cfg.Connectors.Kubernetes.Enabled = true
		cfg.Connectors.Kubernetes.Snapshot = k8sPath
	}
	return cfg, wd, true
}

func detectCwdK8s(wd string) (string, bool) {
	for _, rel := range []string{
		"k8s-snapshot.yaml",
		"k8s-snapshot.yml",
		"k8s.yaml",
		filepath.Join("k8s", "deployments.yaml"),
	} {
		p := filepath.Join(wd, rel)
		if cwdFile(p) {
			if filepath.Base(rel) == "deployments.yaml" {
				return filepath.Join(wd, "k8s"), true
			}
			return p, true
		}
	}
	k8sDir := filepath.Join(wd, "k8s")
	if cwdDir(k8sDir) && k8sDirHasYAML(k8sDir) {
		return k8sDir, true
	}
	return "", false
}

func k8sDirHasYAML(dir string) bool {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if strings.HasSuffix(n, ".yaml") || strings.HasSuffix(n, ".yml") {
			return true
		}
	}
	return false
}

func detectCwdGitLayout(wd string) bool {
	if !cwdDir(filepath.Join(wd, ".git")) && !cwdFile(filepath.Join(wd, ".git")) {
		// Allow go-git to open a repo only when a .git exists here (not a parent
		// walk from cmd/opsgraph tests).
		return false
	}
	return cwdDir(filepath.Join(wd, "services")) || cwdDir(filepath.Join(wd, "apps"))
}

func cwdFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func cwdDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
