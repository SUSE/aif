// All data is fake. Used only by the Token Factory PoC UI.

export interface ModelEndpoint {
  id: string; model: string; workload: string; cluster: string;
  url: string; status: 'Ready' | 'Degraded' | 'Pending'; rateLimit: string; kind: 'chat' | 'embeddings';
}
export interface ApiKey {
  id: string; team: string; prefix: string; models: string[]; quota: string; usedPct: number; expiry: string;
}
export interface Quota { team: string; used: number; quota: number; pricePerM: number }
export interface Workload { name: string; cluster: string; model: string }

const BASE = 'https://tokens.ai-factory.example.com/v1';

export const endpoints: ModelEndpoint[] = [
  { id: 'llama', model: 'llama-3.1-8b-instruct', workload: 'vllm-llama31-8b', cluster: 'gpu-prod-1', url: `${ BASE }/chat/completions`, status: 'Ready', rateLimit: '600 req/min', kind: 'chat' },
  { id: 'mistral', model: 'mistral-7b', workload: 'vllm-mistral-7b', cluster: 'gpu-prod-1', url: `${ BASE }/chat/completions`, status: 'Ready', rateLimit: '300 req/min', kind: 'chat' },
  { id: 'qwen', model: 'qwen2.5-coder', workload: 'vllm-qwen25-coder', cluster: 'gpu-prod-2', url: `${ BASE }/chat/completions`, status: 'Degraded', rateLimit: '120 req/min', kind: 'chat' },
  { id: 'emb', model: 'bge-m3-embeddings', workload: 'tei-bge-m3', cluster: 'gpu-prod-2', url: `${ BASE }/embeddings`, status: 'Ready', rateLimit: '1200 req/min', kind: 'embeddings' }
];

export const apiKeys: ApiKey[] = [
  { id: 'k1', team: 'platform', prefix: 'sk-...a91f', models: ['llama-3.1-8b-instruct', 'mistral-7b'], quota: '50M / month', usedPct: 62, expiry: '2027-03-01' },
  { id: 'k2', team: 'data-science', prefix: 'sk-...7c20', models: ['bge-m3-embeddings', 'llama-3.1-8b-instruct'], quota: '120M / month', usedPct: 88, expiry: '2027-01-15' },
  { id: 'k3', team: 'devtools', prefix: 'sk-...e3b4', models: ['qwen2.5-coder'], quota: '30M / month', usedPct: 41, expiry: '2026-12-31' },
  { id: 'k4', team: 'support-bots', prefix: 'sk-...09dd', models: ['mistral-7b'], quota: '10M / month', usedPct: 97, expiry: '2026-11-30' }
];

export const quotas: Quota[] = [
  { team: 'platform', used: 31, quota: 50, pricePerM: 0.2 },
  { team: 'data-science', used: 105.6, quota: 120, pricePerM: 0.2 },
  { team: 'devtools', used: 12.3, quota: 30, pricePerM: 0.35 },
  { team: 'support-bots', used: 9.7, quota: 10, pricePerM: 0.15 }
];

export const workloads: Workload[] = [
  { name: 'vllm-gemma2-9b', cluster: 'gpu-prod-1', model: 'gemma-2-9b-it' },
  { name: 'vllm-phi3-mini', cluster: 'gpu-dev', model: 'phi-3-mini' },
  { name: 'tei-e5-large', cluster: 'gpu-prod-2', model: 'e5-large-embeddings' }
];

export const kpis = [
  { label: 'Tokens served (24h)', value: '184.2M', delta: '+12%' },
  { label: 'Requests / s', value: '142', delta: '+5%' },
  { label: 'Active API keys', value: '17', delta: '+2' },
  { label: 'p95 latency', value: '812 ms', delta: '-48 ms' },
  { label: 'Est. cost (24h)', value: '$41.30', delta: '+9%' }
];

// 24 hourly points, millions of tokens
export const usageSeries: number[] = [3.1, 2.8, 2.4, 2.2, 2.5, 3.0, 4.2, 5.9, 7.8, 9.4, 10.2, 10.9, 11.4, 11.0, 10.5, 10.8, 11.6, 10.1, 8.7, 7.2, 6.1, 5.0, 4.2, 3.6];

export const sampleAnswer = 'Kubernetes schedules pods onto nodes by filtering nodes that satisfy resource requests, taints and affinity rules, then scoring the remaining candidates. For GPU workloads, the device plugin exposes nvidia.com/gpu as an extended resource so the scheduler can bin-pack model servers like vLLM across your GPU pool. (Mock response, no model was called.)';

export const gatewayBase = BASE;
