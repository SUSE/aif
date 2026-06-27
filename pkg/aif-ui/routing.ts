import { PRODUCT, PAGE_TYPES } from './config/suseai';

export default [
  // Product root → redirect to Overview
  {
    name:     `c-cluster-${PRODUCT}-home-root`,
    path:     `/c/:cluster/${PRODUCT}`,
    redirect: { name: `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`, params: { product: PRODUCT } },
    meta:     { product: PRODUCT }
  },

  // Overview page
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.OVERVIEW}`,
    component: () => import('./pages/Overview.vue'),
    meta:      { product: PRODUCT, category: 'overview' }
  },

  // Apps page - Main application listing
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.APPS}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.APPS}`,
    component: () => import('./pages/Apps.vue'),
    meta:      { product: PRODUCT, category: 'apps' }
  },


  // Install flow (step-based wizard)
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.INSTALL}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.APPS}/:slug/${PAGE_TYPES.INSTALL}`,
    props:     true,
    component: () => import('./pages/Install.vue'),
    meta:      { product: PRODUCT, category: 'install' }
  },

  // App instances page - shows all instances of a specific app
  {
    name:      `c-cluster-${PRODUCT}-app-instances`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.APPS}/:slug/instances`,
    props:     true,
    component: () => import('./pages/AppInstances.vue'),
    meta:      { product: PRODUCT, category: 'app-instances' }
  },

  // Manage flow (manage existing installation)
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.MANAGE}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.APPS}/:slug/${PAGE_TYPES.MANAGE}`,
    props:     true,
    component: () => import('./pages/Manage.vue'),
    meta:      { product: PRODUCT, category: 'manage' }
  },

  // Repositories management (future)
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.REPOSITORIES}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.REPOSITORIES}`,
    component: () => import('./pages/Apps.vue'), // Placeholder for now
    meta:      { product: PRODUCT, category: 'repositories' }
  },

  // Settings management (future)
  {
    name:      `c-cluster-${PRODUCT}-${PAGE_TYPES.SETTINGS}`,
    path:      `/c/:cluster/${PRODUCT}/${PAGE_TYPES.SETTINGS}`,
    component: () => import('./pages/Settings.vue'),
    meta:      { product: PRODUCT, category: 'settings' }
  },

  // Blueprints list page
  {
    name:      `c-cluster-${ PRODUCT }-${ PAGE_TYPES.BLUEPRINTS }`,
    path:      `/c/:cluster/${ PRODUCT }/${ PAGE_TYPES.BLUEPRINTS }`,
    component: () => import('./pages/Blueprints.vue'),
    meta:      { product: PRODUCT, category: 'blueprints' }
  },

  // Blueprint create / edit wizard
  {
    name:      `c-cluster-${ PRODUCT }-blueprint-create`,
    path:      `/c/:cluster/${ PRODUCT }/${ PAGE_TYPES.BLUEPRINTS }/create`,
    component: () => import('./pages/BlueprintCreate.vue'),
    meta:      { product: PRODUCT, category: 'blueprint-create' }
  },

  // Blueprint install wizard
  {
    name:      `c-cluster-${ PRODUCT }-blueprint-install`,
    path:      `/c/:cluster/${ PRODUCT }/${ PAGE_TYPES.BLUEPRINTS }/install`,
    component: () => import('./pages/BlueprintInstall.vue'),
    meta:      { product: PRODUCT, category: 'blueprint-install' }
  },

  // AI Workloads page - all deployed workloads across Apps and Blueprints
  {
    name:      `c-cluster-${ PRODUCT }-${ PAGE_TYPES.WORKLOADS }`,
    path:      `/c/:cluster/${ PRODUCT }/${ PAGE_TYPES.WORKLOADS }`,
    component: () => import('./pages/AIWorkloads.vue'),
    meta:      { product: PRODUCT, category: 'workloads' }
  },

  // About page
  {
    name:      `c-cluster-${ PRODUCT }-${ PAGE_TYPES.ABOUT }`,
    path:      `/c/:cluster/${ PRODUCT }/${ PAGE_TYPES.ABOUT }`,
    component: () => import('./pages/About.vue'),
    meta:      { product: PRODUCT, category: 'about' }
  },

  // Legacy routes (kept for compatibility during transition)
  {
    name:      `c-cluster-${PRODUCT}-home`,
    path:      `/c/:cluster/${PRODUCT}/home`,
    redirect:  { name: `c-cluster-${PRODUCT}-${PAGE_TYPES.OVERVIEW}`, params: { product: PRODUCT } },
    meta:      { product: PRODUCT }
  }
];
