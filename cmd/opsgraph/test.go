package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/spf13/cobra"
)

func newTestCmd() *cobra.Command {
	var update bool
	var format string
	cmd := &cobra.Command{
		Use:   "test <fixture>",
		Short: "Run a fixture pack and compare output against its golden files",
		Long: "Ingests a fixture pack, then for every service that has a runbook it\n" +
			"generates ask_<svc>.json and verify_<svc>.json and compares them against\n" +
			"the pack's expected/ directory. Use --update to (re)generate goldens.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireArg("fixture-dir", args[0]); err != nil {
				return err
			}
			if format != "table" && format != "json" {
				return fail(2, "invalid --format %q (want table or json)", format)
			}
			dir := args[0]
			ls, err := storeFromFixtureDir(dir)
			if err != nil {
				return fail(2, "%v", err)
			}
			defer ls.cleanup()
			root := ls.root
			if root == "" {
				root = dir
			}
			if ingest.IsPackArchive(dir) && update {
				return fail(2, "cannot --update goldens inside a pack archive; unpack it first")
			}

			services, err := ls.store.ListServices()
			if err != nil {
				return fail(2, "%v", err)
			}
			expectedDir := filepath.Join(root, "expected")
			if update {
				if err := os.MkdirAll(expectedDir, 0o755); err != nil {
					return fail(2, "%v", err)
				}
			}

			type fileResult struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Detail string `json:"detail,omitempty"`
			}
			var files []fileResult
			verifier := runbook.NewVerifier(ls.store, ls.now)
			var failures int
			checked := 0
			record := func(name, status, detail string) {
				files = append(files, fileResult{Name: name, Status: status, Detail: detail})
			}
			for _, svc := range services {
				_, rbErr := ls.store.GetRunbook(svc.ID)
				hasRB := rbErr == nil
				askName := "ask_" + svc.ID + ".json"
				askPath := filepath.Join(expectedDir, askName)
				_, askExists := os.Stat(askPath)
				if !hasRB && !update && askExists != nil {
					continue
				}
				if !hasRB && update && askExists != nil {
					continue
				}

				res, err := ask.Ask(ls.store, svc.ID, ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
				if err != nil {
					return fail(2, "ask %s: %v", svc.ID, err)
				}
				if err := goldenIO(cmd, expectedDir, askName, res, update, &failures, format == "json", record); err != nil {
					return err
				}
				checked++

				if !hasRB {
					continue
				}
				vr, err := verifier.VerifyService(svc.ID)
				if err != nil {
					return fail(2, "verify %s: %v", svc.ID, err)
				}
				if err := goldenIO(cmd, expectedDir, "verify_"+svc.ID+".json", vr, update, &failures, format == "json", record); err != nil {
					return err
				}
			}

			if checked == 0 {
				return fail(2, "no services with runbooks or ask goldens found in %q", dir)
			}
			if update {
				if format == "json" {
					return output.JSON(cmd.OutOrStdout(), map[string]any{
						"updated": true,
						"checked": checked,
						"files":   files,
					})
				}
				cmd.Printf("updated goldens for %d service(s)\n", checked)
				return nil
			}
			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), map[string]any{
					"ok":       failures == 0,
					"checked":  checked,
					"failures": failures,
					"files":    files,
				}); err != nil {
					return fail(2, "%v", err)
				}
			}
			if failures > 0 {
				return fail(1, "%d golden mismatch(es)", failures)
			}
			if format != "json" {
				cmd.Printf("ok - %d service(s) match goldens\n", checked)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&update, "update", false, "write/regenerate golden files instead of comparing")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func goldenIO(cmd *cobra.Command, dir, name string, v any, update bool, failures *int, quiet bool, record func(string, string, string)) error {
	var buf bytes.Buffer
	if err := output.JSON(&buf, v); err != nil {
		return err
	}
	got := buf.Bytes()
	path := filepath.Join(dir, name)

	if update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			return fail(2, "write golden %s: %v", name, err)
		}
		if record != nil {
			record(name, "wrote", "")
		}
		if !quiet {
			cmd.Printf("wrote %s\n", name)
		}
		return nil
	}

	want, err := os.ReadFile(path)
	if err != nil {
		detail := err.Error()
		if record != nil {
			record(name, "miss", detail)
		}
		if !quiet {
			cmd.PrintErrf("MISS %s: %v\n", name, err)
		}
		*failures++
		return nil
	}
	wantNorm := bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	gotNorm := bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(wantNorm, gotNorm) {
		detail := firstDiff(wantNorm, gotNorm)
		if record != nil {
			record(name, "diff", detail)
		}
		if !quiet {
			cmd.PrintErrf("DIFF %s: %s\n", name, detail)
		}
		*failures++
		return nil
	}
	if record != nil {
		record(name, "ok", "")
	}
	return nil
}

func firstDiff(want, got []byte) string {
	wl := bytes.Split(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), []byte("\n"))
	gl := bytes.Split(got, []byte("\n"))
	n := len(wl)
	if len(gl) < n {
		n = len(gl)
	}
	for i := 0; i < n; i++ {
		if !bytes.Equal(wl[i], gl[i]) {
			return fmt.Sprintf("line %d:\n  want: %s\n  got:  %s", i+1, wl[i], gl[i])
		}
	}
	return fmt.Sprintf("length differs (want %d lines, got %d lines)", len(wl), len(gl))
}
