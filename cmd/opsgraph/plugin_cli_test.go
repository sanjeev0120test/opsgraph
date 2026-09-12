package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func pluginBillingYAML() string {
	at := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Second).Format(time.RFC3339)
	return fmt.Sprintf(`services:
  - id: billing
    name: billing
    health: degraded
changes:
  - id: deploy-9001
    service_id: billing
    at: %s
    type: deploy
    summary: "release 9001 to billing"
`, at)
}

func catCommand(path string) []string {
	if runtime.GOOS == "windows" {
		return []string{"cmd", "/c", "type", path}
	}
	return []string{"cat", path}
}

func TestAskFromPluginConfig(t *testing.T) {
	dir := t.TempDir()
	outYAML := filepath.Join(dir, "plugin.yaml")
	if err := os.WriteFile(outYAML, []byte(pluginBillingYAML()), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := catCommand(outYAML)
	quoted := make([]string, 0, len(cmd))
	for _, part := range cmd {
		quoted = append(quoted, strconvQuoteForTest(part))
	}
	cfg := "version: 1\nconnectors:\n  git:\n    enabled: false\n  plugins:\n    - name: deploys\n      enabled: true\n      command: [" + strings.Join(quoted, ", ") + "]\n"
	cfgPath := filepath.Join(dir, ".opsgraph.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OPSGRAPH_FIXTURE", "")
	t.Setenv("OPSGRAPH_DATA_DIR", "")
	stdout, stderr, code := runRoot(t, "ask", "billing", "--config", cfgPath, "--format", "json")
	if code != 0 {
		t.Fatalf("ask via plugin exit=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, `"id": "billing"`) || !strings.Contains(stdout, `"health": "degraded"`) || !strings.Contains(stdout, `"plugin:deploys"`) {
		t.Fatalf("plugin service missing:\n%s", stdout)
	}
	if !strings.Contains(stdout, "release 9001") {
		t.Fatalf("plugin change missing:\n%s", stdout)
	}
}
