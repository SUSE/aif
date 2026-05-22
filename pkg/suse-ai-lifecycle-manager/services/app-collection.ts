import { getClusterContext } from '../utils/cluster-operations';
import { log as logger } from '../utils/logger';

// Canonical OCI registry URLs for the two SUSE chart repositories.
// These are the single source of truth for all hardcoded registry URLs in the codebase.
// Air-gapped environments override these via Settings → registryEndpoints.
export const APP_COLLECTION_REPO_URL = 'oci://dp.apps.rancher.io/charts';
export const SUSE_REGISTRY_REPO_URL  = 'oci://registry.suse.com/ai/charts';

export type PackagingFormat = 'HELM_CHART' | 'CONTAINER';

export interface AppCollectionItem {
  name: string;
  slug_name: string;
  description?: string;
  project_url?: string;
  documentation_url?: string;
  reference_guide_url?: string;
  source_code_url?: string;
  logo_url?: string;
  changelog_url?: string;
  last_updated_at?: string;
  packaging_format?: PackagingFormat;
  repository_url?: string;
}

function normalizeLogoUrl(logo?: string): string | undefined {
  if (!logo) return undefined;
  try { new URL(logo); return logo; } catch { /* not absolute */ }
  // These are relative (e.g. "/logos/xxx.png"); load directly from upstream
  return logo.startsWith('/logos/') ? `https://api.apps.rancher.io${logo}` : logo;
}

/**
 * Keep the raw list "as const" so string literals (like 'HELM_CHART') are not widened to `string`.
 * Do NOT annotate with AppCollectionItem here; we preserve the literals then map to the interface.
 */
const RAW_APPS = [
    {
      "name": "Milvus",
      "slug_name": "milvus",
      "description": "Milvus is a high-performance vector database built for scale. It is used by AI applications to organize and search through large amount of unstructured data, such as text, images, and multi-modal information.",
      "project_url": "https://milvus.io/",
      "documentation_url": "https://milvus.io/docs",
      "source_code_url": "https://github.com/milvus-io/milvus",
      "logo_url": "https://apps.rancher.io/logos/milvus.png",
      "changelog_url": "https://milvus.io/docs/release_notes.md",
      "last_updated_at": "2025-04-01T16:10:58.370403Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "Ollama",
      "slug_name": "ollama",
      "description": "Get up and running with Llama 3.2, Mistral, Gemma 2, and other large language models.",
      "project_url": "https://ollama.com/",
      "documentation_url": "https://github.com/ollama/ollama/tree/main/docs",
      "source_code_url": "https://github.com/ollama/ollama",
      "logo_url": "https://apps.rancher.io/logos/ollama.png",
      "changelog_url": "https://github.com/ollama/ollama/releases",
      "last_updated_at": "2025-08-18T22:23:44.766307Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "Open WebUI",
      "slug_name": "open-webui",
      "description": "Open WebUI is an extensible, feature-rich, and user-friendly self-hosted WebUI designed to operate entirely offline. It supports various LLM runners, including Ollama and OpenAI-compatible APIs.",
      "project_url": "https://openwebui.com/",
      "documentation_url": "https://docs.openwebui.com/",
      "source_code_url": "https://github.com/open-webui/open-webui",
      "logo_url": "https://apps.rancher.io/logos/open-webui.png",
      "changelog_url": "https://github.com/open-webui/open-webui/releases",
      "last_updated_at": "2025-06-02T14:49:46.188913Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "Open WebUI mcpo",
      "slug_name": "open-webui-mcpo",
      "description": "A simple, secure MCP-to-OpenAPI proxy server.",
      "project_url": "https://openwebui.com",
      "documentation_url": "https://docs.openwebui.com/openapi-servers/mcp",
      "source_code_url": "https://github.com/open-webui/mcpo",
      "logo_url": "https://apps.rancher.io/logos/open-webui-mcpo.png",
      "last_updated_at": "2025-11-16T22:51:30.016188Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "Open WebUI Pipelines",
      "slug_name": "open-webui-pipelines",
      "description": "Pipelines bring modular, customizable workflows to any UI client supporting OpenAI API specs and much more! Easily extend functionalities, integrate unique logic, and create dynamic workflows with just a few lines of code.",
      "project_url": "https://openwebui.com",
      "documentation_url": "https://docs.openwebui.com/pipelines",
      "source_code_url": "https://github.com/open-webui/pipelines",
      "logo_url": "https://apps.rancher.io/logos/open-webui-pipelines.png",
      "last_updated_at": "2025-05-23T16:25:14.076326Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "PyTorch",
      "slug_name": "pytorch",
      "description": "PyTorch is a machine learning library based on the Torch library, used for applications such as computer vision and natural language processing",
      "project_url": "https://pytorch.org",
      "documentation_url": "https://pytorch.org/docs/stable",
      "source_code_url": "https://github.com/pytorch/pytorch",
      "logo_url": "https://apps.rancher.io/logos/pytorch.png",
      "changelog_url": "https://github.com/pytorch/pytorch/releases",
      "last_updated_at": "2025-05-16T13:41:26.542743Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "SUSE AI Deployer",
      "slug_name": "suse-ai-deployer",
      "description": "A Meta Helm chart for deploying SUSE AI components",
      "project_url": "https://github.com/SUSE/suse-ai-deployer",
      "documentation_url": "https://documentation.suse.com/suse-ai",
      "source_code_url": "https://github.com/SUSE/suse-ai-deployer",
      "logo_url": "https://apps.rancher.io/logos/suse-ai-deployer.png",
      "changelog_url": "https://github.com/SUSE/suse-ai-deployer/releases",
      "last_updated_at": "2025-07-04T08:22:01.087184Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "SUSE AI Observability Extension",
      "slug_name": "suse-ai-observability-extension",
      "description": "The SUSE AI Observability Extension enhances SUSE Observability with AI-optimized dashboards and visualizations. It seamlessly integrates with applications instrumented using the OpenLIT SDK.",
      "project_url": "https://github.com/SUSE/suse-ai-observability-extension",
      "source_code_url": "https://github.com/SUSE/suse-ai-observability-extension",
      "logo_url": "https://apps.rancher.io/logos/suse-ai-observability-extension.png",
      "changelog_url": "https://github.com/SUSE/suse-ai-observability-extension/releases",
      "last_updated_at": "2025-07-29T11:56:35.688301Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "vLLM",
      "slug_name": "vllm",
      "description": "A high-throughput and memory-efficient inference and serving engine for LLMs.",
      "project_url": "https://github.com/vllm-project/vllm",
      "documentation_url": "https://docs.vllm.ai/en/stable",
      "source_code_url": "https://github.com/vllm-project/vllm",
      "logo_url": "https://apps.rancher.io//logos/vllm.png",
      "changelog_url": "https://github.com/vllm-project/vllm/releases",
      "last_updated_at": "2025-09-30T08:22:20.607991Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://dp.apps.rancher.io/charts"    
    },
    {
      "name": "Qdrant",
      "slug_name": "qdrant",
      "description": "High-performance, massive-scale Vector Database and Vector Search Engine for the next generation of AI.",
      "project_url": "https://github.com/qdrant/qdrant",
      "documentation_url": "https://qdrant.tech/documentation/",
      "source_code_url": "https://github.com/qdrant/qdrant",
      "logo_url": "https://qdrant.github.io/qdrant-helm/logo_with_text.svg",
      "changelog_url": "https://github.com/qdrant/qdrant/releases",
      "last_updated_at": "2025-12-19T17:45:42Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://registry.suse.com/ai/charts"    
    },
    {
      "name": "LiteLLM",
      "slug_name": "litellm",
      "description": "Python SDK, Proxy Server (AI Gateway) to call 100+ LLM APIs in OpenAI (or native) format, with cost tracking, guardrails, loadbalancing and logging. [Bedrock, Azure, OpenAI, VertexAI, Cohere, Anthropic, Sagemaker, HuggingFace, VLLM, NVIDIA NIM] ",
      "project_url": "https://github.com/BerriAI/litellm",
      "documentation_url": "https://docs.litellm.ai/docs/",
      "source_code_url": "https://github.com/BerriAI/litellm",
      "logo_url": "https://raw.githubusercontent.com/BerriAI/litellm/refs/heads/main/litellm/proxy/logo.jpg",
      "changelog_url": "https://github.com/BerriAI/litellm/releases",
      "last_updated_at": "2026-01-17T19:55:30Z",
      "packaging_format": "HELM_CHART",
      "repository_url": "oci://registry.suse.com/ai/charts"    
    }
] as const;

/** Freeze literals, normalize logos, and coerce packaging_format to the union */
const STATIC_APPS: AppCollectionItem[] = RAW_APPS.map(it => ({
  ...it,
  logo_url: normalizeLogoUrl(it.logo_url),
  packaging_format: it.packaging_format as PackagingFormat,
  repository_url: it.repository_url
}));

/** Find repository name by URL */
export async function findRepositoryByUrl($store: any, targetUrl: string): Promise<string | null> {
  try {
    const repositories = await fetchClusterRepositories($store);
    const repo = repositories.find(r => r.url === targetUrl);
    return repo?.name || null;
  } catch (err) {
    console.warn('Failed to find repository by URL:', err);
    return null;
  }
}

/** Get cluster repository name from repository URL */
export async function getClusterRepoNameFromUrl($store: any, repoUrl: string): Promise<string | null> {
  return await findRepositoryByUrl($store, repoUrl);
}

/** Static list for now (ignore store/auth). */
export async function fetchSuseAiApps($store: any): Promise<AppCollectionItem[]> {
  return STATIC_APPS.slice();
}

/** Repository information */
export interface AppRepository {
  name: string;
  displayName: string;
  type: string;
  url?: string;
  enabled?: boolean;
}

/** Get list of all cluster repositories */
export async function fetchClusterRepositories($store: any): Promise<AppRepository[]> {
  logger.debug('Starting cluster repositories fetch', {
    component: 'AppCollection'
  });
  try {
    const url = '/k8s/clusters/local/apis/catalog.cattle.io/v1/clusterrepos?limit=1000';
    logger.debug('Requesting cluster repositories', {
      component: 'AppCollection',
      data: { url }
    });
    const res = await $store.dispatch('rancher/request', { url, timeout: 20000 });
    
    logger.debug('Cluster repositories response received', {
      component: 'AppCollection',
      data: {
        hasData: !!res?.data,
        hasItems: !!res?.data?.items,
        dataType: typeof res?.data,
        itemsLength: res?.data?.items ? res.data.items.length : 'N/A'
      }
    });
    
    const repos = res?.data?.items || res?.data || res?.items || [];
    logger.debug('Raw repositories count', {
      component: 'AppCollection',
      data: { count: repos.length }
    });
    
    if (repos.length > 0) {
      logger.debug('First repository sample', {
        component: 'AppCollection',
        data: {
          name: repos[0]?.metadata?.name,
          enabled: repos[0]?.spec?.enabled,
          state: repos[0]?.metadata?.state?.name,
          url: repos[0]?.spec?.url || repos[0]?.spec?.gitRepo
        }
      });
    }
    
    const filtered = repos.filter((repo: any) => {
      const enabled = repo?.spec?.enabled !== false;
      
      // Check if repository is ready based on status conditions
      const conditions = repo?.status?.conditions || [];
      const hasDownloadedCondition = conditions.some((c: any) => 
        (c.type === 'FollowerDownloaded' || c.type === 'OCIDownloaded' || c.type === 'Downloaded') && 
        c.status === 'True'
      );
      const hasIndexConfigMap = !!repo?.status?.indexConfigMapName;
      const isReady = hasDownloadedCondition || hasIndexConfigMap;
      
      logger.debug('Repository filtering', {
        component: 'AppCollection',
        data: {
          repo: repo?.metadata?.name,
          enabled,
          isReady,
          conditionsCount: conditions.length
        }
      });
      return enabled && isReady;
    });
    
    logger.debug('Filtered repositories count', {
      component: 'AppCollection',
      data: { count: filtered.length }
    });
    
    const mapped = filtered.map((repo: any) => ({
      name: repo.metadata?.name || '',
      displayName: getRepoDisplayName(repo.metadata?.name || ''),
      type: getRepoType(repo),
      url: repo.spec?.url || repo.spec?.gitRepo || '',
      enabled: repo.spec?.enabled !== false
    }));
    
    const final = mapped.filter((repo: AppRepository) => repo.name);
    logger.info('Cluster repositories fetched successfully', {
      component: 'AppCollection',
      data: {
        count: final.length,
        repos: final.map((r: AppRepository) => ({ name: r.name, type: r.type, enabled: r.enabled }))
      }
    });
    
    return final;
  } catch (e: any) {
    logger.error('Failed to fetch cluster repositories', e, {
      component: 'AppCollection'
    });
    return [];
  }
}

function getRepoDisplayName(name: string): string {
  const displayNames: Record<string, string> = {
    'rancher-charts': 'Rancher Charts',
    'rancher-partner-charts': 'Rancher Partner Charts',
    'rancher-rke2-charts': 'RKE2 Charts',
    'jetstack': 'Jetstack',
    'suse-edge': 'SUSE Edge'
  };
  return displayNames[name] || name.replace(/-/g, ' ').replace(/\b\w/g, l => l.toUpperCase());
}

function getRepoType(repo: any): string {
  if (repo.spec?.gitRepo) return 'git';
  if (repo.spec?.url?.startsWith('oci:')) return 'oci';
  return 'helm';
}

/** Fetch apps from a specific cluster repository */
export async function fetchAppsFromRepository($store: any, repoName: string): Promise<AppCollectionItem[]> {
  logger.debug('Starting repository apps fetch', {
    component: 'AppCollection',
    data: { repoName }
  });

  const found = await getClusterContext($store, { repoName: repoName});
  if (!found) {
    logger.warn(`ClusterRepo "${repoName}" not found in any cluster`);
    return [];
  }
  const { baseApi } = found
  
  try {
    const indexUrl = `${baseApi}/catalog.cattle.io.clusterrepos/${encodeURIComponent(repoName)}?link=index`;
    logger.debug('Requesting repository index', {
      component: 'AppCollection',
      data: { repoName, indexUrl }
    });
    const res = await $store.dispatch('rancher/request', { url: indexUrl, timeout: 20000 });
    
    logger.debug('Repository index response', {
      component: 'AppCollection',
      data: {
        repoName,
        hasData: !!res?.data,
        dataType: typeof res?.data
      }
    });
    
    const indexData = res?.data || res;
    const entries = indexData?.entries || {};
    logger.debug('Repository index entries', {
      component: 'AppCollection',
      data: {
        repoName,
        entriesCount: Object.keys(entries).length
      }
    });
    
    const apps: AppCollectionItem[] = [];
    
    for (const [chartName, versions] of Object.entries(entries)) {
      if (!Array.isArray(versions) || versions.length === 0) continue;
      
      const latestVersion = versions[0] as any;
      const app: AppCollectionItem = {
        name: latestVersion.name || chartName,
        slug_name: chartName,
        description: latestVersion.description || '',
        project_url: latestVersion.home || '',
        source_code_url: Array.isArray(latestVersion.sources) ? latestVersion.sources[0] : latestVersion.sources,
        logo_url: latestVersion.icon ? normalizeLogoUrl(latestVersion.icon) : undefined,
        last_updated_at: latestVersion.created || new Date().toISOString(),
        packaging_format: 'HELM_CHART'
      };
      
      apps.push(app);
    }
    
    logger.info('Repository apps fetched successfully', {
      component: 'AppCollection',
      data: { repoName, count: apps.length }
    });
    return apps.sort((a, b) => new Date(b.last_updated_at || 0).getTime() - new Date(a.last_updated_at || 0).getTime());
  } catch (e) {
    logger.error('Failed to fetch apps from repository', e, {
      component: 'AppCollection',
      data: { repoName }
    });
    return [];
  }
}

/** Fetch apps from all cluster repositories */
export async function fetchAllRepositoryApps($store: any): Promise<{ [repoName: string]: AppCollectionItem[] }> {
  logger.debug('Starting fetch all repository apps', {
    component: 'AppCollection'
  });
  const repositories = await fetchClusterRepositories($store);
  logger.debug('Found repositories', {
    component: 'AppCollection',
    data: {
      count: repositories.length,
      repos: repositories.map(r => ({ name: r.name, enabled: r.enabled }))
    }
  });
  
  const repoApps: { [repoName: string]: AppCollectionItem[] } = {};
  
  await Promise.all(repositories.map(async (repo) => {
    logger.debug('Processing repository', {
      component: 'AppCollection',
      data: { repoName: repo.name }
    });
    try {
      const apps = await fetchAppsFromRepository($store, repo.name);
      if (apps.length > 0) {
        repoApps[repo.name] = apps;
        logger.debug('Repository apps loaded', {
          component: 'AppCollection',
          data: { repoName: repo.name, count: apps.length }
        });
      }
    } catch (e) {
      logger.error('Failed to fetch apps from repository', e, {
        component: 'AppCollection',
        data: { repoName: repo.name }
      });
    }
  }));
  
  logger.info('All repository apps fetched successfully', {
    component: 'AppCollection',
    data: {
      totalRepos: Object.keys(repoApps).length,
      repos: Object.keys(repoApps).map(key => ({ repo: key, count: repoApps[key].length }))
    }
  });
  return repoApps;
}
