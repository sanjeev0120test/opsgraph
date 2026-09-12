package ingest

// Connector plugins are the extension point for signals opsgraph has no
// built-in connector for: an internal deploy tool, a ticket queue, a CI system.
//
// A plugin is any executable that prints opsgraph pack YAML to stdout. Reusing
// the pack format rather than inventing a plugin API means plugin output is
// ingested by exactly the same code as a fixture, so it packs, replays, proves
// and diffs identically. There is no RPC, no network and no dynamic linking,
// so the single static binary and the offline guarantee are unchanged.
//
// Plugins are never discovered from PATH. Only commands written in
// .opsgraph.yaml run, so dropping a file next to the binary cannot make
// opsgraph execute it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/store"
	"gopkg.in/yaml.v3"
)

// maxPluginOutput caps a plugin's stdout. A runaway plugin should fail the run,
// not exhaust memory during an incident.
const maxPluginOutput = 16 << 20 // 16 MiB

// maxPluginStderr bounds how much of a failing plugin's stderr we quote back.
const maxPluginStderr = 4096

// pluginSectionFiles maps the top-level keys a plugin may emit onto the pack
// file each one corresponds to.
//
// Kubernetes workload sections are deliberately excluded: k8s health carries
// scrape and prune semantics that only a real snapshot should trigger. A plugin
// reports health by setting services[].health directly.
var pluginSectionFiles = map[string]string{
	"services":     "services.yaml",
	"owners":       "owners.yaml",
	"changes":      "changes.yaml",
	"dependencies": "dependencies.yaml",
	"alerts":       "alerts.yaml",
}

// PluginSections lists the accepted top-level keys, sorted, for docs and errors.
func PluginSections() []string {
	out := make([]string, 0, len(pluginSectionFiles))
	for key := range pluginSectionFiles {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// memFS is a tiny read-only fs.FS over in-memory files, so plugin output can be
// handed to the same ingesters that read a pack directory.
type memFS map[string][]byte

func (m memFS) Open(name string) (fs.File, error) {
	data, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{name: name, Reader: bytes.NewReader(data), size: int64(len(data))}, nil
}

type memFile struct {
	*bytes.Reader
	name string
	size int64
}

func (f *memFile) Stat() (fs.FileInfo, error) { return memInfo{name: f.name, size: f.size}, nil }
func (f *memFile) Close() error               { return nil }

type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() fs.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// RunPlugins executes each enabled plugin in configured order and merges its
// output into the store. Plugins run last so that site-specific data wins over
// inferred data. A failing plugin is a hard error: answering an incident with
// silently missing signals is worse than failing loudly.
func RunPlugins(ctx context.Context, s *store.Store, plugins []config.PluginConnector, workDir string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for _, p := range plugins {
		if !p.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		out, err := runPlugin(ctx, p, workDir)
		if err != nil {
			return fmt.Errorf("plugin %q: %w", p.Name, err)
		}
		if err := ingestPluginOutput(s, p.Name, out); err != nil {
			return fmt.Errorf("plugin %q: %w", p.Name, err)
		}
	}
	return nil
}

// runPlugin executes one plugin and returns its stdout.
func runPlugin(ctx context.Context, p config.PluginConnector, workDir string) ([]byte, error) {
	if len(p.Command) == 0 {
		return nil, errors.New("command is empty")
	}
	pctx, cancel := context.WithTimeout(ctx, p.PluginTimeout())
	defer cancel()

	cmd := exec.CommandContext(pctx, p.Command[0], p.Command[1:]...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if pctx.Err() != nil && errors.Is(pctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("timed out after %s", p.PluginTimeout())
	}
	if err != nil {
		if msg := trimStderr(stderr.Bytes()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	if stdout.Len() > maxPluginOutput {
		return nil, fmt.Errorf("output exceeds %d bytes", maxPluginOutput)
	}
	return stdout.Bytes(), nil
}

func trimStderr(b []byte) string {
	if len(b) > maxPluginStderr {
		b = b[:maxPluginStderr]
	}
	return strings.TrimSpace(string(b))
}

// ingestPluginOutput parses one plugin document and upserts its entities.
func ingestPluginOutput(s *store.Store, name string, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		// A plugin with nothing to report is normal (no open tickets, no deploys).
		return nil
	}
	files, err := pluginFiles(data)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no recognized sections (want any of: %s)", strings.Join(PluginSections(), ", "))
	}
	source := "plugin:" + name
	if err := ingestEntityFiles(s, memFS(files), source, source); err != nil {
		return err
	}
	return s.SetMeta("connector:"+source, "ok")
}

// pluginFiles splits one plugin document into the pack files it maps onto.
func pluginFiles(data []byte) (map[string][]byte, error) {
	var doc map[string]yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse output: %w", err)
	}
	files := map[string][]byte{}
	for key, node := range doc {
		file, ok := pluginSectionFiles[key]
		if !ok {
			return nil, fmt.Errorf("unknown section %q (want any of: %s)", key, strings.Join(PluginSections(), ", "))
		}
		body, err := yaml.Marshal(map[string]yaml.Node{key: node})
		if err != nil {
			return nil, fmt.Errorf("re-encode %s: %w", key, err)
		}
		files[file] = body
	}
	return files, nil
}

// resolvePluginWorkDir keeps relative plugin commands predictable by running
// them from the directory holding .opsgraph.yaml.
func resolvePluginWorkDir(configDir string) string {
	if configDir == "" {
		return ""
	}
	abs, err := filepath.Abs(configDir)
	if err != nil {
		return configDir
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return ""
	}
	return abs
}
