# Examples

Minimal sample CRs to verify the AIF operator reconciles cleanly in a local
dev cluster. None of these will actually deploy anything (the chart references
are placeholders).

## Quick start

```bash
make dev-cluster        # k3d cluster create aif-dev
make dev-install        # kubectl apply -f charts/aif-operator/crds/
make run &              # start operator out-of-cluster
kubectl apply -f examples/bundle-smoke.yaml
kubectl get bundles -A
kubectl describe bundle -n default smoke
```

Expected after a few seconds (verified against the running operator on
2026-05-06):

- `kubectl get bundle -n default smoke -o jsonpath='{.status.conditions}'`
  → one condition with `type: Ready, status: "True", reason: Reconciled`.
  The Bundle has no `status.phase` field — the reconciler only sets
  conditions in this scaffold (P1-1).
- `kubectl get blueprint smoke-blueprint.0.1.0 -o jsonpath='{.status}'`
  → `phase: Active`, condition `Ready=True, reason: BlueprintValidated`.
- `kubectl get workload -n default smoke -o jsonpath='{.status}'`
  → `phase: Pending`, condition `Ready=False, reason: AwaitingDeployer`.
  Deploy logic lands in Phases 4–5 (see PROJECT_PLAN.md).

If validation fails, check the operator log and the resource's
`status.conditions` for the failure reason.

> **Note on the Blueprint immutability webhook.** The webhook server is
> registered inside the operator process, but `make run` does NOT install
> the corresponding `ValidatingWebhookConfiguration` in the cluster — that
> resource ships in `charts/aif-operator/templates/webhook.yaml` and is
> created only when you `helm install` the operator chart. So
> `kubectl patch blueprint … --type=merge -p '{"spec":{...}}'` will succeed
> in the dev loop. To exercise immutability end-to-end, install the chart
> instead of running out-of-cluster.
