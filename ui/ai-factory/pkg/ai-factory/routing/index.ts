import { PRODUCT_NAME, PAGE_IDS } from '../config/types';

const routes = [
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.OVERVIEW }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.OVERVIEW }`,
    component: () => import('../pages/overview.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.OVERVIEW
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.APPS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.APPS }`,
    component: () => import('../pages/apps.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.APPS
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.BLUEPRINTS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.BLUEPRINTS }`,
    component: () => import('../pages/blueprints.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.BLUEPRINTS
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.BUNDLES }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.BUNDLES }`,
    component: () => import('../pages/bundles.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.BUNDLES
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.WORKLOADS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.WORKLOADS }`,
    component: () => import('../pages/workloads.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.WORKLOADS
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.PENDING_REVIEWS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.PENDING_REVIEWS }`,
    component: () => import('../pages/pending-reviews.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.PENDING_REVIEWS
    }
  },
  {
    name:      `${ PRODUCT_NAME }-c-cluster-${ PAGE_IDS.SETTINGS }`,
    path:      `/c/:cluster/${ PRODUCT_NAME }/${ PAGE_IDS.SETTINGS }`,
    component: () => import('../pages/settings.vue'),
    meta:      {
      product: PRODUCT_NAME,
      pageId:  PAGE_IDS.SETTINGS
    }
  }
];

export default routes;
