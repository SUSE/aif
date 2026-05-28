/**
 * Mock API responses for UI development before backend integration is ready.
 */
import type { App } from './operator-api';

const MOCK_APPS: App[] = [
  {
    id:                 'nvidia.nim-llm:1.2.0',
    name:               'nim-llm',
    displayName:        'NVIDIA NIM for LLMs',
    description:        'Deploy optimized large language models with NVIDIA NIM inference microservices. Supports Llama 3, Mistral, and other popular LLM architectures.',
    publisher:          'NVIDIA',
    version:            '1.2.0',
    logoURL:            '',
    source:             'nvidia',
    assetType:          'chart',
    categories:         ['Inference', 'LLM'],
    tags:               ['gpu', 'tensorrt', 'triton'],
    chartRef:           { repo: 'oci://registry.suse.com/ai/charts/nvidia', chart: 'nim-llm', version: '1.2.0' },
    projectURL:         'https://developer.nvidia.com/nim',
    referenceBlueprint: false,
    useCase:            'Text generation and chat completions',
    lastUpdatedAt:      '2026-04-15T10:30:00Z'
  },
  {
    id:                 'nvidia.nim-vlm:1.0.0',
    name:               'nim-vlm',
    displayName:        'NVIDIA NIM for VLMs',
    description:        'Run vision-language models at scale with NVIDIA NIM. Supports multimodal inference for image understanding and visual question answering.',
    publisher:          'NVIDIA',
    version:            '1.0.0',
    logoURL:            '',
    source:             'nvidia',
    assetType:          'chart',
    categories:         ['Inference', 'VLM'],
    tags:               ['gpu', 'vision', 'multimodal'],
    chartRef:           { repo: 'oci://registry.suse.com/ai/charts/nvidia', chart: 'nim-vlm', version: '1.0.0' },
    projectURL:         'https://developer.nvidia.com/nim',
    referenceBlueprint: false,
    useCase:            'Visual question answering and image captioning',
    lastUpdatedAt:      '2026-03-20T14:00:00Z'
  },
  {
    id:                 'nvidia.nim-llm-blueprint:1.2.0',
    name:               'nim-llm-blueprint',
    displayName:        'NIM LLM Reference Blueprint',
    description:        'Pre-validated reference blueprint for deploying NIM LLM with GPU Operator, Network Operator, and monitoring. Ready-to-deploy AI stack for text generation workloads.',
    publisher:          'NVIDIA',
    version:            '1.2.0',
    logoURL:            '',
    source:             'nvidia',
    assetType:          'chart',
    categories:         ['Reference Blueprint', 'LLM'],
    tags:               ['gpu', 'validated', 'stack'],
    chartRef:           { repo: 'oci://registry.suse.com/ai/charts/nvidia', chart: 'nim-llm', version: '1.2.0' },
    projectURL:         'https://developer.nvidia.com/nim',
    referenceBlueprint: true,
    useCase:            'Full-stack LLM inference deployment',
    lastUpdatedAt:      '2026-04-10T08:00:00Z'
  },
  {
    id:                 'suse.gpu-operator:24.9.0',
    name:               'gpu-operator',
    displayName:        'NVIDIA GPU Operator',
    description:        'Automates the management of all NVIDIA software components needed to provision GPUs in Kubernetes, including drivers, container runtime, device plugin, and monitoring.',
    publisher:          'SUSE',
    version:            '24.9.0',
    logoURL:            '',
    source:             'suse',
    assetType:          'chart',
    categories:         ['Infrastructure', 'GPU'],
    tags:               ['gpu', 'driver', 'operator'],
    chartRef:           { repo: 'oci://dp.apps.rancher.io/charts', chart: 'gpu-operator', version: '24.9.0' },
    projectURL:         'https://apps.rancher.io',
    referenceBlueprint: false,
    lastUpdatedAt:      '2026-05-01T12:00:00Z'
  },
  {
    id:                 'suse.network-operator:24.7.0',
    name:               'network-operator',
    displayName:        'NVIDIA Network Operator',
    description:        'Manages networking resources in Kubernetes for high-performance GPU workloads. Supports RDMA, GPUDirect, and SR-IOV for multi-node training.',
    publisher:          'SUSE',
    version:            '24.7.0',
    logoURL:            '',
    source:             'suse',
    assetType:          'chart',
    categories:         ['Infrastructure', 'Networking'],
    tags:               ['rdma', 'gpudirect', 'sriov'],
    chartRef:           { repo: 'oci://dp.apps.rancher.io/charts', chart: 'network-operator', version: '24.7.0' },
    projectURL:         'https://apps.rancher.io',
    referenceBlueprint: false,
    lastUpdatedAt:      '2026-04-20T09:30:00Z'
  },
  {
    id:                 'suse.ollama:0.5.4',
    name:               'ollama',
    displayName:        'Ollama',
    description:        'Run large language models locally. Get up and running with Llama 3, Mistral, Gemma, and other models on Kubernetes with GPU acceleration.',
    publisher:          'SUSE',
    version:            '0.5.4',
    logoURL:            '',
    source:             'suse',
    assetType:          'chart',
    categories:         ['Inference', 'LLM'],
    tags:               ['llm', 'local', 'gpu'],
    chartRef:           { repo: 'oci://dp.apps.rancher.io/charts', chart: 'ollama', version: '0.5.4' },
    projectURL:         'https://apps.rancher.io',
    referenceBlueprint: false,
    lastUpdatedAt:      '2026-04-30T23:56:07Z'
  },
  {
    id:                 'suse.open-webui:0.6.5',
    name:               'open-webui',
    displayName:        'Open WebUI',
    description:        'Full-featured web interface for interacting with LLMs. Supports multiple model backends, conversation history, RAG, and collaborative features.',
    publisher:          'SUSE',
    version:            '0.6.5',
    logoURL:            '',
    source:             'suse',
    assetType:          'chart',
    categories:         ['Application', 'Chat'],
    tags:               ['ui', 'chat', 'rag'],
    chartRef:           { repo: 'oci://dp.apps.rancher.io/charts', chart: 'open-webui', version: '0.6.5' },
    projectURL:         'https://apps.rancher.io',
    referenceBlueprint: false,
    lastUpdatedAt:      '2026-05-10T16:45:00Z'
  },
  {
    id:                 'suse.milvus:2.4.0',
    name:               'milvus',
    displayName:        'Milvus Vector Database',
    description:        'High-performance vector database for AI applications. Supports similarity search, hybrid search, and production-ready RAG pipelines at scale.',
    publisher:          'SUSE',
    version:            '2.4.0',
    logoURL:            '',
    source:             'suse',
    assetType:          'chart',
    categories:         ['Data', 'Vector DB'],
    tags:               ['vector', 'embeddings', 'rag'],
    chartRef:           { repo: 'oci://dp.apps.rancher.io/charts', chart: 'milvus', version: '2.4.0' },
    projectURL:         'https://apps.rancher.io',
    referenceBlueprint: false,
    lastUpdatedAt:      '2026-03-15T10:00:00Z'
  }
];

export const mockAPI = {
  blueprints: {
    // versions, deploy, deprecate, withdraw, reactivate
  },

  workloads: {
    // start, stop, restart, upgrade
  },

  apps: {
    list(params?: { source?: string; category?: string; includeReferenceBlueprints?: boolean }): App[] {
      let result = MOCK_APPS;

      if (params?.source && params.source !== 'all') {
        result = result.filter((a) => a.source === params.source);
      }
      if (params?.category) {
        result = result.filter((a) => a.categories.includes(params.category!));
      }
      if (!params?.includeReferenceBlueprints) {
        result = result.filter((a) => !a.referenceBlueprint);
      }

      return result;
    },

    categories(): string[] {
      const cats = new Set<string>();

      for (const app of MOCK_APPS) {
        for (const c of app.categories) {
          cats.add(c);
        }
      }

      return [...cats].sort();
    }
  },

  settings: {
    // get, update, testConnection
  }
};

// VueCLI's DefinePlugin inlines process.env.USE_MOCK_API at build time — this is not a runtime toggle.
// Set USE_MOCK_API=true in the build environment to enable; the value is frozen into the bundle.
//
// Scope: only operator HTTP reads in operator-api.ts (listApps, listCategories, etc.) are mocked.
// Anything that goes through Rancher's Steve store directly bypasses this flag entirely and
// requires the AIF CRDs to be installed on the cluster (kubectl apply -f charts/aif-operator/crds/).
// Without them, Steve cannot resolve the schema and throws
// "Cannot read properties of undefined (reading 'linkFor')".
export const USE_MOCK_API = process.env.USE_MOCK_API === 'true';
