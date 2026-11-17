import { importTypes } from '@rancher/auto-import';
import type { IPlugin } from '@shell/core/types';
import routes from './routing';
import * as productModule from './product';

export default function(plugin: IPlugin): void {
  importTypes(plugin);
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  plugin.metadata = require('./package.json');

  // Module with `init`; it only extends the AIF product, it does not create one.
  plugin.addProduct(productModule as any);

  plugin.addRoutes(routes);
}
