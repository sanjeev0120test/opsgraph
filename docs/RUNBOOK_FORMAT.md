# Runbook format

Runbooks are Markdown with optional YAML front matter and HTML-comment check
annotations. `opsgraph` parses numbered steps and evaluates each check against
the current store.

## Front matter

```yaml
---
service: checkout
owner: payments
aliases: [checkout-api]
---
```

## Checks

Annotate steps with `opsgraph:check=…`. A check
binds to the nearest preceding numbered step. Annotations always win.

Unmarked steps are inferred from wording (so existing Markdown runbooks still
verify something useful):

- deploy / rollout / release → `deploy_age_lt:60m`
- unhealthy / not healthy / still down → `service_unhealthy:<service>`
- healthy / healthz / recover → `service_healthy:<service>`
- otherwise → `manual`

```markdown
1. Confirm a recent deploy.
<!-- opsgraph:check=deploy_age_lt:60m -->

2. Service must be healthy.
<!-- opsgraph:check=service_healthy:checkout -->

3. Human follow-up.
<!-- opsgraph:check=manual -->
```

### Catalog

| Check | Pass when |
|-------|-----------|
| `deploy_age_lt:Xm` / `deploy_age_gt:Xm` | Newest **deploy/rollout** at/before now vs window (commits + future timestamps ignored) |
| `k8s_deployment_exists:name` | Rollout evidence for that deployment (`raw_ref`, `ev-k8s-rollout-<name>`, or namespaced `ev-k8s-rollout-<ns>-<name>`) |
| `service_healthy:name` / `service_unhealthy:name` | Health matches |
| `alert_firing:name` | Active alert with that exact name **on this runbook's service** (`firing`/`pending`) |
| `manual` | Always manual (never fails the step) |

### Roll-up

- Step: failing deploy/health/k8s/alert checks → `stale`; unknown/invalid checks → `error`; `manual` → `manual`.
- Runbook: any `fail`/`error` → `fail`; else any `stale` → `stale`; else any automated `pass` → `pass`; else only manuals (or empty) → `manual`; missing runbook → `missing`.
- CLI: `verify-runbook` exits `0` for `pass` and `manual`, `1` for `stale`/`fail`/`missing`.
