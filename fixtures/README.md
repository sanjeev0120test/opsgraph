# Fixtures

Each subdirectory is a self-contained incident pack that `opsgraph` can ingest
with no external systems. Packs are used by `opsgraph demo`, `opsgraph test`,
and `opsgraph ask --fixture <pack>`.

## Layout

- `services.yaml`, `owners.yaml`, `dependencies.yaml`
- `changes.yaml`, `alerts.yaml`
- `k8s/deployments.yaml`, `k8s/events.yaml` (optional; opsgraph dialect **or** native `kubectl get -o yaml`)
- `runbooks/*.md`
- `meta.yaml` (`now:` fixture clock)
- `expected/*.json` goldens for `opsgraph test`

Packs in this repo:

- `incident_checkout` — hot incident (degraded checkout, unhealthy auth); golden pack for `opsgraph test` / demo.
- `fleet_healthy` — all-green fleet for `health --strict` and healthy-path contracts (`expected/` goldens included).

Validate a pack: `opsgraph validate-fixture ./fixtures/incident_checkout`.
