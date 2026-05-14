import { getApiBase } from '../utils/api-base';
import { mockAPI, USE_MOCK_API } from '../utils/mock-api';

export interface ChartRef {
  repo: string;
  chart: string;
  version: string;
}

export interface App {
  id: string;
  name: string;
  displayName: string;
  description: string;
  publisher: string;
  version: string;
  logoURL: string;
  source: string;
  assetType: string;
  categories: string[];
  tags: string[];
  chartRef: ChartRef;
  projectURL: string;
  referenceBlueprint: boolean;
  useCase?: string;
}

export interface FetchAppsParams {
  source?: 'nvidia' | 'suse' | 'all';
  category?: string;
  includeReferenceBlueprints?: boolean;
}

export async function fetchApps(params?: FetchAppsParams): Promise<App[]> {
  if (USE_MOCK_API) {
    return mockAPI.apps.list(params);
  }

  const query = new URLSearchParams();

  if (params?.source && params.source !== 'all') {
    query.set('source', params.source);
  }
  if (params?.category) {
    query.set('category', params.category);
  }
  if (params?.includeReferenceBlueprints !== undefined) {
    query.set('includeReferenceBlueprints', String(params.includeReferenceBlueprints));
  }

  const qs = query.toString();
  const url = `${ getApiBase() }/api/v1/apps${ qs ? `?${ qs }` : '' }`;
  const res = await fetch(url);

  if (!res.ok) {
    throw new Error(`GET ${ url } failed: ${ res.status }`);
  }

  return res.json();
}

export async function fetchApp(id: string): Promise<App> {
  if (USE_MOCK_API) {
    const app = mockAPI.apps.list({ includeReferenceBlueprints: true }).find((a) => a.id === id);

    if (!app) {
      throw new Error(`App not found: ${ id }`);
    }

    return app;
  }

  const url = `${ getApiBase() }/api/v1/apps/${ encodeURIComponent(id) }`;
  const res = await fetch(url);

  if (!res.ok) {
    throw new Error(`GET ${ url } failed: ${ res.status }`);
  }

  return res.json();
}

export async function fetchCategories(): Promise<string[]> {
  if (USE_MOCK_API) {
    return mockAPI.apps.categories();
  }

  const url = `${ getApiBase() }/api/v1/apps/categories`;
  const res = await fetch(url);

  if (!res.ok) {
    throw new Error(`GET ${ url } failed: ${ res.status }`);
  }

  return res.json();
}
