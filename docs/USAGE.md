# Usage

`opsgraph` is a single static binary. Build it once:

```bash
go build -o bin/opsgraph ./cmd/opsgraph   # or: make build
```

`opsgraph` with no args prints a start-here (prove → dump → ask → pack → open/delta). Full help: `opsgraph --help`.
A pack path as the first argument is opened (`opsgraph incident.opsgraph` equals `opsgraph ask --fixture incident.opsgraph`).

Most inspection commands accept `--format table|json` (default `table`).
Exceptions: `graph` (`ascii|table|mermaid|json`), `export`/`report` (`json|markdown`), `watch` (`--once` supports `--format json`).
`status` / `doctor` / `ingest` / `pack` / `prove` / `open` / `receipt` / `delta` / `version` / `validate-fixture` / `test` / `why` / `handoff` / `explain` / `evidence` accept `--format json`.
`health --strict` exits `1` when any **real** service is degraded, unhealthy, or unknown (fail-closed). Dependency stubs and git-only unknown folders are ignored; JSON includes `"ok": true|false`.
Healthy-path CI pack: `fixtures/fleet_healthy` (all services healthy; use with `health --strict`).
`status` JSON includes `"ok"` / `"has_data"`; `path` JSON includes `"ok"` / `"found"` / `"direction"` (`depends_on` or `dependents`). Missing routes still exit `1` with a JSON envelope. `path auth order` works when the only edges are order → checkout → auth.
List commands treat `--limit 0` as unlimited (`changes`/`top`/`alerts`/`timeline`/`evidence`).
`export --meta` prints `{path,bytes,service,format}` for automation.

Environment defaults (flags win when set):
- `OPSGRAPH_CONFIG` — path to `.opsgraph.yaml`
- `OPSGRAPH_DATA_DIR` — persistent store directory
- `OPSGRAPH_FIXTURE` — fixture pack directory or `.zip` / `.opsgraph` archive

## Core commands

### `opsgraph demo`
Runs the built-in `incident_checkout` scenario end to end. No config, no network.

```bash
opsgraph demo
opsgraph demo --format json
opsgraph demo --ai            # local AI summary (offline fallback if Ollama absent)
```

### `opsgraph ask [service]`
Evidence-backed context for a service. Omit the name to auto-select the
hottest service by severity score (stderr prints what was picked so JSON
stdout stays pipe-clean).

```bash
# From a fixture pack (deterministic, offline):
opsgraph ask checkout --fixture fixtures/incident_checkout
opsgraph ask --fixture fixtures/incident_checkout

# CWD dump, no flags, no catalog:
kubectl get deploy,statefulset,daemonset,job,event -o yaml > k8s-snapshot.yaml
opsgraph ask

# From a persistent store (explicit; no live re-scrape):
opsgraph ask checkout --data-dir .opsgraph/data

# Live k8s/prom/AM connectors from .opsgraph.yaml (preferred over stale state.db):
opsgraph ask --since 90m
opsgraph ask checkout --format json --ai
```

Flags: `--fixture`, `--config`, `--data-dir`, `--since`, `--runbook` (default true), `--ai`, `--format`.
`--fixture` and `--data-dir` are mutually exclusive on read commands (`ask`, `status`, helpers).

Notes:
- Live alerts (`firing`/`pending`/`suppressed`) are always shown; `--since` filters resolved/historical alerts only. `suppressed` (Alertmanager silence) is visible but does not drive R3/score.
- Changes use at least a 60m lookback (covers the 30m prime-suspect window and change→alert correlation) even when `--since` is narrower. The reported window shows both when they differ (e.g. `10m (changes 60m)`).
- If kubernetes (with snapshot path) / Prometheus / Alertmanager (with URL) are configured in `.opsgraph.yaml`, `ask`/`status`/`why`/`watch` re-scrape them. Git-alone configs stay on the persisted store (run `opsgraph ingest` to refresh commits). Live prefer requires a real scrape signal (k8s/helm/prom/AM rows or sources, or a successful Prom/AM HTTP scrape). Config seed alone / empty snapshot dirs do **not** displace a populated store. Use `--data-dir` to force the persistent store. Config `data_dir` is resolved relative to the config file.
- Prometheus/Alertmanager scrape failures are hard errors: live mode falls back to a populated `state.db` (stderr warning) instead of answering with empty alerts. If AM is your paging source, keep `opsgraph ingest` healthy or force `--data-dir`.

Exit codes: `0` success, `1` service not found / empty store, `2` usage/config error.

Other exits: `status` with no data → `1`; `watch` timeout → `1` (bad config → `2`); `top` exits `1` only when every service fails to score.

### `opsgraph init`
Write `.opsgraph.yaml` for this repo. No service catalog required.

```bash
kubectl get deploy,statefulset,daemonset,job,event -o yaml > k8s-snapshot.yaml
opsgraph init --k8s k8s-snapshot.yaml
opsgraph ask
```

`--k8s` accepts a directory (`deployments.yaml` + optional `events.yaml`) or a
single kubectl YAML file (`kind: List` / `Deployment` / `StatefulSet` /
`DaemonSet` / `Event`, including `events.k8s.io/v1`). `--force` overwrites an
existing config. `--git` defaults to `.`.

Workload health comes from replica readiness: `spec.replicas` vs
`status.readyReplicas` for Deployments and StatefulSets, and
`status.desiredNumberScheduled` vs `status.numberReady` for DaemonSets. If a
snapshot contains none of these kinds, `opsgraph` says so on stderr and lists
the kinds it did find rather than reporting an empty fleet.

### `opsgraph prove`
One-command offline proof. Ingests the built-in incident, writes a pack, replays
the directory and a `.opsgraph` zip, and prints a SHA-256. No cluster, no
account. Anyone can run this and get the same evidence IDs + JSON.

```bash
opsgraph prove
opsgraph prove --format json
```

### `opsgraph pack`
Write a portable incident pack (fixture + goldens) from the current source.
Anyone can replay it with no cluster and get the same JSON.

```bash
opsgraph pack                      # writes ./incident.opsgraph
opsgraph pack --out ./incident
opsgraph pack --out ./incident.opsgraph    # single emailable file
opsgraph pack --fixture fixtures/incident_checkout --out ./incident
opsgraph test ./incident
opsgraph test ./incident.opsgraph
opsgraph incident.opsgraph             # drop the file on the binary
opsgraph receipt ./incident.opsgraph   # pasteable evidence IDs
opsgraph delta morning.opsgraph now.opsgraph
```

Default `--out` is `incident.opsgraph`. `--force` overwrites an existing pack (`meta.yaml` present or empty dir) or an
existing `.zip`/`.opsgraph`. Pack always self-checks replay before exiting 0.
Archive bytes are sorted + fixed-mtime so the SHA-256 is stable across OS.

### `opsgraph open [pack]` / `opsgraph receipt [pack]` / `opsgraph delta <before> <after>`
`open` is the pack viewer (hottest service). A pack path as argv[1] does the same.
`receipt` prints a pasteable summary: hottest service, score, evidence IDs, optional SHA-256.
`delta` diffs two packs by evidence ID set (exit 0 identical, 1 if they differ) — the org workflow that SaaS IRM does not offer offline.

```bash
opsgraph open incident.opsgraph
opsgraph receipt --fixture fixtures/incident_checkout --format json
opsgraph delta fixtures/incident_checkout fixtures/fleet_healthy --format json
```

### `opsgraph ingest`
Load a fixture pack (or live config sources) into a persistent data dir.

```bash
opsgraph ingest --fixture fixtures/incident_checkout --data-dir .opsgraph/data
opsgraph ingest --since 2h          # live connectors; change lookback (default: config default_since)
opsgraph ingest --replace           # default for live: atomic swap of validated scrape
opsgraph ingest --merge             # upsert; prunes removed config deps + stale k8s health; prefer --replace for full churn
opsgraph ask checkout --data-dir .opsgraph/data
```

`--merge` is live-only (fixtures always replace). Quiet Prom/AM scrapes (reachable, zero alerts) fall back to a richer persisted `state.db` instead of answering from config seed alone.

### `opsgraph verify-runbook <service>`
Checks whether a runbook is still valid against current state.

```bash
opsgraph verify-runbook checkout --fixture fixtures/incident_checkout
```

Exit codes: `0` pass or manual-only, `1` stale/fail/missing, `2` usage/error.

### `opsgraph handoff <service>` / `why` / `explain`
Short handoff note, one-line hypothesis, or multi-paragraph narrative.

```bash
opsgraph handoff checkout --fixture fixtures/incident_checkout
opsgraph why checkout --fixture fixtures/incident_checkout --format json
opsgraph explain checkout --fixture fixtures/incident_checkout --format json
```

### `opsgraph test <fixture>`
Compares `ask`/`verify` output against the pack's `expected/*.json` goldens.
`<fixture>` may be a directory or a `.zip`/`.opsgraph` archive.

```bash
opsgraph test ./fixtures/incident_checkout
opsgraph test ./incident.opsgraph
opsgraph test ./fixtures/incident_checkout --update
```

### `opsgraph status` / `opsgraph doctor` / `opsgraph version`
Store counts, environment checks, and build metadata. `status` uses the same source selection as `ask`, prints `ACTIVE SOURCE` (`fixture|live|persisted`), and probes Prometheus/Alertmanager/Ollama when configured. `doctor` verifies git, kubernetes snapshot (when enabled), opens `--data-dir` schema, probes Prom/AM endpoints (unreachable enabled URLs fail the doctor run), notes a cwd dump/layout when present, and hints `opsgraph init` when no config file is present. `opsgraph prove` is the zero-setup validator.

### `opsgraph alerts`
Fleet alert list. `--firing` keeps active (`firing`/`pending`) alerts; `--service <name>` filters by service; `--since` trims resolved/historical rows while live and `suppressed` alerts stay visible.

## Fleet / incident helpers

| Command | Purpose |
|---------|---------|
| `services`, `owners`, `health`, `top` | Inventory and ranking |
| `blast` | 1-hop upstream/downstream |
| `impact` | Recursive downstream impact |
| `changes`, `alerts`, `timeline` (`--limit`), `evidence` | Signal browsers |
| `explain`, `why`, `score`, `fingerprint` | Hypotheses / severity |
| `path`, `graph`, `compare`, `who`, `resolve` | Topology / ownership (`path` tries depends-on, then dependents) |
| `report`, `export`, `handoff` | Markdown/JSON/text handoff |
| `watch` | Poll until healthy (default interval 5s; live/persistent sources) |
| `validate-fixture`, `completion`, `init`, `pack`, `prove`, `open`, `receipt`, `delta` | Pack checks / proof / open emailed file / pasteable receipt / evidence-ID diff |

## Validation

- Heavy validation runs in GitHub Actions (ubuntu-24.04 + macos + windows, race+coverage floor on ubuntu, native linux/arm64 smoke, cross-compile 6 targets, install smoke, deadcode/govulncheck).
- Locally: `pwsh scripts/verify.ps1` (Windows) or `bash scripts/verify.sh` / `make quick` (Unix).

## Optional live connectors

In `.opsgraph.yaml` (see `.opsgraph.example.yaml`):

- `connectors.git` — local repo scan
- `connectors.kubernetes.snapshot` — directory or file. Accepts native `kubectl get -o yaml` (`kind: List` / `Deployment` / `StatefulSet` / `DaemonSet` / `Job` / `Event`, including multi-doc) and the opsgraph dialect (`deployments:` / `events:`). Optional Helm `releases.yaml`.
- `connectors.prometheus` / `connectors.alertmanager` — disabled by default
- `connectors.plugins` — local commands that print opsgraph pack YAML on stdout
  (`services`, `owners`, `changes`, `dependencies`, `alerts`). Never discovered
  from PATH. A failing plugin fails the run. Kubernetes workload YAML is not
  accepted here; dump that as a snapshot instead.

Optional cluster demo: `bash hack/kind-demo.sh`.

## Install

Precompiled binaries are published only in **GitHub Releases** (never committed
to the git source tree). Pushing a `v*` tag automatically runs the release
workflow: build 6 targets, package archives, write `SHA256SUMS`, attest
provenance, and publish the Release.

### Quick install (latest release)

Both installers resolve the latest tag, download the archive for your OS/arch,
and **verify it against the release `SHA256SUMS` before installing**. They abort
on any mismatch.

```bash
# Linux/macOS — lands in ~/.local/bin (add to PATH if needed)
curl -fsSL https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.sh | bash
```

```powershell
# Windows — lands in %LOCALAPPDATA%\opsgraph\bin (add to PATH if needed)
irm https://github.com/sanjeev0120test/opsgraph/releases/latest/download/install.ps1 | iex
```

Then confirm the install works, entirely offline:

```bash
opsgraph version
opsgraph prove
```

### Verify the installer before running it

If you would rather not pipe a script into a shell, download the installer and
check it against `SHA256SUMS` (which is covered by the release provenance
attestation) first:

```bash
BASE=https://github.com/sanjeev0120test/opsgraph/releases/latest/download
curl -fsSL "$BASE/SHA256SUMS" -o SHA256SUMS
curl -fsSL "$BASE/install.sh" -o install.sh
sha256sum -c SHA256SUMS --ignore-missing   # must print: install.sh: OK
chmod +x install.sh && ./install.sh
```

Pin a specific tag with `OPSGRAPH_VERSION=v1.2.3`, change the target directory
with `OPSGRAPH_INSTALL_DIR`, and verify provenance with
`gh attestation verify <archive> --repo sanjeev0120test/opsgraph`.

### From source

```bash
# Tip of main (not @latest — that is the newest release tag):
go install github.com/sanjeev0120test/opsgraph/cmd/opsgraph@main
```

Release page: https://github.com/sanjeev0120test/opsgraph/releases

## Enabling AI (optional)

1. Install [Ollama](https://ollama.com) and pull models (`qwen2.5:7b`, `nomic-embed-text`).
2. Set `ai.enabled: true` in `.opsgraph.yaml` (or pass `--ai`).
3. If Ollama is unavailable, `--ai` degrades to a deterministic offline summary.
