# AIWorkload namespace migration (Phase 2)

Relocates existing `AIWorkload` CRs from their per-target namespaces into the single
control-cluster workload namespace (default `aif-workloads`).

This is **opt-in** and safe to run any time after upgrading to an operator build that
stores new workloads in the workload namespace. It does **not** redeploy or interrupt
running workloads: the downstream deployment identity does not depend on the CR's
namespace, so a relocated CR re-adopts its existing Fleet Bundle / Helm release.

## What it does, per workload

1. Recreates the CR in the workload namespace (same name, spec, and status), annotated
   `ai-factory.suse.com/migrated-from: <old-namespace>/<name>`.
2. Strips the `ai-factory.suse.com/cleanup` finalizer from the old CR **before** deleting
   it, so deletion does not tear down the live downstream workload.
3. Sweeps orphaned pull-secret Bundles still labelled with the old namespace.

It is idempotent and restartable. CRs already in the workload namespace are skipped. A
name collision (two different workloads that would share a name in the destination) is
**reported and skipped, never auto-renamed** — renaming a workload changes its downstream
identity. Resolve collisions manually, then re-run.

## Dry run first

Always preview before applying: run the Job below (or the CLI) with `-dry-run` and inspect
the logs. Dry run lists every planned relocation and collision without mutating anything.

## Running it as a Job

The tool reuses the operator's ServiceAccount, which already holds the required
cluster-wide permissions (list/create/update/delete AIWorkloads, delete Fleet Bundles,
patch namespaces). Replace the image and, if you overrode them at install time, the
ServiceAccount name and `WORKLOAD_NAMESPACE`.

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: aif-aiworkload-migration
  namespace: aif-operator          # the operator release namespace
spec:
  backoffLimit: 1
  template:
    spec:
      restartPolicy: Never
      serviceAccountName: aif-operator   # the operator ServiceAccount
      securityContext:
        runAsNonRoot: true
      containers:
        - name: migrate
          image: ghcr.io/suse/aif-operator:2.0.0
          command: ["/migrate"]
          # Add "-dry-run" to preview without making changes.
          args: []
          env:
            - name: WORKLOAD_NAMESPACE
              value: aif-workloads
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
```

Apply, then check the report:

```sh
kubectl apply -f aiworkload-namespace-migration.yaml
kubectl -n aif-operator logs job/aif-aiworkload-migration
```

The Job exits non-zero if any workload was skipped due to a name collision, so a failed
Job means "manual action required" — read the logs, resolve, and re-run.

## Running from a workstation

With kubeconfig pointing at the control cluster:

```sh
cd operator
make build-migrate
./bin/migrate -dry-run        # preview
./bin/migrate                 # apply
```
