# PR #387: live disconnected upgrade and token retirement

This records the November 18, 2026 qualification of
[PR #387](https://github.com/SUSE/aif/pull/387) in the `suseai-882-airgap` lab.
For architecture, trust requirements, permissions, and migration semantics, see
[Operation without a personal Rancher API token](../tokenless-operator.md).

## Installation and provenance

| Item | Qualified configuration |
| --- | --- |
| Starting installation | Published AIF 2.4.0, combined operator/UI installation |
| Rancher | 2.15.3 |
| Kubernetes | RKE2 v1.34.4+rke2r1; one management and one downstream cluster, CPU only |
| Private services | Harbor and private HTTPS Gitea, with the lab's private CA |
| Registry trust | Native AIF Settings CA configuration |
| Network boundary | Host and pod egress restrictions remained enabled; default registry fallback disabled |
| Final test version | `2.5.0-dev.3873` for the operator, chart, and UI |
| Prerequisite PR #383 | `5d61809ba1e030a382e3f3f7bad7bfc41b0ddbc2` |
| PR #387 implementation | `461faeb0` (includes the private Git CA fix) |
| Exact artifact source | `9906e8d092318037e8ca3b36ee9ce8695fa44413` |
| Operator/UI image tag | `2.5.0-dev.3873-9906e8d09231` |

The artifact source is a local QA commit containing the implementation plus
version metadata changes. Those packaging commits are not part of the PR.
The unique version forces Rancher to load the matching UI bundle. Artifacts were
built from `git archive`, exported to a checksummed bundle, imported into private
Harbor, and installed using Helm **3.18.6** on the management node. The lab clusters
were not reconnected to the Internet for the upgrade.

The full lifecycle and negative-test matrix ran on `2.5.0-dev.3872`
(`ff556edd32b175283d71f0dc9acf0ebc26cd9caf`), before the latest PR #383 commit
arrived. That commit changes registry-status error reporting and idle polling.
The branch was then rebased, rebuilt, and upgraded to `2.5.0-dev.3873`.
The final build received current-state, authorization, browser, local cleanup,
and fresh private Git deployment/Fleet cleanup checks. This distinction matters:
the earlier scenario evidence is not presented
as a complete rerun of the matrix on a different artifact.

## What the live tests established

| Scenario | Observed result | Local evidence file |
| --- | --- | --- |
| Upgrade from published 2.4.0 | Original Settings UID and active registry/Fleet settings preserved; existing AIWorkloads retained their UIDs and Running status | `continuity-proof.json`, `current-verification.json` |
| Existing applications and data | Original Qdrant and Ollama resource UIDs and PVC/PV bindings preserved on both clusters; a point written before upgrade remained readable; Ollama APIs healthy | `*-final-continuity.txt`, `current-verification.json` |
| Retire the old shared token | Token revoked in Rancher, old bearer returns 401, old Secret removed, AIF API continues to work | `token-retirement.json` |
| Authorization | Anonymous spoofed identity gets 401; user without AIF access gets 403; scoped viewer gets Settings with 200 but cannot write it (403) | `authorization-proof.json`, `authorization-current-proof.json` |
| Local Helm cleanup | Operator deletes release resources and history; respects `helm.sh/resource-policy: keep`; finishes finalizer without the personal token | `local-uninstall-proof.json` |
| Missing uninstall permissions | Hook Job permissions denied before deletion; release and ordinary resources remain intact; finalizer retained | `hook-permission-denied.json` |
| Recovery after restart | Namespace-scoped hook permission granted, operator restarted, hook succeeded and cleanup completed | `hook-restart-proof.json` |
| Direct downstream Helm | Headless deletion held finalizer; stale acknowledgment returned 409; caller-session uninstall followed by valid acknowledgment completed; same-named local release remained intact | `downstream-uninstall-proof.json` |
| Private Git directory chart | Fetched indexed commit through authenticated HTTPS/private CA and deployed to both clusters with no personal Rancher token | `git-deploy-proof.json` |
| Fresh deployment on the final build | New Git-backed workload reached Running on both clusters; Fleet cleanup then removed both copies | `fresh-git-proof.json` |
| Git credential rotation | Old repository token revoked, source Secret rotated, new commit indexed, changed chart content reached both clusters | `git-rotation-proof.json` |
| Git access and trust failures | Bad repository password and valid-but-untrusted CA each blocked a fresh deployment; restoring the correct configuration recovered it | `git-denial-proof.json` |
| Real application from indexed Git archive | Mirrored Qdrant 1.19.0 archive passed digest validation, deployed to both clusters, accepted vector writes and queries | `archive-qdrant-proof.json`, `*-archive-qdrant.txt` |
| Fleet cleanup | Deleting that Qdrant workload removed its StatefulSets and Services on both targets and completed the finalizer | `fleet-cleanup-proof.json` |
| Browser behavior | Overview, Settings, Apps, Blueprints, Workloads, and About loaded without page errors; no Authorize section or token creation; local Delete used the AIF API | `browser-proof.json`, `browser-uninstall-proof.json` |
| Idle outage detection on the final build | Open Overview, Blueprints, and Workloads pages displayed the unreachable banner when the operator stopped and cleared it automatically after recovery | `browser-outage-proof.json` |
| Disconnected operation | Fresh pods reached internal Harbor but could not reach public `1.1.1.1:443`; host probe also failed; isolation service and disabled registry fallback verified | `current-verification.json` |

The Settings comparison deliberately excludes the deprecated `rancherCatalog`
field, which Helm removed, and the custom repositories added for the test. It
does not claim that the entire Settings object is byte-for-byte unchanged.
The network checks validate this lab's configured boundary and sample public
egress; they are not an exhaustive network penetration test.

The credentials used to log into Rancher for these tests were short-lived user
sessions. Repository credentials remain necessary for private Git. Neither was
stored as a shared personal Rancher token for the operator.

## Defect found and fixed during qualification

The first private Git repository failed Rancher indexing with
`error setting certificate file`. AIF supplied base64-encoded PEM in `caBundle`,
but Rancher's Git indexer expects DER and wraps it in PEM itself. The result was
a malformed certificate file.

The Settings controller now decodes and validates a single PEM certificate from
the CA Secret, then supplies DER for Git repositories. HTTP/OCI repositories keep
their existing PEM behavior. Multiple certificates are rejected for Git rather
than silently selecting one. The regression test and the real private Git
directory/archive deployments cover the correction. Rancher's behavior is visible
in its [Git downloader](https://github.com/rancher/rancher/blob/v2.15.3/pkg/catalogv2/git/download.go)
and [DER conversion](https://github.com/rancher/rancher/blob/v2.15.3/pkg/catalogv2/git/utils.go).

## Repeat validation on this lab

Run these commands on the workstation used to prepare the lab. The helpers read
the existing protected credentials and kubeconfigs; they do not print passwords
or tokens. The generated directory contains sensitive backups and is intentionally
excluded from Git. It is local evidence, not part of the repository checkout.

```bash
export AIF_EVIDENCE="$HOME/suse/suse-ai-stack-airgap-882/airgap-lab/generated/pr387-20261118"
python3 "$AIF_EVIDENCE/connect.py"
python3 "$AIF_EVIDENCE/verify-current.py"
python3 "$AIF_EVIDENCE/verify-auth.py"
```

Expected: **All 17 current-state checks passed**, followed by viewer GET 200,
viewer PUT 403, and anonymous spoofed-header 401. The current-state verifier
creates and removes one egress-probe pod per cluster; otherwise it reads the
installation and the saved application-data marker. It assumes that the lab and
the retained demonstration workload have not been intentionally removed.

Inspect the retained private Git demonstration directly:

```bash
kubectl --kubeconfig "$AIF_EVIDENCE/management.kubeconfig" \
  -n aif-pr387-qa get aiworkload pr387-git-deployment

kubectl --kubeconfig "$AIF_EVIDENCE/management.kubeconfig" \
  -n aif-pr387-qa get configmap pr387-git-probe-evidence \
  -o jsonpath='{.data.marker}{"\n"}'

kubectl --kubeconfig "$AIF_EVIDENCE/downstream.kubeconfig" \
  -n aif-pr387-qa get configmap pr387-git-probe-evidence \
  -o jsonpath='{.data.marker}{"\n"}'

kubectl --kubeconfig "$AIF_EVIDENCE/management.kubeconfig" \
  -n aif-operator get secret aif-rancher-token
```

Expected: workload **Running**, both markers equal
`rotated-credential-new-indexed-commit`, and the retired token Secret is
**NotFound**. That last command intentionally returns a nonzero exit code.
The verifier additionally checks that the actual old Rancher token is revoked;
Secret absence alone would not establish that.

### Walk through the browser

1. Sign in to this lab's Rancher and hard-refresh the UI.
2. Open **AI Factory → About**. Operator, chart, and UI must all show
   `2.5.0-dev.3873`, with operator commit starting `9906e8d0`.
3. Open **Settings**. There is no **Rancher API Access / Authorize** section.
   Custom repositories `pr387-private-git` and `pr387-archive-git` use private
   HTTPS Git and the configured lab CA.
4. Open **Workloads**. The original Qdrant and Ollama workloads remain Running,
   as does `pr387-git-deployment`. Inspect its two target clusters.
5. Open **Blueprints**. The test entries **PR 387 private chart** and
   **PR 387 Qdrant from private Git** remain available for further demonstrations.

To demonstrate local deletion yourself, first prepare a disposable fixture:

```bash
python3 "$AIF_EVIDENCE/prepare-browser-delete.py"
```

In **Workloads**, delete **only `pr387-browser-delete`** and confirm. Then run:

```bash
python3 "$AIF_EVIDENCE/check-browser-delete.py"
```

Expected: AIWorkload, ordinary ConfigMap, and Helm history removed; the ConfigMap
marked with Helm's keep policy remains. No registry credentials or personal
Rancher token are needed. With browser DevTools Network open, the deletion is a
`DELETE /v1/aif-operator/api/v1/namespaces/aif-pr387-qa/aiworkloads/pr387-browser-delete`.
There is no local Steve App uninstall or `/v3/token` creation request.

For an API-driven repeat of the same local lifecycle:

```bash
python3 "$AIF_EVIDENCE/cleanup-probe.py"
```

For a fresh private Git deployment to both clusters, followed by Fleet cleanup:

```bash
python3 "$AIF_EVIDENCE/fresh-git-probe.py"
```

This uses a separate `aif-pr387-final` namespace and leaves the retained
`pr387-git-deployment` demonstration untouched.

The other scenario scripts in the evidence directory record the original
qualification, including credential rotation and permission changes. They are
not a single idempotent test suite: some deliberately revoke credentials or
modify repository trust. Review their fixtures before replaying them.

`browser-outage.cjs` repeats the three-page outage test using the workstation's
installed Playwright/Chromium. It briefly scales the operator to zero and restores
its original replica count in a `finally` block. Run it only when a short AIF API
interruption is acceptable. Existing deployed applications continue running.
Browser automation ignores the lab browser's certificate warning; Git trust
checks and Python API requests use verified CAs. The browser run is not evidence
of browser TLS trust configuration.

## Upgrade records, rollback, and retained fixtures

`upgrade-values-v3.yaml`, `upgrade-v3.log`, `artifacts-source-v3.yml`, and
`build-provenance.json` record the final deployment. The private upgrade values
preserve the lab's mirrors and CA configuration and explicitly enable aggregation.
The local `upgrade-current.py` helper repeats the recorded Helm 3 upgrade from
private Harbor. Running it creates a new Helm revision; it is not required for
the validation commands above.

The lab's original `vars.yml` and qualification markers still describe the
published **2.4.0** setup. Its general install phase also creates the old personal
token. Do not use that phase to reproduce this proposal deployment without
updating the installer. The PR-specific upgrade records are authoritative for
what is currently installed.

The old token was revoked, not merely removed from Settings. A rollback to 2.4.0
would require an appropriate replacement credential for the old code. Rollback
was not executed during this qualification.

Retained demonstration resources include the two private Git repositories and
Blueprints, `aif-pr387-qa/pr387-git-deployment`, a scoped viewer test account, Helm
keep-policy ConfigMaps, and the Qdrant continuity collection. The temporary Role
granting the operator hook Job permissions was removed after testing. Avoid
deleting the shared test namespace while inspecting the retained workload.
The original applications are in `aif-suse-apps` and were not used as cleanup
fixtures.

## Scope of the conclusion

The tested combined installation can upgrade, use private HTTPS Git, deploy
through Fleet, and clean up local Helm releases without depending on a personal
Rancher token. Failures in repository trust, credentials, and uninstall
permissions retain visible recovery paths. This is evidence for that deployment
profile, not a blanket production qualification.

Remaining release coverage includes SSH and mutual-TLS Git against real servers,
standalone installation mode, multi-node/HA variants, supported production charts'
custom resources and delete hooks, GPU/model inference, and an exercised rollback.
Direct downstream Helm still requires the caller's session and an acknowledgment;
Fleet remains the unattended downstream lifecycle path.

After the rebase, the full Go suite, **754 UI tests**, and TypeScript checks passed.
The implementation also has **161 passing Helm chart tests**, envtest cleanup
coverage, targeted race checks, and commit-hook validation. See the local logs
for the test runs; the live evidence and these automated checks cover different
failure modes.
