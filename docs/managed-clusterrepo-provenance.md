# Managed ClusterRepo Provenance Label

## Upgrade Ordering

The operator must ship before the UI when rolling out managed ClusterRepo discovery changes.

The label-reading UI shows an empty managed repository set until ClusterRepos carry the `ai-factory.suse.com/managed-repo: "true"` label. When the operator is upgraded, the controller restarts, re-reconciles all Settings resources, and re-applies existing operator-created repositories with the provenance label near-immediately. This ensures that the labeled repositories become visible to the UI shortly after the operator upgrade completes.

**Recommended upgrade sequence:**
1. Upgrade the `aif-operator` chart first
2. Wait for the operator to reconcile and apply labels to managed ClusterRepos
3. Upgrade the `aif-ui` chart

## Manual Cleanup

Pre-existing UI-created air-gap mirrors have slug-based names and no provenance label. After upgrading to the new operator version, it creates a labeled canonical `nvidia` ClusterRepo at the same URL, leaving the old slug-based repository orphaned as a cosmetic duplicate.

The operator intentionally does not auto-delete "any repo at this URL we did not create" because doing so could delete a legitimately admin-created repository. Operators should delete the old slug-based ClusterRepo manually after confirming the labeled `nvidia` repository is Ready:

```bash
kubectl get clusterrepos -l '!ai-factory.suse.com/managed-repo' -o name   # candidates
kubectl delete clusterrepo <old-slug-name>
```

The first command lists ClusterRepos without the managed-repo label (candidates for cleanup). Review the list to identify UI-created air-gap mirrors, then use the second command to delete the specific old slug-based repository once the new labeled repository is confirmed Ready.
