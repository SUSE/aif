import { PRODUCT, LAB_ROUTE, LAB_PATH } from './config';

export default [
  {
    name:      LAB_ROUTE,
    path:      LAB_PATH,
    component: () => import('./pages/TokenFactory.vue'),
    meta:      { product: PRODUCT, category: 'lab-token-factory' }
  }
];
