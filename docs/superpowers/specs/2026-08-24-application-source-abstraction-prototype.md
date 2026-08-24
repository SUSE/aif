# AG-001: Application source abstraction prototype

**Status:** Prototype for team review; not yet a final API decision.

## Basic air-gap contract

AIF is configurable for an air-gapped environment when every chart and Git
source it contacts can resolve through an internal Rancher `ClusterRepo`, with
authentication and private CA trust configured on that source, and AIF does not
fall back to a public endpoint.

Populating private registries and configuring node-level container-image
mirrors remain deployment prerequisites. They are not part of this prototype.

## Decision under test

A Blueprint declares which logical application and version it requires. It
does not declare where the package is hosted or which credential behavior the
package needs.

```text
Blueprint component
  applicationRef: suse.ollama@1.55.0
        |
        v
Application/suse.ollama
  chart: ollama
  sourceRef: application-collection
  credentialProfile: suse
        |
        v
ClusterRepo/application-collection
  OCI, HTTP, or Git endpoint + auth + CA
```

The `Application` is the only new abstraction. Rancher `ClusterRepo` remains
the source and transport implementation.

## Prototype behavior

- `Application` is cluster-scoped because both `Blueprint` and `ClusterRepo`
  are cluster-scoped.
- Application IDs are qualified Kubernetes names such as `suse.ollama`; the
  exact long-term naming policy remains a review point.
- A Blueprint component uses either `applicationRef` or the legacy direct
  `chartRepo`/`chartName`/`chartVersion` fields. Admission rejects mixed or
  incomplete forms.
- The operator resolves Application-backed components on every reconcile and
  does not write resolved coordinates back into the Blueprint.
- Blueprint, Application, and ClusterRepo changes requeue Blueprint workloads.
- Missing Applications produce a recoverable `Ready=False` condition instead
  of a controller error loop.
- The UI resolves Application-backed components for its existing forms, then
  strips derived chart coordinates before writes.
- Existing custom and persisted direct-chart Blueprints remain compatible.

## Demonstrated prototype

`operator/samples/application-source-abstraction.yaml` contains one logical
Application and a Blueprint that references it. Bundled Blueprints remain on
the legacy representation in this prototype, so an upgrade introduces the API
without changing an existing product definition.

The controller test changes the backing `ClusterRepo` from a public OCI URL to
an internal Harbor URL, reconciles again, and verifies that the HelmOp changes
while the stored Blueprint remains untouched.

## Helm upgrade boundary

The prototype deliberately does not render `Application` custom resources as
Helm release objects. A live upgrade test from the current four-CRD chart showed
that Helm resolves the target manifest before running the pre-upgrade CRD hook;
it therefore rejects a new `Application` object before the hook can install the
new CRD. The chart safely introduces the CRD, and the sample can be applied
after that upgrade.

Bundled Application provisioning must be a separate product decision: stage it
in a later release, bootstrap defaults outside the Helm release, or split CRD
delivery from custom resources. A two-step user upgrade is not acceptable.

## Questions for team review

1. Is `publisher.application` the right stable ID convention?
2. Should changing an Application's chart name be allowed for already-deployed
   workloads, or should only `sourceRef` and `credentialProfile` be mutable?
3. How should bundled Applications be provisioned after the CRD has shipped,
   without violating Helm's new-kind upgrade ordering?
4. After the API is accepted, should the custom Blueprint builder create and
   select Applications, or should Application management remain GitOps/API-only
   initially?

Artifact manifests, transfer tooling, runtime model downloads, readiness
preflight, and a generic Settings source editor are deliberately deferred until
this resolution boundary is accepted.
