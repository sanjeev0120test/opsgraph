# Contributing

## Validate changes

GitHub Actions is the source of truth. Prefer push → watch the `ci` workflow.

Optional local smoke (keeps the laptop light; no `-race`):

- Windows: `pwsh scripts/verify.ps1` (`go test -short` — skips the 21-loop prove burn)
- Unix: `bash scripts/verify.sh` or `make quick`

CI: ubuntu runs the full suite (`-race`, 21-loop prove, module-graph rebuilds).
Windows/macOS/arm64 use `-short` so the wall-clock stays on the ubuntu race job.

## Fixtures

- Hot incident pack: `fixtures/incident_checkout` (goldens under `expected/`)
- Healthy pack: `fixtures/fleet_healthy`

Update goldens only when behavior intentionally changes:

```bash
go run ./cmd/opsgraph test ./fixtures/incident_checkout --update
```

## Module / build invariants (CI-enforced)

- No `replace` / `exclude` in `go.mod`
- Default binary stays pure-Go (`CGO_ENABLED=0`, no `k8s.io/*` in the link graph)
- No first-party `unsafe` or deprecated `io/ioutil`
- Fixture goldens are LF-only + structurally complete (`.gitattributes`, `scripts/check_lf.go`, `scripts/check_fixtures.go`)
- After `go mod tidy`, builds use a locked module graph (`-mod=readonly -buildvcs=false`)
- Release ldflags keep an empty Go build ID (`-buildid=`)
- Lint runs `go vet` and compile-only `-vet=all`
- `AskResult` / `VerifyResult` / nested machine JSON field sets are frozen in `internal/model`
- CLI command surface + critical flags are frozen in `cmd/opsgraph`
- Runbook check catalog, core exit-code matrix, demo↔fixture JSON parity, and `OPSGRAPH_*` env exclusivity are frozen
- Direct deps are allowlisted; forbidden module prefixes (k8s/cloud/MCP) stay out of the link graph
- SQLite `schemaDDL` SHA-256 is coupled to `SchemaVersion`
- Short fuzz budgets cover fingerprint, runbook parse, and services YAML
- `govulncheck` runs in source mode (hard fail) and binary mode (explicit allowlist in `scripts/govulncheck-allowlist.txt`)
- Full smoke proves fixture/demo/doctor work with HTTP(S) proxy blackholed

## Do not commit

- Binaries / release archives (`bin/`, `dist/`, `*.tar.gz`, `*.zip`, `SHA256SUMS`)
- Local planning docs (`PLAN.md`, `AGENTS.md` — gitignored)
- Secrets or `.opsgraph/` runtime data
