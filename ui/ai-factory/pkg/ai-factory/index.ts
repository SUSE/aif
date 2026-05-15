import { importTypes } from '@rancher/auto-import';
import { IPlugin } from '@shell/core/types';
import routes from './routing';
import './style/brand.css';

/**
 * SUSE AI Factory UI extension entry point.
 */
export default function(plugin: IPlugin): void {
  // Auto-import model, detail, edit from the folders
  importTypes(plugin);

  // Provide plugin metadata from package.json and resolve the local tile icon.
  plugin.metadata = {
    ...require('./package.json'),
    icon: require('./assets/logo.svg')
  };

  // Note: AI Factory uses the 'management' store for all CRD resources
  // (Bundles, Blueprints, Workloads, Settings). The management store
  // auto-discovers all CRDs via Steve API. No custom store needed.
  plugin.addProduct(require('./config/aif-product'));
  plugin.addRoutes(routes);
  plugin.addL10n('en-us', require('./l10n/en-us.yaml'));
}
