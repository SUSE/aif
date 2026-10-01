// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { nextTick, ref } from 'vue';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import AIWorkloadDetailPanel from '../AIWorkloadDetailPanel.vue';
import { BLUEPRINT_NAME_LABEL } from '../../types/blueprint-types';
import type { AIWorkload } from '../../types/aiworkload-types';
import type { Blueprint } from '../../types/blueprint-types';

// CodeMirror needs a real layout engine; the tests only care what it shows.
vi.mock('@shell/components/YamlEditor', async(importOriginal) => ({
  ...(await importOriginal<Record<string, unknown>>()),
  default: {
    props:    ['value'],
    methods:  { refresh: () => undefined },
    template: '<pre class="yaml-stub">{{ JSON.stringify(value) }}</pre>',
  },
}));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../l10n/en-us.yaml'), 'utf8'));

// Strict for this extension's keys; the shell's own strings come back as keys.
function translate(key: string, args: Record<string, string> = {}): string {
  if (!key.startsWith('suseai.')) return key;
  const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);

  if (typeof value !== 'string') throw new Error(`Missing translation: ${ key }`);

  return value.replace(/\{(\w+)\}/g, (_match, name) => args[name] ?? '');
}

function blueprintWorkload(status?: AIWorkload['status'], spec: Partial<AIWorkload['spec']> = {}): AIWorkload {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'AIWorkload',
    metadata:   { name: 'rag', namespace: 'suseai' },
    spec:       {
      displayName:      'My RAG',
      targetNamespace:  'rag-ns',
      targetClusters:   ['local', 'c-m-1'],
      deployStrategy:   'FleetBundle',
      fleetBundleNames: ['rag-ollama'],
      source:           { sourceType: 'Blueprint', blueprint: { name: 'simple-rag', version: '1.0.0' } },
      ...spec,
    },
    status,
  };
}

function appWorkload(status?: AIWorkload['status']): AIWorkload {
  return {
    apiVersion: 'ai-factory.suse.com/v1alpha1',
    kind:       'AIWorkload',
    metadata:   { name: 'ollama', namespace: 'suseai' },
    spec:       {
      displayName:     'Ollama',
      targetNamespace: 'ollama',
      targetClusters:  ['local'],
      source:          {
        sourceType: 'App',
        app:        {
          chartRepo: 'suse-ai', chartName: 'ollama', chartVersion: '1.2.3', release: 'ollama-rel'
        },
      },
    },
    status,
  };
}

const blueprint: Blueprint = {
  apiVersion: 'ai-factory.suse.com/v1alpha1',
  kind:       'Blueprint',
  metadata:   { name: 'simple-rag-1-0-0', labels: { [BLUEPRINT_NAME_LABEL]: 'simple-rag' } },
  spec:       {
    displayName: 'Simple RAG',
    version:     '1.0.0',
    components:  [
      {
        chartRepo: 'suse-ai', chartName: 'ollama', chartVersion: '1.0.0', values: { gpu: { enabled: true } }
      },
      {
        chartRepo: 'suse-ai', chartName: 'milvus', chartVersion: '4.0.0', targetNamespace: 'vectors'
      },
    ],
  },
};

const mounted: ReturnType<typeof mount>[] = [];

afterEach(() => mounted.splice(0).forEach((wrapper) => wrapper.unmount()));

function setup(initial: AIWorkload, bp: Blueprint | null = null) {
  const workload = ref<AIWorkload | undefined>(initial);
  const store = {
    getters: { 'i18n/t': translate },
    commit:  vi.fn(),
    dispatch: vi.fn(),
  };
  const wrapper = mount(AIWorkloadDetailPanel, {
    props: {
      workload:  () => workload.value,
      blueprint: () => bp,
      clusters:  () => [{ id: 'local', name: 'Local', ready: true }, { id: 'c-m-1', name: 'Edge', ready: true }],
    },
    global: {
      provide: { store },
      mocks:   { $store: store },
      // Rancher's global t(); a globalProperty, so the panel's own t still wins.
      plugins: [{ install: (app) => { app.config.globalProperties.t = translate; } }],
      directives: { 'clean-html': {}, 'clean-tooltip': {}, 'stripped-aria-label': {}, 'trim-whitespace': {} },
      stubs:   {
        t:          true,
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        // A plain table: one row per item, cell slots where the panel supplies them.
        SortableTable: {
          props:    ['headers', 'rows', 'keyField'],
          template: `<table><tr v-for="row in rows" :key="row[keyField]" class="row-stub">
            <td v-for="h in headers" :key="h.name"><slot :name="'cell:' + h.name" :row="row">{{ row[h.value] }}</slot></td>
          </tr></table>`,
        },
      },
    },
  });

  mounted.push(wrapper);

  return { wrapper, workload, store };
}

describe('AIWorkloadDetailPanel', () => {
  it('interpolates the workload name into the title', () => {
    const { wrapper } = setup(blueprintWorkload(), blueprint);

    expect(wrapper.text()).toContain('My RAG (AI Workload) - Details');
  });

  it('follows the live workload instead of the snapshot it opened with', async() => {
    const { wrapper, workload } = setup(appWorkload({ phase: 'Pending' }));

    expect(wrapper.text()).not.toContain('Manage');

    workload.value = appWorkload({ phase: 'Running' });
    await nextTick();

    expect(wrapper.text()).toContain('Manage');
  });

  it('closes itself when the workload disappears', async() => {
    const { wrapper, workload, store } = setup(appWorkload({ phase: 'Running' }));

    workload.value = undefined;
    await nextTick();

    expect(store.commit).toHaveBeenCalledWith('slideInPanel/close');
    // The last known state stays on screen while the panel slides shut.
    expect(wrapper.text()).toContain('Ollama (AI Workload) - Details');
  });

  it('surfaces component failures and the active operation on the Status tab', () => {
    const { wrapper } = setup(blueprintWorkload({
      phase:             'Failed',
      componentStatuses: [
        {
          componentName: 'ollama', releaseName: 'rag-ollama', clusterId: 'c-m-1', phase: 'Failed', message: 'ImagePullBackOff'
        },
      ],
      activeOperation: {
        type: 'Upgrade', nonce: 'n', requestedAt: '', state: 'Failed', reason: 'render failed'
      },
    }), blueprint);
    const operation = wrapper.get('[data-testid="active-operation"]');

    expect(operation.text()).toContain('Upgrade: Failed');
    expect(operation.text()).toContain('(render failed)');
    expect(wrapper.text()).toContain('ImagePullBackOff');
    expect(wrapper.text()).toContain('Edge');
  });

  it('lists each component with its release and target namespace', () => {
    const { wrapper } = setup(blueprintWorkload({
      componentStatuses: [{
        componentName: 'ollama', releaseName: 'rag-ollama', clusterId: 'local', phase: 'Running'
      }],
    }), blueprint);
    const rows = wrapper.findAll('.row-stub').map((row) => row.text());

    expect(rows.find((row) => row.includes('milvus'))).toContain('vectors');
    expect(rows.find((row) => row.includes('ollama'))).toContain('rag-ollama');
    expect(rows.find((row) => row.includes('ollama'))).toContain('rag-ns');
  });

  it('shows Blueprint defaults on the Values tab when nothing was customized', () => {
    const { wrapper } = setup(blueprintWorkload(), blueprint);
    const editors = wrapper.findAll('.yaml-stub').map((editor) => editor.text());

    expect(editors).toContain(JSON.stringify({ gpu: { enabled: true } }));
    expect(wrapper.text()).toContain('Blueprint defaults');
    expect(wrapper.text()).toContain('Values are shown in plain text');
  });

  it('shows overrides instead of defaults and skips excluded components', () => {
    const { wrapper } = setup(blueprintWorkload(undefined, {
      componentValues: [
        { componentName: 'ollama', values: { replicas: 2 } },
        { componentName: 'milvus', enabled: false },
      ],
    }), blueprint);
    const editors = wrapper.findAll('.yaml-stub').map((editor) => editor.text());

    expect(editors).toContain(JSON.stringify({ replicas: 2 }));
    expect(editors).not.toContain(JSON.stringify({ gpu: { enabled: true } }));
    expect(wrapper.text()).toContain('Customized');
    expect(wrapper.text()).toContain('Excluded');
  });

  it('links Fleet bundles in every workspace the targets map to', () => {
    const { wrapper } = setup(blueprintWorkload(), blueprint);
    const links = wrapper.get('[data-testid="fleet-bundles"]').findAll('a').map((link) => link.attributes('href'));

    expect(links).toEqual([
      '/c/_/fleet/fleet.cattle.io.bundle/fleet-local/rag-ollama',
      '/c/_/fleet/fleet.cattle.io.bundle/fleet-default/rag-ollama',
    ]);
  });

  it('does not claim Fleet bundles for the Helm strategy', () => {
    const { wrapper } = setup(blueprintWorkload(undefined, { deployStrategy: 'Helm' }), blueprint);

    expect(wrapper.find('[data-testid="fleet-bundles"]').exists()).toBe(false);
  });

  it('renders target clusters as chips rather than a disabled select', () => {
    const { wrapper } = setup(blueprintWorkload(), blueprint);
    const clusters = wrapper.get('[data-testid="target-clusters"]');

    expect(clusters.findAll('.cluster-chip').map((chip) => chip.text())).toEqual(['Local', 'Edge']);
    expect(clusters.find('.labeled-select').exists()).toBe(false);
  });

  it('keeps an App workload to one chart, without an Applications tab', () => {
    const { wrapper } = setup(appWorkload({ phase: 'Running' }));

    expect(wrapper.text()).not.toContain('Applications');
    expect(wrapper.text()).not.toContain('Helm Release');
    expect(wrapper.text()).not.toContain('Blueprint details are not available');
    expect(wrapper.text()).toContain('No custom values are set');
  });
});
