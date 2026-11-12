# AIF Lab: Token Factory (PoC)

Mock lab chart. Serves the `aif-lab-token-factory` UI extension (same shape as `aif-ui`) and sketches
an optional backend (`mockApi.enabled`, default false) plus an example lab-owned CRD
(`ModelRoute`, group `tokenfactory.labs.ai-factory.suse.com`) in the separate `aif-lab-token-factory-crds` chart (installed by a cluster admin; aif-operator cannot create CRDs).
All UI data is mocked in the browser. Not for production.
