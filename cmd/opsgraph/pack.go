package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/spf13/cobra"
)

func newPackCmd() *cobra.Command {
	var src sourceFlags
	var out string
	var force bool
	var since time.Duration
	var format string
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Write a portable incident pack anyone can replay with opsgraph test",
		Long: "pack snapshots the current source into a fixture directory or a single\n" +
			".zip / .opsgraph file. Email the file; the recipient needs no cluster:\n\n" +
			"  opsgraph pack\n" +
			"  opsgraph pack --out ./incident.opsgraph\n" +
			"  opsgraph test ./incident.opsgraph\n" +
			"  opsgraph ask --fixture ./incident.opsgraph\n\n" +
			"Default --out is incident.opsgraph. Replay is bit-identical across\n" +
			"Windows/macOS/Linux (pure Go archive/zip).",
		Example: `  opsgraph pack
  opsgraph pack --fixture fixtures/incident_checkout --out ./incident
  opsgraph pack --out ./incident.opsgraph
  opsgraph test ./incident.opsgraph`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			out = strings.TrimSpace(out)
			if out == "" {
				out = "incident.opsgraph"
			}
			if err := rejectDangerousPackDir(out); err != nil {
				return err
			}
			ls, _, err := src.loadCtx(cmd.Context(), since)
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()

			workDir := out
			archive := ingest.IsPackArchive(out)
			var extraCleanup func()
			if archive {
				tmp, err := os.MkdirTemp("", "opsgraph-pack-*")
				if err != nil {
					return fail(2, "%v", err)
				}
				extraCleanup = func() { _ = os.RemoveAll(tmp) }
				defer extraCleanup()
				workDir = tmp
				if _, err := os.Stat(out); err == nil {
					if !force {
						return fail(1, "%s already exists (use --force to overwrite)", out)
					}
					if err := os.Remove(out); err != nil {
						return fail(2, "remove %s: %v", out, err)
					}
				}
			} else if err := preparePackDir(out, force); err != nil {
				return err
			}
			if err := ingest.WritePack(ls.store, ls.now, workDir); err != nil {
				return fail(2, "write pack: %v", err)
			}
			// Golden the pack's own replay, not the source store. WritePack
			// normalizes the snapshot (namespaces collapse to default, replica
			// counts are synthesized from health), so goldens taken from the
			// source would describe something the pack cannot reproduce.
			replay, err := storeFromFixtureDir(workDir)
			if err != nil {
				return fail(2, "read back pack: %v", err)
			}
			n, err := writePackGoldens(replay, workDir)
			replay.cleanup()
			if err != nil {
				return fail(2, "write goldens: %v", err)
			}
			if err := provePackReplay(workDir); err != nil {
				return fail(1, "pack replay failed: %v", err)
			}
			if archive {
				if err := ingest.ZipPack(workDir, out); err != nil {
					return fail(2, "zip pack: %v", err)
				}
				if err := provePackReplay(out); err != nil {
					return fail(1, "archive replay failed: %v", err)
				}
			}
			abs, err := filepath.Abs(out)
			if err != nil {
				abs = out
			}
			if format == "json" {
				payload := map[string]any{
					"ok":      true,
					"dir":     abs,
					"path":    abs,
					"goldens": n,
					"now":     ls.now.UTC().Format(time.RFC3339),
				}
				if archive {
					payload["archive"] = true
					if sum, sumErr := ingest.FileSHA256(out); sumErr == nil {
						payload["sha256"] = sum
					}
				}
				return output.JSON(cmd.OutOrStdout(), payload)
			}
			cmd.Printf("wrote %s (%d golden file(s))\n", abs, n)
			cmd.Printf("next: opsgraph test %s\n", out)
			cmd.Printf("      opsgraph ask --fixture %s\n", out)
			return nil
		},
	}
	bindSourceFlags(cmd, &src)
	cmd.Flags().StringVar(&out, "out", "incident.opsgraph", "directory or .zip/.opsgraph archive to write")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing opsgraph pack directory")
	cmd.Flags().DurationVar(&since, "since", 0, "lookback window (live sources)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

func rejectDangerousPackDir(out string) error {
	clean := filepath.Clean(out)
	if clean == "." || clean == string(filepath.Separator) {
		return fail(2, "refusing to write pack to %q", out)
	}
	if vol := filepath.VolumeName(clean); vol != "" && clean == vol+"\\" {
		return fail(2, "refusing to write pack to %q", out)
	}
	return nil
}

func preparePackDir(out string, force bool) error {
	info, err := os.Stat(out)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(out, 0o755)
		}
		return fail(2, "stat %s: %v", out, err)
	}
	if !info.IsDir() {
		return fail(2, "%s exists and is not a directory", out)
	}
	if !force {
		return fail(1, "%s already exists (use --force to overwrite a pack)", out)
	}
	meta := filepath.Join(out, "meta.yaml")
	if _, err := os.Stat(meta); err != nil {
		entries, readErr := os.ReadDir(out)
		if readErr != nil {
			return fail(2, "read %s: %v", out, readErr)
		}
		if len(entries) > 0 {
			return fail(2, "%s is not an opsgraph pack (no meta.yaml); refusing to overwrite", out)
		}
	}
	if err := os.RemoveAll(out); err != nil {
		return fail(2, "remove %s: %v", out, err)
	}
	return os.MkdirAll(out, 0o755)
}

func writePackGoldens(ls *loadedStore, dir string) (int, error) {
	expected := filepath.Join(dir, "expected")
	if err := os.MkdirAll(expected, 0o755); err != nil {
		return 0, err
	}
	svcs, err := ls.store.ListServices()
	if err != nil {
		return 0, err
	}
	hottest, _, _ := pickHottestService(ls, time.Hour)
	want := map[string]bool{}
	if hottest != "" {
		want[hottest] = true
	}
	for _, svc := range svcs {
		if _, err := ls.store.GetRunbook(svc.ID); err == nil {
			want[svc.ID] = true
		}
		if svc.Health == "degraded" || svc.Health == "unhealthy" {
			want[svc.ID] = true
		}
	}
	n := 0
	verifier := runbook.NewVerifier(ls.store, ls.now)
	for _, svc := range svcs {
		if !want[svc.ID] {
			continue
		}
		res, err := ask.Ask(ls.store, svc.ID, ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
		if err != nil {
			return n, err
		}
		var buf bytes.Buffer
		if err := output.JSON(&buf, res); err != nil {
			return n, err
		}
		if err := os.WriteFile(filepath.Join(expected, "ask_"+svc.ID+".json"), buf.Bytes(), 0o644); err != nil {
			return n, err
		}
		n++
		if _, err := ls.store.GetRunbook(svc.ID); err != nil {
			continue
		}
		vr, err := verifier.VerifyService(svc.ID)
		if err != nil {
			return n, err
		}
		buf.Reset()
		if err := output.JSON(&buf, vr); err != nil {
			return n, err
		}
		if err := os.WriteFile(filepath.Join(expected, "verify_"+svc.ID+".json"), buf.Bytes(), 0o644); err != nil {
			return n, err
		}
		n++
	}
	if n == 0 {
		return 0, fail(1, "pack has no services to golden")
	}
	return n, nil
}

func provePackReplay(path string) error {
	ls, err := storeFromFixtureDir(path)
	if err != nil {
		return err
	}
	defer ls.cleanup()
	root := ls.root
	if root == "" {
		root = path
	}
	expected := filepath.Join(root, "expected")
	entries, err := os.ReadDir(expected)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()
		want, err := os.ReadFile(filepath.Join(expected, name))
		if err != nil {
			return err
		}
		got, err := replayGolden(ls, name)
		if err != nil {
			return err
		}
		wantNorm := bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
		gotNorm := bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
		if !bytes.Equal(wantNorm, gotNorm) {
			return fail(1, "%s: %s", name, firstDiff(wantNorm, gotNorm))
		}
	}
	return nil
}

func replayGolden(ls *loadedStore, name string) ([]byte, error) {
	switch {
	case strings.HasPrefix(name, "ask_") && strings.HasSuffix(name, ".json"):
		id := strings.TrimSuffix(strings.TrimPrefix(name, "ask_"), ".json")
		res, err := ask.Ask(ls.store, id, ask.Options{Since: time.Hour, Now: ls.now, WithRunbook: true})
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := output.JSON(&buf, res); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	case strings.HasPrefix(name, "verify_") && strings.HasSuffix(name, ".json"):
		id := strings.TrimSuffix(strings.TrimPrefix(name, "verify_"), ".json")
		vr, err := runbook.NewVerifier(ls.store, ls.now).VerifyService(id)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := output.JSON(&buf, vr); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, nil
	}
}
