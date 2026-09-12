package main

import (
	"os"
	"path/filepath"
	"testing"
)

// PowerShell 5.1 reads a downloaded UTF-8 script as the system ANSI page
// unless a BOM is present. A single em-dash in install.ps1 made v0.2.0's
// published installer unparseable on this machine.
func TestInstallScriptsAreASCII(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("scripts", "install.ps1"),
		filepath.Join("scripts", "install.sh"),
		filepath.Join("scripts", "verify.ps1"),
	} {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range data {
			if b > 127 {
				t.Fatalf("%s: non-ASCII byte 0x%02x at offset %d (Windows PS 5.1 will misparse UTF-8 without a BOM)", rel, b, i)
			}
		}
	}
}
