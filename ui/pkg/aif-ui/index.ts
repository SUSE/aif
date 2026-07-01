import { importTypes } from '@rancher/auto-import';
import type { IPlugin } from '@shell/core/types';
import routes from './routing';
import * as productModule from './product';
import './style/brand.css';

export default function(plugin: IPlugin): void {
  importTypes(plugin);
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  plugin.metadata = require('./package.json');

  // Pass the MODULE so Rancher finds `init`
  plugin.addProduct(productModule as any);

  // Add routes explicitly
  plugin.addRoutes(routes);
}
