package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sanjeev0120test/opsgraph/internal/ask"
	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/ingest"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/store"
	"github.com/spf13/cobra"
)

func newIngestCmd() *cobra.Command {
	var (
		fixture    string
		configPath string
		dataDir    string
		replace    bool
		merge      bool
		since      time.Duration
		format     string
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Load fixture or live connectors into the local store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if err := validSince(since); err != nil {
				return fail(2, "%v", err)
			}
			if replace && merge {
				return fail(2, "use either --replace or --merge, not both")
			}
			if fixture == "" {
				fixture = strings.TrimSpace(os.Getenv("OPSGRAPH_FIXTURE"))
			}
			if fixture != "" && merge {
				return fail(2, "--merge applies to live ingest only; fixtures always replace")
			}
			cfgPath := configPathOrEnv(configPath)
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fail(2, "%v", err)
			}
			effPath := resolveConfigPath(cfgPath)
			configDir := "."
			if effPath != "" {
				configDir = filepath.Dir(effPath)
			}
			dir := resolveDataDir(dataDir, cfg, configDir)
			s, err := store.Open(dir)
			if err != nil {
				return fail(2, "%v", err)
			}
			defer s.Close()

			// Fixtures always replace. Live ingest replaces by default so
			// removed entities cannot linger; pass --merge to upsert-only.
			doReset := fixture != "" || replace || !merge

			lookback := since
			if lookback <= 0 {
				lookback = cfg.Since()
			}

			now := nowUTC()
			liveClock := false
			switch {
			case fixture != "":
				if doReset {
					if err := s.Reset(); err != nil {
						return fail(2, "reset store: %v", err)
					}
				}
				now, err = ingest.IngestFixtureDir(s, fixture)
			default:
				if effPath == "" {
					return fail(2, "no data source: pass --fixture <pack> or add a .opsgraph.yaml")
				}
				sinceAt := now.Add(-ask.ChangeLookback(lookback))
				tmp, cleanup, terr := store.OpenTemp()
				if terr != nil {
					return fail(2, "%v", terr)
				}
				terr = ingest.LiveIngest(cmd.Context(), tmp, cfg, configDir, sinceAt, now)
				if terr != nil {
					cleanup()
					return fail(2, "%v", terr)
				}
				if doReset {
					terr = s.ReplaceFromFile(tmp.Path())
					cleanup()
					if terr != nil {
						return fail(2, "%v", terr)
					}
				} else {
					terr = s.MergeFrom(tmp)
					cleanup()
					if terr != nil {
						return fail(2, "%v", terr)
					}
				}
				if err := s.ClearAsOf(); err != nil {
					return fail(2, "%v", err)
				}
				liveClock = true
				err = nil
			}
			if err != nil {
				return fail(2, "%v", err)
			}
			if collisions, cerr := s.FindAliasCollisions(); cerr != nil {
				return fail(2, "%v", cerr)
			} else if len(collisions) > 0 {
				return fail(1, "ambiguous service aliases after ingest: %s", strings.Join(collisions, "; "))
			}
			countsCheck, cerr := s.Counts()
			if cerr != nil {
				return fail(2, "%v", cerr)
			}
			if countsCheck["services"] == 0 {
				return fail(1, "ingest produced zero services")
			}

			counts, err := s.Counts()
			if err != nil {
				return fail(2, "%v", err)
			}
			mode := "replace"
			switch {
			case fixture != "":
				mode = "fixture"
			case merge:
				mode = "merge"
			}
			type ingestOut struct {
				Path       string         `json:"path"`
				Mode       string         `json:"mode"`
				Schema     int            `json:"schema"`
				Counts     map[string]int `json:"counts"`
				AsOf       string         `json:"as_of,omitempty"`
				IngestedAt string         `json:"ingested_at,omitempty"`
			}
			payload := ingestOut{
				Path:   s.Path(),
				Mode:   mode,
				Schema: store.SchemaVersion,
				Counts: counts,
			}
			if liveClock {
				payload.IngestedAt = now.UTC().Format(time.RFC3339)
			} else {
				payload.AsOf = now.UTC().Format(time.RFC3339)
			}
			if format == "json" {
				return output.JSON(cmd.OutOrStdout(), payload)
			}
			cmd.Printf("ingested into %s (schema v%d, mode %s)\n", s.Path(), store.SchemaVersion, mode)
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				cmd.Printf("  %-13s %d\n", k+":", counts[k])
			}
			if liveClock {
				cmd.Printf("  ingested_at:  %s\n", payload.IngestedAt)
				cmd.Printf("  as_of:        (wall clock)\n")
			} else {
				cmd.Printf("  as_of:        %s\n", payload.AsOf)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "path to a fixture pack directory (or OPSGRAPH_FIXTURE)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to .opsgraph.yaml (or OPSGRAPH_CONFIG)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "persistent store directory (default: config data_dir or .opsgraph/data; or OPSGRAPH_DATA_DIR)")
	cmd.Flags().BoolVar(&replace, "replace", false, "clear the store before ingest (implied unless --merge)")
	cmd.Flags().BoolVar(&merge, "merge", false, "upsert without clearing prior rows (live ingest only; fixtures always replace)")
	cmd.Flags().DurationVar(&since, "since", 0, "change lookback for live git/helm ingest (default: config default_since)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}
