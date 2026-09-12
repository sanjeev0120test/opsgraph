# Architecture

`opsgraph` is a single static Go binary. Deterministic incident context is the core path; optional local AI enrichment never affects goldens/CI.

## Packages

- `cmd/opsgraph` — cobra CLI (`ask`, `open`, `receipt`, `delta`, `prove`, `pack`, fleet helpers, `demo`, `test`, `status`, …).
- `internal/model` — shared domain types (`AskResult`, services, alerts, evidence).
- `internal/config` — `.opsgraph.yaml` loader with defaults.
- `internal/store` — pure-Go SQLite (`modernc.org/sqlite`), opened with `_defensive=1`; `PRAGMA user_version` gated (current schema v2; v1→v2 adds alert/change indexes).
- `internal/ingest` — fixtures, git (inferred monorepo paths, no catalog), native kubectl YAML (Deployment/StatefulSet/DaemonSet/Event), `WritePack` + stdlib zip packs, optional Prometheus/Alertmanager/Helm, local connector plugins (pack YAML on stdout).
- `internal/ask` — blast radius, timeline, recommendations R1–R6.
- `internal/runbook` — Markdown parse + check catalog (`opsgraph:check=`); unmarked steps get inferred checks from wording.
- `internal/ai` — optional Ollama + chromem-go RAG; stubbed in tests.
- `internal/{score,explain,report,graphviz,pathfind,impact,fingerprint}` — enterprise helpers.
- `fixtures/` — embedded `incident_checkout` pack for `demo`; disk-only `fleet_healthy` for strict healthy-path contracts.

## Data flow

1. Resolve source (same rules for `ask` / `status` / fleet helpers):
   - `--fixture` → ephemeral fixture pack (directory or `.zip`/`.opsgraph`)
   - explicit `--data-dir` → persistent store only
   - else if config has a *usable* kubernetes snapshot, Prometheus/Alertmanager URL, and/or enabled plugin → live scrape (ephemeral)
   - else populated `state.db` under `data_dir` (resolved relative to the config file when set in YAML)
   - else a `k8s-snapshot.yaml` (or `services/` / `apps/` git layout) in cwd when no config file
   - else live config seed (git + services) when `.opsgraph.yaml` exists
   - Git alone, seed-only / empty snapshot scrapes, or quiet Prom/AM (reachable, zero alerts) do **not** prefer live over a richer populated store.
   - Prom/AM scrape failures hard-fail; `ask`/`status` fall back to populated `state.db` with a stderr warning. `--data-dir` always forces the store.
2. Upsert entities into SQLite.
3. `ask` assembles owner, changes, alerts, 1-hop blast, runbook verify, timeline, recommendations, evidence (keeps live alerts even with skewed StartsAt; drops future resolved/historical rows; suppressed AM alerts are visible but not “active”).
4. Render table or JSON via `internal/output` (`SetEscapeHTML(false)`, indent, trailing newline).

## Cross-platform

`CGO_ENABLED=0`, `filepath` for OS paths, `fs.FS` + forward slashes for fixtures, LF via `.gitattributes`. CI covers ubuntu-24.04/macOS/windows, native linux/arm64 smoke, race+coverage floor on ubuntu, plus linux/darwin/windows × amd64/arm64 cross-builds. Ubuntu is the full suite; Windows/macOS/arm64 use `go test -short` so wall-clock stays on the ubuntu race job.

## Kubernetes snapshot

The default binary never shells out to `kubectl` and never links `k8s.io/*`.
Operators dump YAML themselves. The parser accepts:

1. Native API objects: `kind: Deployment`, `kind: StatefulSet`,
   `kind: DaemonSet`, `kind: Event`, or `kind: List` of those
   (including multi-document `---` streams). `service_id` is inferred from
   `app.kubernetes.io/name` / `app` labels, else the object name (ReplicaSet/Pod
   controller hashes that contain a digit are stripped; StatefulSet pods
   `name-0` map to `name`).
2. The opsgraph dialect (`deployments:` / `events:` with optional `service_id`).
   A missing `service_id` falls back to `name` so simplified dumps still ingest.

A single List file can supply both Deployments and Events; a sibling `events.yaml`
is merged when present.

## Connector plugins

A plugin is a local command listed in `.opsgraph.yaml`. It prints the same pack
YAML a fixture uses (`services`, `owners`, `changes`, `dependencies`, `alerts`).
Output is ingested by the fixture parser, so it packs, replays and diffs like
any other source. Commands are never discovered from PATH. A failing plugin is
a hard error. Kubernetes scrape/prune semantics stay on the snapshot connector.

## Incident packs

`opsgraph pack` serializes the current store into a fixture directory (`meta.yaml` + entities + reconstructed k8s + runbooks + `expected/*.json`) and immediately re-ingests it to prove bit-identical `ask`/`verify` JSON. `--out incident.opsgraph` wraps that directory in a stdlib zip with sorted names and a fixed mtime so the SHA-256 is stable across OS. Email the folder or the one file; `opsgraph test` / `opsgraph ask --fixture` work on either.

`opsgraph prove` is the zero-setup validator: embedded checkout → pack → replay dir → zip → replay zip. Same evidence IDs, same JSON, no cluster, no account.

`opsgraph incident.opsgraph` opens a pack (hottest service). `opsgraph receipt` is the pasteable ID list. `opsgraph delta a b` diffs two packs by evidence ID set — the offline org workflow (morning vs now) that live-cluster LLM tools do not ship.
