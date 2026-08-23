#!/usr/bin/env bash
# Optional Phase-2 demo: stand up a tiny kind cluster and feed native kubectl
# YAML into opsgraph (no custom dialect, no service catalog). NOT required for
# CI or everyday use.
# Prerequisites: docker, kind, kubectl. Free and local only.
set -euo pipefail

CLUSTER="${OPSGRAPH_KIND_CLUSTER:-opsgraph-demo}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SNAP="$(mktemp -d "${TMPDIR:-/tmp}/opsgraph-snap.XXXXXX")"
trap 'rm -rf "$SNAP"' EXIT

echo "==> create kind cluster (idempotent)"
if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
  kind create cluster --name "$CLUSTER"
fi
kubectl config use-context "kind-$CLUSTER" >/dev/null

echo "==> apply demo workload"
kubectl apply -f - <<'YAML'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: checkout
  labels:
    app: checkout
spec:
  replicas: 1
  selector:
    matchLabels:
      app: checkout
  template:
    metadata:
      labels:
        app: checkout
    spec:
      containers:
        - name: checkout
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
YAML
kubectl rollout status deploy/checkout --timeout=90s

echo "==> export native kubectl YAML (no opsgraph dialect, no client-go)"
kubectl get deploy -o yaml >"$SNAP/deployments.yaml"
kubectl get events -o yaml >"$SNAP/events.yaml"

CFG="$SNAP/opsgraph.yaml"
echo "==> opsgraph init (no service catalog)"
(cd "$ROOT" && go run ./cmd/opsgraph init --out "$CFG" --k8s "$SNAP" --force)

echo "==> opsgraph ask (auto-select hottest service from snapshot)"
(cd "$ROOT" && go run ./cmd/opsgraph ask --config "$CFG" --since 60m)

echo "OK - kind demo finished (cluster left running: kind delete cluster --name $CLUSTER)"
