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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  plugin.addProduct(productModule as any); // Rancher plugin.addProduct expects a specific shape; module satisfies it at runtime

  // Add routes explicitly
  plugin.addRoutes(routes);
}
