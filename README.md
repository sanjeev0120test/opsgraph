# opsgraph

[![CI](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml/badge.svg)](https://github.com/sanjeev0120test/opsgraph/actions/workflows/ci.yml)

A single offline binary that turns a Kubernetes dump (and optional git / local
signals) into an evidence-backed incident: timeline, blast radius, runbook
status, severity, and a file you can email. No account. No cluster API from
the tool. No cloud LLM.

## The problem

An on-call page arrives. The service is degraded. In the next ten minutes you
are not looking for an LLM essay — you are looking for four facts:

1. What changed in the last hour?
2. What is unhealthy upstream or downstream?
3. Is the runbook still valid against *this* state?
4. What can I paste into Slack so the next person sees the same evidence?

Today that means a pager, a dashboard, `kubectl`, git log, a runbook wiki, and
a ticket. Each tool has a different clock and a different name for the same
service. The handoff is a screenshot. Replay tomorrow is "I think we looked at
checkout."

Existing IRM and k8s-GPT tools usually want a service catalog, a live
kubeconfig, or a SaaS session. That is the opposite of a laptop on a plane, or
a contractor you cannot give cluster credentials.

## What it does

`opsgraph ask` reads what you already have — a `kubectl get … -o yaml` file,
optional git history, optional local Prometheus/Alertmanager, optional local
plugins — and prints one deterministic answer: service, owner, recent changes,
alerts, blast radius, runbook roll-up, timeline, recommendations, evidence IDs.

`opsgraph pack` writes `incident.opsgraph`. Anyone with the binary can open
that file, get the same evidence IDs, and `delta` it against this morning's
pack. The SHA-256 is stable across Windows, macOS, and Linux.

## Why you should care

- **Minutes, not a catalog.** Dump YAML. Run `ask`. No CMDB import.
- **The same answer twice.** Fixture clock + sorted JSON. `prove` checks it.
- **Handoff is a file**, not a chat log. Email `incident.opsgraph`.
- **Kubernetes as it actually runs.** Deployments, StatefulSets, DaemonSets,
  and Events. A database at 0/3 ready is a service, not silence.
- **Offline by default.** No required secrets. Optional AI is local Ollama only.

## What it is / is not

**Is:** a local CLI. Deterministic engine first. Pack format second.

**Is not:** a live cluster agent, a pager, a SaaS IRM, or a replacement for
`kubectl`. It never talks to the API server. You bring the YAML.

### When to use it

- Something is on fire and you have (or can take) a kubectl dump.
- You need a pasteable evidence list for Slack / ticket / handoff.
- You want morning-vs-now without an IRM tenant.
- CI should fail if a fixture incident no longer matches goldens.

### When not to use it

- You need live watch against the API server (no client-go in this binary).
- You need auto-remediation. Recommendations are text, not actions.
- You need a web UI or a multi-user workspace.
- Your only signal is a SaaS that cannot dump to a file.

### Explicit non-goals

Paid connectors, cloud LLMs, MCP, a web UI, `kubectl` exec from the binary,
and linking `k8s.io/*`. Those stay out so the default build stays one static
file with `CGO_ENABLED=0`.

## Key capabilities

| Capability | What you get |
|---|---|
| Zero-catalog ask | Hottest service from a cwd `k8s-snapshot.yaml` |
| Workloads | Deployment, StatefulSet, DaemonSet + Events |
| Blast / impact | 1-hop and recursive downstream |
| Runbooks | Markdown checks, inferred if unmarked |
| Pack / prove / delta | Emailable, hash-stable, replayable |
| Plugins | Local commands that print pack YAML |
| Optional AI | Ollama only; ignored by `test` / goldens |

## Quick start (no install)

You need [Go](https://go.dev/dl/) 1.26+ and a clone. This is the fastest way
to see a real answer, on Windows or macOS.

```bash
git clone https://github.com/sanjeev0120test/opsgraph.git
cd opsgraph
go build -o bin/opsgraph ./cmd/opsgraph   # Windows: bin\opsgraph.exe
```

Use `./bin/opsgraph` below (`.\bin\opsgraph.exe` on Windows). `go run ./cmd/opsgraph …` is the same if you skip the build.

**1. Prove the engine (offline, no cluster, no account)**

```bash
./bin/opsgraph prove
```

Expect `ok`, service `checkout`, and a SHA-256. That hash is the trust check: CI
asserts the same bytes on ubuntu, macOS, and Windows.

**2. Walk the built-in incident**

```bash
./bin/opsgraph demo
./bin/opsgraph ask checkout --fixture fixtures/incident_checkout
./bin/opsgraph pack --fixture fixtures/incident_checkout
./bin/opsgraph test incident.opsgraph
./bin/opsgraph receipt incident.opsgraph
```

`pack` with no `--fixture` only works when the current directory already has a
dump or a config. From a fresh clone, pass the fixture (or dump YAML first).

**3. Optional: your cluster (read-only dump)**

```bash
kubectl get deploy,statefulset,daemonset,event -o yaml > k8s-snapshot.yaml
./bin/opsgraph ask
./bin/opsgraph pack --force
```

`ask` with no name picks the hottest service. StatefulSets and DaemonSets are
first-class: `postgres` at 0/3 ready is `unhealthy`, not "service not found".

## Installation

Skip this until the quick start worked, unless you want `opsgraph` on PATH.

**From source (tip of `main` — `@latest` is the newest *tag*, often older):**

```bash
go install github.com/sanjeev0120test/opsgraph/cmd/opsgraph@main
# Binary lands in $(go env GOPATH)/bin — add that to PATH if `opsgraph` is not found.
opsgraph prove
```

**GitHub Release installers** resolve the latest *tag*, verify `SHA256SUMS`,
and write a binary. They only include what that tag contains. See
[releases](https://github.com/sanjeev0120test/opsgraph/releases). To verify
the installer script before running it, follow [Install](docs/USAGE.md#install).

```bash
# Linux / macOS (~/.local/bin)
curl -fsSL https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.sh | bash
```

```powershell
# Windows (%LOCALAPPDATA%\opsgraph\bin)
irm https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.ps1 | iex
```

Then: `opsgraph version` and `opsgraph prove`.

## How it works

1. You dump signals (kubectl YAML, optional git, optional Prom/AM, optional plugin).
2. Ingest upserts into an ephemeral or persistent SQLite store (pure Go).
3. `ask` resolves the service, scores blast/runbook/alerts, builds a timeline.
4. `pack` writes a fixture + goldens and immediately replays them.
5. Someone else runs `opsgraph incident.opsgraph` or `delta`.

Kubernetes parsing is native YAML (`kind: List` / `Deployment` / `StatefulSet`
/ `DaemonSet` / `Event`). Health is replica readiness
(`readyReplicas` / `desiredNumberScheduled`). No `kubectl` from the process.

## Real-world usage

**Checkout is 503.** Dump workloads + events. `opsgraph ask` (or `ask checkout`).
Read changes ≤30m (R1), unhealthy upstream (R2), firing alert (R3), stale
runbook (R4). Paste `opsgraph receipt` into the channel. Pack the file for the
EU morning shift.

**Postgres StatefulSet is 0/3.** Same dump. `ask postgres` — do not also need
a Deployment of that name.

**Compare 09:00 vs now.** Keep `morning.opsgraph`. Pack again. `opsgraph delta
morning.opsgraph incident.opsgraph` exits 1 if evidence IDs changed.

**CI of a known incident.** Check in `fixtures/incident_checkout` (or your pack).
`opsgraph test ./fixtures/incident_checkout` fails if goldens drift.

## How to integrate

- **On-call script:** dump YAML → `ask` → `pack` → attach `incident.opsgraph`.
- **Repo:** `opsgraph init --k8s k8s-snapshot.yaml` writes `.opsgraph.yaml`.
- **CI:** `opsgraph prove` or `opsgraph test <pack>`. No secrets.
- **Site-specific signals:** a plugin command (below). Not a new binary protocol.

## Configuration

Everything is optional. `demo`, `prove`, and `test` need no file.

Copy [`.opsgraph.example.yaml`](.opsgraph.example.yaml) to `.opsgraph.yaml`, or:

```bash
opsgraph init --k8s k8s-snapshot.yaml
```

Useful keys: `data_dir`, `default_since`, `connectors.kubernetes.snapshot`,
`connectors.git.repo_path`, `connectors.plugins`, `ai.enabled` (Ollama).

Environment (flags win): `OPSGRAPH_CONFIG`, `OPSGRAPH_DATA_DIR`, `OPSGRAPH_FIXTURE`.

## Extension / plugin model

A plugin is **not** a shared library and is **not** loaded from PATH. It is a
command you list in config. It prints opsgraph pack YAML on stdout:

```yaml
connectors:
  plugins:
    - name: deploys
      command: ["./bin/opsgraph-deploys"]
      enabled: true
      timeout: 10s
```

Accepted top-level keys: `services`, `owners`, `changes`, `dependencies`,
`alerts`. Kubernetes scrape/prune stays on the snapshot connector — plugins
set `services[].health` instead.

Rules that keep this safe during an incident:

- Only listed commands run.
- Empty stdout is OK (nothing to report).
- Non-zero exit, timeout, or unknown YAML key fails the run. Missing signals
  are not silently dropped.
- Rows are tagged `plugin:<name>` so receipt/delta stay attributable.

## CLI reference

| Command | Purpose |
|---|---|
| `prove` | Offline proof: same IDs + pack hash |
| `demo` | Built-in checkout incident |
| `ask [service]` | Context; omit name = hottest |
| `init` | Write `.opsgraph.yaml` |
| `pack` | Write `incident.opsgraph` |
| `open` / `incident.opsgraph` | Open a pack |
| `receipt` | Pasteable IDs + score |
| `delta a b` | Evidence-ID diff (exit 1 if changed) |
| `test <pack>` | Compare goldens |
| `verify-runbook` | Runbook vs current state |
| `ingest` / `status` / `doctor` | Load / inspect / env check |
| `health --strict` | Fail if any service is not healthy |
| `services`, `top`, `blast`, `impact` | Fleet |
| `why`, `handoff`, `explain`, `score` | Narrative / severity |

`opsgraph` with no args prints the five-step start-here. Full flags:
`opsgraph --help` and [docs/USAGE.md](docs/USAGE.md).

Exit codes: `0` success, `1` not-found / golden mismatch / unhealthy, `2` usage
or config error.

## Reliability / failure modes

| Situation | What happens |
|---|---|
| No dump, no fixture, empty store | Exit 2, tells you to dump / ingest / `--fixture` |
| Dump has only Services/CronJobs | Stderr lists kinds found; no fake healthy fleet |
| Prom/AM scrape fails | Hard error; `ask` falls back to populated `state.db` |
| Plugin fails or times out | Hard error, stderr quoted |
| Ollama missing with `--ai` | Deterministic result + "AI unavailable" |
| Pack from a live dump | Goldens are taken from the pack replay, not the live clock |
| Same Deployment name in two namespaces | Rollout ids include namespace |

`watch` polls a fixture or live/persisted source; it is not a cluster watch API.

## Security and privacy

- No telemetry. No accounts.
- Default path never opens a kubeconfig.
- Installers verify SHA-256; releases carry provenance attestation.
- Plugins run **your** command with **your** cwd. Treat them like any script
  you would run on the laptop that holds the dump.
- Do not commit dumps that contain Secrets. `export` sanitizes known secret
  keys; it is not a redaction guarantee.

See [SECURITY.md](SECURITY.md).

## Enterprise considerations

- Deterministic JSON contracts and golden `opsgraph test` in CI.
- `--format json` on inspection commands for automation.
- License allowlist + govulncheck + CodeQL on PRs. No required SaaS.
- Live-cluster client-go remains deferred; snapshot is the supported path.
- GHES: workflows use `./` local actions after checkout (not github.com-only `$/`).

## Compatibility / prerequisites

- OS: Windows, macOS, Linux (amd64/arm64). `CGO_ENABLED=0`.
- Go 1.26+ to build. Runtime: the static binary only.
- `kubectl` only if you want a live dump. Optional: git repo, Prom/AM, Ollama.
- No Docker required for core / CI.

## Project structure

```
cmd/opsgraph/     CLI
internal/ingest/  fixtures, k8s YAML, git, plugins, pack
internal/ask/     timeline, blast, R1–R6
internal/runbook/ parse + verify
fixtures/         incident_checkout, fleet_healthy
docs/             USAGE, ARCHITECTURE, RUNBOOK_FORMAT
```

## Troubleshooting

| Symptom | Check |
|---|---|
| `service not found` | Dump includes that workload kind? Label / name inference? |
| Empty fleet | `doctor`; dump kinds listed on stderr? |
| `pack` replay failed | Update to a build that goldens the pack itself (not the live store) |
| Installer missing commands | That tag is older than the commands; use `go install …@main` or a newer tag |
| Prom/AM empty answers | Scrape failed → fallback store; or `--data-dir` |
| Plugin "unknown section" | Only the five pack keys; no `deployments:` |
| `prove` hash drifted | Only happens if pack bytes change; CI freezes the hash |

`opsgraph doctor` and `opsgraph status` are the first two commands after a
bad day.

## Observability / logging

There is no daemon. Diagnostics go to **stderr**; machine output stays on
**stdout** so `ask --format json` is pipe-clean. Warnings are prefixed
`warning:`. `status --format json` and `doctor --format json` are the
automation surfaces.

## Upgrade / migration

- Config `version: 1` is current. Extra keys are ignored.
- Schema: store opens with `PRAGMA user_version`; v1→v2 is automatic.
- Pack files from older builds still `open` / `test` if goldens match.
- Pin a release tag in installers. Tip of `main` is `go install …@main`.
  `go install …@latest` is the highest semver tag, not the branch.

## Can I trust it?

`opsgraph prove` is the contract: same evidence IDs and the same `.opsgraph`
SHA-256 on any OS, no cluster. CI runs that on ubuntu, macOS, and Windows.
Read [Architecture](docs/ARCHITECTURE.md) if you need the pipeline, not the pitch.

Docs: [Usage](docs/USAGE.md) · [Architecture](docs/ARCHITECTURE.md) · [Runbook format](docs/RUNBOOK_FORMAT.md) · [Contributing](CONTRIBUTING.md) · [Security](SECURITY.md) · [Releases](https://github.com/sanjeev0120test/opsgraph/releases)
