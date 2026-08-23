# Architecture

`opsgraph` is a single static Go binary. Deterministic incident context is the core path; optional local AI enrichment never affects goldens/CI.

## Packages

- `cmd/opsgraph` — cobra CLI (`ask`, fleet helpers, `demo`, `test`, `status`, …).
- `internal/model` — shared domain types (`AskResult`, services, alerts, evidence).
- `internal/config` — `.opsgraph.yaml` loader with defaults.
- `internal/store` — pure-Go SQLite (`modernc.org/sqlite`), opened with `_defensive=1`; `PRAGMA user_version` gated (current schema v2; v1→v2 adds alert/change indexes).
- `internal/ingest` — fixtures, git, k8s snapshot, optional Prometheus/Alertmanager/Helm.
- `internal/ask` — blast radius, timeline, recommendations R1–R6.
- `internal/runbook` — Markdown parse + check catalog (`opsgraph:check=`).
- `internal/ai` — optional Ollama + chromem-go RAG; stubbed in tests.
- `internal/{score,explain,report,graphviz,pathfind,impact,fingerprint}` — enterprise helpers.
- `fixtures/` — embedded `incident_checkout` pack for `demo`; disk-only `fleet_healthy` for strict healthy-path contracts.

## Data flow

1. Resolve source (same rules for `ask` / `status` / fleet helpers):
   - `--fixture` → ephemeral fixture pack
   - explicit `--data-dir` → persistent store only
   - else if config has a *usable* kubernetes snapshot and/or Prometheus/Alertmanager URL → live scrape (ephemeral)
   - else populated `state.db` under `data_dir` (resolved relative to the config file when set in YAML)
   - else live config seed (git + services) when `.opsgraph.yaml` exists
   - Git alone, seed-only / empty snapshot scrapes, or quiet Prom/AM (reachable, zero alerts) do **not** prefer live over a richer populated store.
   - Prom/AM scrape failures hard-fail; `ask`/`status` fall back to populated `state.db` with a stderr warning. `--data-dir` always forces the store.
2. Upsert entities into SQLite.
3. `ask` assembles owner, changes, alerts, 1-hop blast, runbook verify, timeline, recommendations, evidence (keeps live alerts even with skewed StartsAt; drops future resolved/historical rows; suppressed AM alerts are visible but not “active”).
4. Render table or JSON via `internal/output` (`SetEscapeHTML(false)`, indent, trailing newline).

## Cross-platform

`CGO_ENABLED=0`, `filepath` for OS paths, `fs.FS` + forward slashes for fixtures, LF via `.gitattributes`. CI covers ubuntu-24.04/macOS/windows, native linux/arm64 smoke, race+coverage floor on ubuntu, plus linux/darwin/windows × amd64/arm64 cross-builds.
