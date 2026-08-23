# Incident walkthrough

This is the built-in `incident_checkout` scenario, exactly as `opsgraph demo`
runs it.

## What happened

Around `2026-07-31T11:38:00Z`, checkout was redeployed. Shortly after, error
rate spiked. Upstream `auth` is unhealthy; downstream `order` depends on
checkout.

## Reproduce

```bash
opsgraph prove
opsgraph demo
```

You should see: degraded checkout, firing `CheckoutErrorRateHigh`, recent
deploy, unhealthy auth upstream, impacted order downstream, and a runbook that
is `stale` (step 2 expects healthy checkout).

## Dig deeper

```bash
opsgraph demo --format json | jq '.evidence'
opsgraph blast checkout --fixture fixtures/incident_checkout
opsgraph impact auth --fixture fixtures/incident_checkout
opsgraph why checkout --fixture fixtures/incident_checkout --format json
opsgraph explain checkout --fixture fixtures/incident_checkout
opsgraph handoff checkout --fixture fixtures/incident_checkout
opsgraph status --fixture fixtures/incident_checkout   # shows ACTIVE SOURCE
opsgraph demo --ai
```
