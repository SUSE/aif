# SUSE AI Factory

SUSE AI Factory is a Rancher UI Extension for managing SUSE AI Factory components across Kubernetes clusters. This extension provides a unified interface for installing, managing, and monitoring AI workloads in Rancher-managed clusters.

> **Note:** This extension requires an active [SUSE AI Factory](https://www.suse.com/products/ai/) subscription to access the application catalog.

## Development

### Prerequisites

- Node.js 20+ and Yarn
- Access to a Rancher cluster
- Extension developer features enabled in Rancher

### Setup

1. **Clone and install dependencies:**
   ```bash
   git clone <repository-url>
   cd aif
   yarn install
   ```

2. **Build the extension:**
   ```bash
   yarn build-pkg aif-ui
   ```

3. **Serve during development:**
   ```bash
   yarn serve-pkgs
   ```
   Copy the URL shown in the terminal.

4. **Load in Rancher:**
   - In Rancher, go to your user profile (top right) → Preferences
   - Enable "Extension Developer Features"
   - Navigate to Extensions from the side nav
   - Click the 3 dots (top right) → Developer Load
   - Paste the URL from step 3, select "Persist"
   - Reload the page
  
### Debug Mode

Enable debug logging in development:

```bash
NODE_ENV=development yarn build-pkg aif-ui
```

## Building for Production

```bash
yarn build-pkg aif-ui --mode production
```

## Extension Catalog Container

- The container packages SUSE AI Factory (Rancher UI Extension) into a single OCI container image.
- This container is:
   - Built and published during CI
   - Stored in GitHub Container Registry (GHCR)
   - Consumed by Rancher as an extension catalog source
- The catalog container allows:
   - Versioned releases
   - Immutable distribution
   - Simple rollout via container tags

 ### Versioning
- The catalog container tag is derived from the Git tag:
 
```
aif-ui-<version> → ghcr.io/suse/aif-ui:<version>
```

In the examples below, `<version>` refers to a published extension release (e.g. `0.2.0`).

Available catalog image versions are published in GitHub Container Registry:
https://github.com/SUSE/aif/pkgs/container/aif-ui
 
### Container Structure
```
/home/plugin-server
└── plugin-contents/
    ├── files.txt
    ├── index.yaml
    └── plugin/
        ├── index.yaml
        ├── package.json
        ├── aif-ui
            └── aif-ui-<version>.tgz
        └── aif-ui-<version>
            ├── files.txt
            └── plugin/
                └── <plugin source code>
```

### Consuming the Catalog in Rancher
- Add the catalog source in the Rancher Dashboard:
   1. Navigate to Extensions → Manage Extensions Catalog
   2. Import Extension Catalog → Use the Catalog Image Reference: `ghcr.io/suse/aif-ui:<version>` → Press `Load`
   3. From the Extensions page, Go to Manage Repositories. Verify if the SUSE AI Rancher Extension repository has the `Active` state. If not, refresh the connection.
   4. Go back to Extensions and install SUSE AI Rancher Extension.
   5. Re-load Rancher Dashboard. Reload the browser to ensure the extension is loaded in the UI (ctrl+r or F5 or cmd+r).
   6. The "SUSE AI Factory" logo will now appear on the left panel of the Rancher Dashboard.

> NOTE: Replace `<version>` with a tag published in GitHub Container Registry.
> NOTE: Newly published catalogs are not always available immediately. If the catalog does not show up after publishing, navigate to Extensions → Manage Repositories and manually refresh the repository to force a re-sync.

## Extension GitHub Branch
- In addition to the OCI-based catalog container, the SUSE AI Factory extension can be distributed via the GitHub branch (`gh-pages`). This method hosts the extension artifacts as files within a specific branch of your repository, allowing Rancher to consume the extension directly from there.

**Overview**
- The extension is built into static assets (`index.yaml`, `.tgz`, etc.)
- These assets are published to the GitHub branch: `gh-pages`
- Rancher consumes the extension catalog via the repo url and branch.

### GitHub Branch Structure
- Once the artifacts are pushed to the GitHub branch, the repository will expose the extension files like so:
```
https://github.com/<org>/<repo>/tree/gh-pages
├── index.yaml
├── assets/
│   ├── index.yaml
│   └── aif-ui/
│       ├── aif-ui-<version>.tgz
│       └── ...
├── charts/
│   └── aif-ui/
│       ├── <version>/
│       │   ├── templates/
│       │   │   ├── _helpers.tpl
│       │   │   └── cr.yaml
│       │   ├── Chart.yaml
│       │   └── values.yaml
│       └── ...
└── extensions/
    └── aif-ui/
        ├── <version>/
        │   ├── plugin/
        │   │   └── ...
        │   └── files.txt
        └── ...
```
This structure mirrors the catalog format that Rancher expects.

### Consuming the Extension from the GitHub Branch in Rancher
- To load the extension from a GitHub branch:
   1. Navigate to Extensions → Manage Repositories
   2. Click Create New Repository
   3. Add a Name, then select `Git repository containing Helm chart or cluster template definitions`
   4. Enter the `Git Repo URL` and the `Git Branch` (`gh-pages`)
   5. Click Create
   6. Wait until the the SUSE AI Factory repository has the `Active` state.
   7. Go back to Extensions and install SUSE AI Factory.
   8. Re-load Rancher Dashboard. Reload the browser to ensure the extension is loaded in the UI (ctrl+r or F5 or cmd+r).
   9. The "SUSE AI Factory" logo will now appear on the left panel of the Rancher Dashboard.

## Contributing

When contributing to this extension:

1. **Follow Standard Patterns**: Use the established domain model and store patterns
2. **Component Organization**: Place components in appropriate directories (formatters/, validators/, pages/)
3. **Type Safety**: Maintain strict TypeScript usage, avoid `any` types
4. **Internationalization**: Add translation keys to l10n/en-us.json for new UI text
5. **Code Quality**: Run `yarn lint` and ensure all pre-commit hooks pass
6. **Feature Flags**: Use feature flags for new functionality
7. **Manual Testing**: Ensure all functionality works across multi-cluster scenarios

### Commit Message Format

This project uses conventional commits enforced by commitlint:

```
type: subject

body (optional)

footer (optional)
```

**Valid types:** `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`, `wip`, `deps`, `security`

Example:
```bash
git commit -m "feat: add multi-cluster installation support"
git commit -m "fix: resolve app installation error handling"
```

## Troubleshooting

### Common Issues

1. **Extension not loading**: Verify URL in developer tools console
2. **Build errors**: Check Node.js version compatibility (requires 20+)
3. **API errors**: Verify cluster permissions and connectivity
4. **Linting errors**: Run `cd pkg/aif-ui && yarn lint` to see details
