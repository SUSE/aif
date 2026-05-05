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

Expected: the Bundle reaches `Phase: Draft` with `conditions[0].type: Ready`,
`conditions[0].status: "True"`. If validation fails, check the operator log
and the bundle's `status.conditions` for the failure reason.
