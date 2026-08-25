# Air-gap application and Blueprint sources

**Status:** implementation candidate for team review, validated in the CPU-only
air-gap lab.

## Basic contract

For basic air-gap support, an administrator must be able to point AIF at
private sources for:

- Blueprint definitions;
- application charts;
- workload GitOps output; and
- container images.

AIF must accept private authentication and CA trust for those sources and must
not require a Blueprint to contain an environment-specific URL or repository
name. Container-image mirroring and disabled registry fallback remain node
configuration prerequisites.

## Design

A Blueprint identifies a logical application and version:

```yaml
components:
  - applicationRef:
      name: suse.ollama
      version: 1.55.0
```

An administrator maps that identity to a package and a Rancher source:

```yaml
apiVersion: ai-factory.suse.com/v1alpha1
kind: Application
metadata:
  name: suse.ollama
spec:
  chart:
    name: ollama
    sourceRef: application-collection
  credentialProfile: suse
```

`sourceRef` names a Rancher `ClusterRepo`. Rancher remains responsible for the
chart endpoint, transport, authentication, and CA. Moving the package from an
upstream source to Harbor changes the `Application` or `ClusterRepo`; the
Blueprint does not change.

One configured Fleet Git repository has two reserved paths:

- `blueprints/` is administrator input. Fleet applies Blueprint and Application
  resources stored there.
- `workloads/` is AIF output for the GitOps deployment strategy.

For private-CA HTTPS Git, `Settings.spec.fleet.caBundleSecretRef` supplies one
PEM bundle to both AIF's go-git client and the generated Fleet `GitRepo`.
Token and basic authentication use the same username/password interpretation
in both consumers. There is no insecure TLS fallback.

## Compatibility and behavior

- `Application`, `Blueprint`, and `ClusterRepo` are cluster-scoped.
- A Blueprint component uses either `applicationRef` or the legacy direct
  chart fields. Mixed and incomplete forms are rejected by CRD validation.
- Resolution happens on every AIWorkload reconcile and is never written back
  into the Blueprint.
- Changes to a Blueprint, Application, or ClusterRepo requeue Blueprint
  workloads.
- A missing Application or ClusterRepo is reported as a recoverable workload
  condition.
- Existing direct-chart Blueprints remain supported.
- The UI resolves logical references for its existing forms, then removes the
  derived chart coordinates before writing a Blueprint.

## Empirical qualification

The air-gap lab builds the exact source commit, mirrors its charts and images
to authenticated private-CA Harbor, closes public host and pod egress, and
installs AIF only from the mirror. Its acceptance matrix covers:

- authenticated private-CA HTTPS Gitea for AIF writes and Fleet reads;
- Blueprint delivery from `blueprints/` in private Gitea;
- direct and logical Blueprint compatibility;
- FleetBundle and GitOps on local and downstream clusters;
- changing only an Application `sourceRef` between two private ClusterRepos;
- an unchanged stored Blueprint before and after that source change; and
- upstream-named workload images pulled through fail-closed RKE2 Harbor
  rewrites.

This is a CPU-only control-plane qualification. GPU applications, model
artifacts, and vendor-specific runtime network contracts require separate
profiles and hardware.

## Helm upgrade boundary

The chart installs the new Application CRD but does not bundle Application
custom resources in the same release. Helm resolves release objects before a
pre-upgrade CRD hook runs, so adding both a new kind and objects of that kind in
one upgrade fails discovery. Defaults can be added in a later release or
managed through the private Git repository after the CRD exists.

## Team decisions before merge

1. Confirm the long-term logical ID convention, such as
   `publisher.application`.
2. Decide whether changing an Application mapping should immediately reconcile
   every existing workload or require an explicit rollout action.
3. Confirm whether `credentialProfile` belongs on Application and whether the
   current `suse`/`nvidia` enum should become a generic runtime-credential
   reference.
4. Decide how product-provided Application defaults are introduced after the
   CRD ships.
5. Keep SSH Git support as a separate contract: it requires private-key and
   known-hosts fields and must never disable host-key verification. The current
   qualified baseline is HTTPS token/basic authentication.
