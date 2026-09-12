// Package deps holds cross-cutting build-invariant tests. It has no runtime code.
package deps

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultBuildHasNoK8sIO asserts the default build does not link any
// k8s.io/* package. The v1 Kubernetes connector is a pure-Go snapshot parser;
// client-go is intentionally not a dependency (see PLAN.md).
func skipGraphRebuildIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("module-graph / rebuild gates run on the full ubuntu suite")
	}
}

func TestDefaultBuildHasNoK8sIO(t *testing.T) {
	skipGraphRebuildIfShort(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := moduleRoot(t)

	cmd := exec.Command("go", "list", "-deps", "./cmd/opsgraph")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps failed: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "k8s.io/") {
			t.Fatalf("default build unexpectedly links %q; client-go must stay out of the default build", line)
		}
	}
}

// TestGoModHygiene forbids replace/exclude and direct k8s.io requires.
// Enterprise offline CLIs must stay on a clean, portable module graph.
func TestGoModHygiene(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "replace ") || trim == "replace (" {
			t.Fatalf("go.mod:%d: replace directives are forbidden", i+1)
		}
		if strings.HasPrefix(trim, "exclude ") || trim == "exclude (" {
			t.Fatalf("go.mod:%d: exclude directives are forbidden", i+1)
		}
		if strings.Contains(trim, "k8s.io/") {
			t.Fatalf("go.mod:%d: k8s.io modules are forbidden in the default module graph", i+1)
		}
	}
}

// TestCmdOpsgraphIsPureGo asserts no package in the default link graph has
// CgoFiles, so CGO_ENABLED=0 cross-builds stay valid on every OS.
func TestCmdOpsgraphIsPureGo(t *testing.T) {
	skipGraphRebuildIfShort(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := moduleRoot(t)
	// Force CGO off even when the race job sets CGO_ENABLED=1.
	cmd := exec.Command("go", "list", "-deps", "-f", "{{if .CgoFiles}}{{.ImportPath}}{{end}}", "./cmd/opsgraph")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	if hit := strings.TrimSpace(string(out)); hit != "" {
		t.Fatalf("default build links cgo packages (want pure-Go):\n%s", hit)
	}
}

// TestNoFirstPartyUnsafe forbids importing "unsafe" in first-party packages.
// Offline CLIs should stay reviewable without pointer games.
func TestNoFirstPartyUnsafe(t *testing.T) {
	skipGraphRebuildIfShort(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := moduleRoot(t)
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}} {{join .TestImports \" \"}}", "./...")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	mod := "github.com/sanjeev0120test/opsgraph"
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, mod) {
			continue
		}
		fields := strings.Fields(line)
		pkg := fields[0]
		for _, imp := range fields[1:] {
			if imp == "unsafe" {
				t.Fatalf("%s imports unsafe (forbidden in first-party code)", pkg)
			}
		}
	}
}

// TestNoIoutilUsage bans the deprecated ioutil package from first-party sources.
func TestNoIoutilUsage(t *testing.T) {
	root := moduleRoot(t)
	// Build the needle without embedding the banned import path literally.
	needle := []byte("io/" + "ioutil")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "bin" || name == "dist" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, needle) {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: deprecated ioutil import is forbidden (use io and os)", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestGitAttributesPinsLF keeps golden determinism portable across Windows checkouts.
func TestGitAttributesPinsLF(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, needle := range []string{
		"* text=auto eol=lf",
		"*.go   text eol=lf",
		"*.yaml text eol=lf",
		"*.json text eol=lf",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf(".gitattributes missing required LF pin %q", needle)
		}
	}
}

// TestDirectDepsAllowlisted freezes the direct require block in go.mod.
// Parsed offline so GOPROXY=off / -mod=readonly race CI stays reliable.
func TestDirectDepsAllowlisted(t *testing.T) {
	root := moduleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"github.com/go-git/go-git/v5":        true,
		"github.com/philippgille/chromem-go": true,
		"github.com/spf13/cobra":             true,
		"github.com/tmc/langchaingo":         true,
		"gopkg.in/yaml.v3":                   true,
		"modernc.org/sqlite":                 true,
	}
	got := map[string]bool{}
	inRequire := false
	for _, line := range strings.Split(string(raw), "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case trim == "require (":
			inRequire = true
		case inRequire && trim == ")":
			inRequire = false
		case inRequire:
			if trim == "" || strings.HasPrefix(trim, "//") || strings.Contains(trim, "// indirect") {
				continue
			}
			fields := strings.Fields(trim)
			if len(fields) < 1 {
				continue
			}
			pkg := fields[0]
			got[pkg] = true
			if !want[pkg] {
				t.Fatalf("unexpected direct dependency %q (update allowlist only after architecture review)", pkg)
			}
		case strings.HasPrefix(trim, "require ") && !strings.HasPrefix(trim, "require ("):
			fields := strings.Fields(trim)
			if len(fields) >= 2 {
				pkg := fields[1]
				got[pkg] = true
				if !want[pkg] {
					t.Fatalf("unexpected direct dependency %q (update allowlist only after architecture review)", pkg)
				}
			}
		}
	}
	for pkg := range want {
		if !got[pkg] {
			t.Fatalf("missing expected direct dependency %q", pkg)
		}
	}
}

// TestForbiddenModulePrefixes keeps cloud/k8s/MCP stacks out of the link graph.
func TestForbiddenModulePrefixes(t *testing.T) {
	skipGraphRebuildIfShort(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := moduleRoot(t)
	cmd := exec.Command("go", "list", "-deps", "./cmd/opsgraph")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps: %v", err)
	}
	deny := []string{
		"k8s.io/",
		"sigs.k8s.io/",
		"cloud.google.com/",
		"github.com/aws/aws-sdk-go",
		"github.com/Azure/",
		"github.com/openai/",
		"github.com/anthropics/",
		"github.com/modelcontextprotocol/",
	}
	for _, line := range strings.Split(string(out), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" {
			continue
		}
		for _, prefix := range deny {
			if strings.HasPrefix(pkg, prefix) {
				t.Fatalf("default build links forbidden package %q (prefix %q)", pkg, prefix)
			}
		}
	}
}

// TestReleaseBuildHasEmptyBuildID proves -ldflags=-buildid= yields a reproducible stamp.
func TestReleaseBuildHasEmptyBuildID(t *testing.T) {
	skipGraphRebuildIfShort(t)
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := moduleRoot(t)
	outBin := filepath.Join(t.TempDir(), "opsgraph-release")
	build := exec.Command("go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags=-s -w -buildid=", "-o", outBin, "./cmd/opsgraph")
	build.Dir = root
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("release-style build: %v\n%s", err, out)
	}
	idCmd := exec.Command("go", "tool", "buildid", outBin)
	idCmd.Dir = root
	idOut, err := idCmd.Output()
	if err != nil {
		t.Fatalf("go tool buildid: %v", err)
	}
	if id := strings.TrimSpace(string(idOut)); id != "" {
		t.Fatalf("want empty buildid for release ldflags, got %q", id)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root (go.mod)")
		}
		dir = parent
	}
}
