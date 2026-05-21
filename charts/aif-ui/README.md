# AIF UI Helm Chart

Registers the SUSE AI Factory UI plugin with Rancher via a `UIPlugin` custom resource in `cattle-ui-plugin-system`.

## Prerequisites

- Rancher with UI Extensions support (`catalog.cattle.io/v1` API)
- AIF operator deployed and serving the UI endpoint

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `endpoint` | string | `http://aif-operator.aif.svc.cluster.local:8080/ui` | AIF operator UI endpoint URL |
| `plugin.name` | string | `ai-factory` | UIPlugin name registered in Rancher |
| `plugin.version` | string | `1.0.0` | Plugin version |
| `plugin.noCache` | boolean | `false` | Disable browser caching of plugin assets |
