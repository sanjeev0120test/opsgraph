# opsgraph

[![CI](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml/badge.svg)](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml)

Free, offline-first incident context for engineers.

Evidence-backed timeline, blast radius, runbooks, severity scoring, and optional local AI - no accounts, no cloud LLM, no paid services.

## Install

Installers resolve the latest release and verify every download against the
release `SHA256SUMS` before writing anything.

```bash
# Linux / macOS
curl -fsSL https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.sh | bash
```

```powershell
# Windows PowerShell
irm https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.ps1 | iex
```

To verify the installer itself before running it, or to build from source, see
[Install](docs/USAGE.md#install).

## Validate it works (offline, no cluster, no account)

```bash
opsgraph prove                 # same evidence IDs + .opsgraph SHA-256 on any OS
```

## Use

```bash
opsgraph demo

# Real cluster, no service catalog (kubectl YAML as-is):
kubectl get deploy,statefulset,daemonset,event -o yaml > k8s-snapshot.yaml
opsgraph ask                   # cwd dump is enough; hottest service
opsgraph pack                  # writes incident.opsgraph; email one file
opsgraph incident.opsgraph     # open the emailed file
opsgraph receipt               # pasteable evidence IDs + score
opsgraph delta a.opsgraph b.opsgraph
opsgraph test incident.opsgraph

# From a clone, deterministic fixture:
opsgraph ask --fixture fixtures/incident_checkout
opsgraph health --fixture fixtures/fleet_healthy --strict
```

Docs: [Usage](docs/USAGE.md) | [Architecture](docs/ARCHITECTURE.md) | [Runbook format](docs/RUNBOOK_FORMAT.md) | [Contributing](CONTRIBUTING.md) | [Security](SECURITY.md) | [Releases](https://github.com/sanjeev0120test/opsgraph/releases)
