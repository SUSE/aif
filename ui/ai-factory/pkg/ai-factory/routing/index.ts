import { PRODUCT_NAME, PAGE_IDS } from '../config/types';

const routes = [
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.OVERVIEW }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.OVERVIEW }`,
    component: () => import('../pages/overview.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.OVERVIEW }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.APPS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.APPS }`,
    component: () => import('../pages/apps.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.APPS }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.BLUEPRINTS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.BLUEPRINTS }`,
    component: () => import('../pages/blueprints.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.BLUEPRINTS }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.WORKLOADS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.WORKLOADS }`,
    component: () => import('../pages/workloads.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.WORKLOADS }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.SETTINGS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.SETTINGS }`,
    component: () => import('../pages/settings.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.SETTINGS }
  },
  // AIDEV: task-3-1 — App Install wizard route
  {
    name:      `${ PRODUCT_NAME }-c-cluster-app-install`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/apps/:id/install`,
    component: () => import('../pages/wizards/app-install.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.APPS }
  },
  // AIDEV: /task-3-1
  // AIDEV: task-3-2 — App Manage page (P6-8)
  {
    name:      `${ PRODUCT_NAME }-c-cluster-workload-manage`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/workloads/:ns/:name/manage`,
    component: () => import('../pages/manage.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.WORKLOADS }
  },
  // AIDEV: /task-3-2
  // AIDEV: 2-3 — Blueprint Create wizard route
  {
    name:      `${ PRODUCT_NAME }-c-cluster-blueprint-create`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/blueprints/create`,
    component: () => import('../pages/wizards/blueprint-create.vue'),
    meta:      { product: PRODUCT_NAME, pageId: PAGE_IDS.BLUEPRINTS }
  }
  // AIDEV: /2-3
];

export default routes;
