package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/spf13/cobra"
)

// rewriteRootArgs turns `opsgraph incident.opsgraph` into `ask --fixture …`
// so an emailed pack is a file you open, not a flag you remember.
func rewriteRootArgs(root *cobra.Command, args []string) []string {
	if len(args) == 0 {
		return args
	}
	first := args[0]
	if strings.HasPrefix(first, "-") {
		return args
	}
	if isKnownCommand(root, first) {
		return args
	}
	if !openablePack(first) {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "ask", "--fixture", first)
	return append(out, args[1:]...)
}

func openablePack(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	if st.IsDir() {
		if fileExists(filepath.Join(path, "meta.yaml")) {
			return true
		}
		return fileExists(filepath.Join(path, "services.yaml"))
	}
	return ingest.IsPackArchive(path)
}

func isKnownCommand(root *cobra.Command, name string) bool {
	if root == nil || name == "" {
		return false
	}
	for _, c := range root.Commands() {
		if c.Name() == name {
			return true
		}
		for _, a := range c.Aliases {
			if a == name {
				return true
			}
		}
	}
	return false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func defaultPackPath() string {
	if openablePack("incident.opsgraph") {
		return "incident.opsgraph"
	}
	return ""
}

func maybePackSHA256(path string) string {
	if !ingest.IsPackArchive(path) {
		return ""
	}
	sum, err := ingest.FileSHA256(path)
	if err != nil {
		return ""
	}
	return sum
}
