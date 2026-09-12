# Free stack

Everything in `opsgraph` is free and open-source. There are no accounts, API
keys, paid SaaS connectors, or required cloud services.

| Layer | Choice | Notes |
|-------|--------|-------|
| Language | Go 1.26+ | Static binary, `CGO_ENABLED=0` |
| CLI | cobra | Pure Go |
| Store | modernc.org/sqlite | Pure Go SQLite |
| Git | go-git | Local repo only |
| K8s | YAML snapshot parser | No `client-go` / `k8s.io` in v1 |
| AI (optional) | Ollama + langchaingo + chromem-go | Local only; inert unless `--ai` |
| CI | GitHub Actions | ubuntu-24.04/macOS/windows matrix, native linux/arm64 smoke, race+coverage floor, GOPROXY=off rebuild, binary size budget, deadcode/govulncheck, 6-target cross + install smoke |

Explicitly out of scope for v1: paid observability SaaS, MCP servers, web UI,
live Kubernetes client (documented future opt-in).
