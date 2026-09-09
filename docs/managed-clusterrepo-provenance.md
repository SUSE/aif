# Managed ClusterRepo Provenance Label

## Upgrade Ordering

The operator must ship before the UI when rolling out managed ClusterRepo discovery changes.

The label-reading UI shows an empty managed repository set until ClusterRepos carry the `ai-factory.suse.com/managed-repo: "true"` label. When the operator is upgraded, the controller restarts, re-reconciles all Settings resources, and re-applies existing operator-created repositories with the provenance label near-immediately. This ensures that the labeled repositories become visible to the UI shortly after the operator upgrade completes.

**Recommended upgrade sequence:**
1. Upgrade the `aif-operator` chart first
2. Wait for the operator to reconcile and apply labels to managed ClusterRepos
3. Upgrade the `aif-ui` chart

## Manual Cleanup

Pre-existing UI-created air-gap mirrors carry no provenance label. Their name is whatever the old UI supplied — a canonical name (e.g. `nvidia`) when one was passed, otherwise a URL-derived slug. When that old name differs from the canonical name the operator now uses, upgrading creates a labeled canonical `nvidia` ClusterRepo at the same URL and leaves the old unlabeled repository orphaned as a cosmetic duplicate. (If the old name already matched the canonical name, the operator re-applies the label in place and there is nothing to clean up.)

The operator intentionally does not auto-delete "any repo at this URL we did not create" because doing so could delete a legitimately admin-created repository. Operators should delete the old unlabeled ClusterRepo manually after confirming the labeled `nvidia` repository is Ready:

```bash
# List every unlabeled ClusterRepo WITH its URL, so you can match by URL — do not
# delete by the label selector alone.
kubectl get clusterrepos -o custom-columns=NAME:.metadata.name,URL:.spec.url,GITREPO:.spec.gitRepo
kubectl delete clusterrepo <old-slug-name>
```

Match the old mirror by its **URL** — it will equal your `registryEndpoints.nvidia` value (the air-gap mirror URL) — not by name. Delete only that repository, and only once the new labeled `nvidia` repository is confirmed Ready.

> **Warning — do not delete built-in or extension repositories.** The inverse label
> selector (`-l '!ai-factory.suse.com/managed-repo'`) also matches Rancher's own
> `rancher-charts`, `rancher-partner-charts`, `rancher-rke2-charts`, and every
> UI-extension ClusterRepo — none of which carry the managed-repo label. Never
> delete a `rancher-*` or extension repository; deleting them breaks the cluster's
> chart catalog. Always confirm the URL before deleting.

Two related caveats:

- **AIWorkloads pin their source repo by name** (`spec.source.app.chartRepo`). Deleting a slug-based repository that an existing workload was installed from will break redeploy of that workload until it is repointed at the labeled canonical repository.
- The removed UI code also created a `cattle-system/<slug>-auth` Secret alongside each mirror. After the operator takes over, that Secret is orphaned; remove it manually if you no longer need it.
