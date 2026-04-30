# Designating Blueprint Publishers

> **Audience:** Cluster administrators installing or operating SUSE AI Factory.
> **Scope:** How to grant the Blueprint Publisher role to one or more users or groups so that Bundle approvals can proceed.

---

## What is the Blueprint Publisher role?

A **Blueprint Publisher** is a user (or group of users) who can approve or reject Bundles that AI/ML Practitioners have submitted for review. Approving a Bundle mints a new immutable AIF Blueprint version. Publishers can also Deprecate, Withdraw, and Reactivate published Blueprint versions.

See `docs/spec/SOFTWARE_SPEC.md` §2 (Personas) for the role's responsibilities, and `docs/spec/ARCHITECTURE.md` §8.5 for how the role is enforced under the hood.

The role is **not bound to anyone by default**. Until a cluster admin binds it, the Pending Reviews queue accumulates submissions that no one can act on. The AIF UI surfaces a banner on the Bundles and Overview pages whenever this is the case.

---

## Who should be a Publisher?

Typical guidance:

- **Platform Engineers** are the natural fit. They already own cluster operations and understand what should be promoted to a published Blueprint.
- **Pick a small group** rather than individual users. Group-based bindings scale and survive personnel changes.
- **Use OIDC / Rancher group claims** if your cluster authenticates via OIDC. Map an existing group (e.g., `platform-engineering` or a dedicated `ai-blueprint-publishers`) rather than creating a new identity layer.
- **Avoid binding to `system:authenticated`** — that grants publish rights to every authenticated user, which defeats the approval workflow.

---

## Binding the role

Three equivalent paths produce the same result. Pick whichever matches your cluster's RBAC management style.

### Recipe 1: Bind to an individual user via `kubectl`

For ad-hoc designation of a specific user:

```bash
kubectl create clusterrolebinding aif-publishers-alice \
  --clusterrole=aif-blueprint-publisher \
  --user=alice@example.com
```

The `--user` value must match the username your cluster's authenticator surfaces (e.g., the OIDC `sub` claim, or the Rancher username). To verify what the cluster sees a user as, run:

```bash
kubectl auth whoami --as=alice@example.com
```

### Recipe 2: Bind to a group via `kubectl` (recommended)

For OIDC-backed clusters with group claims:

```bash
kubectl create clusterrolebinding aif-publishers-platform-eng \
  --clusterrole=aif-blueprint-publisher \
  --group=platform-engineering
```

The `--group` value must match the group name as it appears in your IdP's group claim (case-sensitive). For Keycloak, this is typically the `groups` claim; for Okta or Azure AD, check your OIDC group mapping configuration in Rancher.

For multiple groups in one binding, use a YAML manifest:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: aif-publishers
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: aif-blueprint-publisher
subjects:
  - kind: Group
    name: platform-engineering
    apiGroup: rbac.authorization.k8s.io
  - kind: Group
    name: ai-platform-leads
    apiGroup: rbac.authorization.k8s.io
```

Apply with `kubectl apply -f publishers-binding.yaml`.

### Recipe 3: Bind via the Rancher Cluster RBAC UI

1. Open Rancher and navigate to **Cluster Management** → select the cluster running AIF → **Members** tab (or **Cluster Members** depending on your Rancher version).
2. Click **Add Member**.
3. In the **Member** field, search for the user or group (group support depends on your Rancher auth provider configuration).
4. Under **Cluster Permissions**, choose **Custom** and click **Add Custom Role**.
5. From the role list, select **`aif-blueprint-publisher`** (it appears because the AIF operator chart created it as a ClusterRole).
6. Click **Create**. Rancher creates the underlying ClusterRoleBinding.

Verify the binding in the resulting Cluster Members list — the user/group should show `aif-blueprint-publisher` in their assigned roles.

---

## Verifying the binding

Confirm the user has the right via Kubernetes' built-in access check:

```bash
kubectl auth can-i update bundles/approve --as=alice@example.com
# Expected output: yes
```

For a group:

```bash
kubectl auth can-i update bundles/approve --as=alice@example.com --as-group=platform-engineering
# Expected output: yes
```

If you see `no`, double-check:
- The username/group name matches exactly what the cluster's authenticator emits (case-sensitive).
- The ClusterRoleBinding's `roleRef.name` is exactly `aif-blueprint-publisher` (no typos).
- The ClusterRole `aif-blueprint-publisher` actually exists: `kubectl get clusterrole aif-blueprint-publisher`. If missing, the AIF operator chart hasn't been installed correctly.

---

## Confirming inside AIF

After binding the role:

1. Open the AIF Bundles page in the Rancher Dashboard.
2. The **"No Blueprint Publishers configured"** banner at the top should disappear within a few seconds (cached up to 30s).
3. If a designated publisher logs in, the **Approve & Publish** and **Request Changes** buttons on Submitted Bundles should now be enabled (no greyed-out state, no tooltip).

If the banner persists for longer than a minute after binding, refresh the page or run:

```bash
kubectl get clusterrolebindings -o json | \
  jq '.items[] | select(.roleRef.name == "aif-blueprint-publisher") | {name: .metadata.name, subjects: .subjects}'
```

to confirm the binding is in place.

---

## OIDC group example

When Rancher is configured with OIDC, group membership flows through the Rancher proxy as the `Impersonate-Group` header. AIF's `SubjectAccessReview` honours this naturally — no AIF-side configuration is required.

Step-by-step example with a Keycloak group called `ai-platform-publishers`:

1. In Keycloak, ensure the `groups` claim is included in the ID token for the OIDC client Rancher uses.
2. In Rancher, go to **Users & Authentication** → **Auth Provider** (Keycloak) → confirm group claim mapping is enabled.
3. Add the `ai-platform-publishers` group to relevant Keycloak users.
4. Run:

```bash
kubectl create clusterrolebinding aif-publishers \
  --clusterrole=aif-blueprint-publisher \
  --group=ai-platform-publishers
```

5. A user logged in to Rancher as a member of `ai-platform-publishers` will now see Approve / Request Changes enabled in AIF.

---

## Removing publisher access

To revoke a single user from a binding:

```bash
kubectl edit clusterrolebinding aif-publishers
# Remove the entry from .subjects[]
```

To revoke an entire binding:

```bash
kubectl delete clusterrolebinding aif-publishers
```

Removing the last bound subject restores the no-publishers banner in AIF within ~30s (the auth endpoint caches results per caller).

---

## Troubleshooting

| Symptom | Likely cause | Fix |
|---|---|---|
| Banner persists after binding | Cached auth response (30s TTL) | Wait up to 30s, then refresh |
| `kubectl auth can-i` returns `no` despite a binding | Username case mismatch | Confirm the exact name your authenticator emits via `kubectl auth whoami --as=<user>` |
| Group binding doesn't take effect | OIDC group claim not surfacing | Check Rancher's auth provider config; confirm the OIDC token includes the `groups` claim |
| Approve button is enabled but the API call returns `403 FORBIDDEN` | The user lost the role between page-load and click | Refresh the page; the disabled state will reappear if the role was revoked |
| `aif-blueprint-publisher` ClusterRole doesn't exist | Operator chart not installed or partially installed | `helm list -n aif`; reinstall the `aif-operator` chart if missing |
| Multiple bindings exist with overlapping subjects | Redundant — AIF treats them as a union | Optional cleanup: consolidate into a single binding for clarity |

---

## See also

- `docs/spec/SOFTWARE_SPEC.md` §2 — Blueprint Publisher persona
- `docs/spec/SOFTWARE_SPEC.md` §11 — Role-Based UI States (banner + button gating)
- `docs/spec/ARCHITECTURE.md` §8.5 — Publisher Role and SAR enforcement
- `docs/spec/ARCHITECTURE.md` §5 (Auth) — `GET /api/v1/auth/publishers` endpoint
