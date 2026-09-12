package main

import (
	"github.com/sanjeev0120test/opsgraph/internal/version"
	"github.com/spf13/cobra"
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "opsgraph",
		Short: "Evidence-backed incident context for on-call engineers",
		Long: "opsgraph gathers what changed, what's affected, who owns it, and whether the\n" +
			"runbook is still valid - from local git, a Kubernetes snapshot, and fixtures.\n" +
			"It is free, offline-first, and needs no accounts or secrets.\n\n" +
			"Unique property: the same evidence IDs and JSON on any OS, with no SaaS or LLM.\n" +
			"Validate with: opsgraph prove",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version.String(),
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			printStartHere(cmd)
			return nil
		},
	}
	root.AddGroup(&cobra.Group{ID: "core", Title: "Core Incident Commands"})
	root.AddGroup(&cobra.Group{ID: "fleet", Title: "Fleet & Topology"})
	root.AddGroup(&cobra.Group{ID: "signals", Title: "Signals & Evidence"})
	root.AddGroup(&cobra.Group{ID: "ops", Title: "Ops & Tooling"})

	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add("core",
		newAskCmd(), newOpenCmd(), newReceiptCmd(), newDeltaCmd(),
		newWhyCmd(), newExplainCmd(), newHandoffCmd(),
		newScoreCmd(), newFingerprintCmd(), newVerifyRunbookCmd(),
		newReportCmd(), newExportCmd(), newWatchCmd(),
	)
	add("fleet",
		newServicesCmd(), newOwnersCmd(), newHealthCmd(), newTopCmd(),
		newBlastCmd(), newImpactCmd(), newPathCmd(), newGraphCmd(),
		newCompareCmd(), newResolveCmd(), newWhoCmd(),
	)
	add("signals",
		newChangesCmd(), newAlertsCmd(), newTimelineCmd(), newEvidenceCmd(),
	)
	add("ops",
		newDemoCmd(), newProveCmd(), newIngestCmd(), newInitCmd(), newPackCmd(), newStatusCmd(), newDoctorCmd(),
		newTestCmd(), newValidateFixtureCmd(), newCompletionCmd(), newVersionCmd(),
	)
	return root
}

func printStartHere(cmd *cobra.Command) {
	cmd.Print(`opsgraph — portable incident evidence (offline, no account)

  1. Prove it   opsgraph prove
  2. Dump k8s   kubectl get deploy,statefulset,daemonset,job,event -o yaml > k8s-snapshot.yaml
  3. Ask        opsgraph ask
  4. Share      opsgraph pack          → incident.opsgraph
  5. Open/diff  opsgraph incident.opsgraph
                opsgraph delta a.opsgraph b.opsgraph
                opsgraph receipt

More: opsgraph --help   ·   docs: https://github.com/sanjeev0120test/opsgraph
`)
}
