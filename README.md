# opsgraph

[![CI](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml/badge.svg)](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml)

Free, offline-first incident context for engineers.

Evidence-backed timeline, blast radius, runbooks, severity scoring, and optional local AI - no accounts, no cloud LLM, no paid services.

```bash
# Prefer attested GitHub Release installers (see docs/USAGE.md).
# Dev-only tip-of-tree (no release ldflags):
go install github.com/sanjeev0120test/opsgraph/cmd/opsgraph@latest
opsgraph prove                 # offline proof: same JSON + .opsgraph hash on any OS
opsgraph demo

# Real cluster, no service catalog (kubectl YAML as-is):
kubectl get deploy,event -o yaml > k8s-snapshot.yaml
opsgraph ask                   # cwd dump is enough; hottest service
opsgraph pack                  # writes incident.opsgraph; email one file
opsgraph test incident.opsgraph

# From a clone, deterministic fixture:
opsgraph ask --fixture fixtures/incident_checkout
opsgraph health --fixture fixtures/fleet_healthy --strict
```

Docs: [Usage](docs/USAGE.md) | [Architecture](docs/ARCHITECTURE.md) | [Runbook format](docs/RUNBOOK_FORMAT.md) | [Contributing](CONTRIBUTING.md) | [Security](SECURITY.md) | [Releases](https://github.com/sanjeev0120test/opsgraph/releases)
