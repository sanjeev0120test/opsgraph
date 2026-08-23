package main

import (
	"os"
	"path/filepath"

	"github.com/sanjeev0120test/opsgraph/internal/config"
	"github.com/sanjeev0120test/opsgraph/internal/model"
	"github.com/sanjeev0120test/opsgraph/internal/output"
	"github.com/sanjeev0120test/opsgraph/internal/runbook"
	"github.com/spf13/cobra"
)

func newVerifyRunbookCmd() *cobra.Command {
	var (
		fixture    string
		configPath string
		dataDir    string
		format     string
	)
	cmd := &cobra.Command{
		Use:               "verify-runbook <service-or-file>",
		Short:             "Check whether a service's runbook is still valid",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeServiceArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validFormat(format); err != nil {
				return fail(2, "%v", err)
			}
			if fixture != "" && dataDir != "" {
				return fail(2, "--fixture and --data-dir are mutually exclusive")
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return fail(2, "%v", err)
			}
			if err := requireArg("service-or-file", args[0]); err != nil {
				return err
			}
			ls, err := loadAskStore(cmd.Context(), fixture, configPath, dataDir, cfg, cfg.Since())
			if err != nil {
				return failSource(err)
			}
			defer ls.cleanup()

			res, err := verifyTarget(ls, args[0])
			if err != nil {
				return err
			}

			if format == "json" {
				if err := output.JSON(cmd.OutOrStdout(), res); err != nil {
					return err
				}
			} else if err := output.VerifyTable(cmd.OutOrStdout(), res); err != nil {
				return err
			}

			switch res.Status {
			case model.StatusPass, model.StatusManual:
				// Manual-only runbooks are valid; nothing automated failed.
				return nil
			case model.StatusStale, model.StatusFail, model.StatusMissing:
				return &exitError{code: 1, err: errStatus(res.Status)}
			default:
				return &exitError{code: 2, err: errStatus(res.Status)}
			}
		},
	}
	cmd.Flags().StringVar(&fixture, "fixture", "", "path to a fixture pack directory")
	cmd.Flags().StringVar(&configPath, "config", "", "path to .opsgraph.yaml")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "read from a persistent store (from `opsgraph ingest`)")
	cmd.Flags().StringVar(&format, "format", "table", "output format: table|json")
	bindFormatCompletion(cmd)
	return cmd
}

// verifyTarget resolves args[0] as an existing file path first, else as a
// service name/alias against the loaded store.
func verifyTarget(ls *loadedStore, target string) (model.VerifyResult, error) {
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		data, err := os.ReadFile(target)
		if err != nil {
			return model.VerifyResult{}, fail(2, "read runbook %q: %v", target, err)
		}
		rb, _, err := runbook.ParseWithInfer(data, filepath.ToSlash(target))
		if err != nil {
			return model.VerifyResult{}, fail(2, "parse runbook %q: %v", target, err)
		}
		if rb.ServiceID == "" {
			return model.VerifyResult{}, fail(2, "runbook %q has no service in front matter", target)
		}
		res, err := runbook.NewVerifier(ls.store, ls.now).Verify(rb)
		if err != nil {
			return model.VerifyResult{}, fail(2, "%v", err)
		}
		return res, nil
	}

	svc, err := ls.store.GetServiceByNameOrAlias(target)
	if err != nil {
		return model.VerifyResult{}, failLookup(target, err)
	}
	res, err := runbook.NewVerifier(ls.store, ls.now).VerifyService(svc.ID)
	if err != nil {
		return model.VerifyResult{}, fail(2, "%v", err)
	}
	return res, nil
}

type statusErr string

func (e statusErr) Error() string { return "runbook status: " + string(e) }

func errStatus(s string) error { return statusErr(s) }
