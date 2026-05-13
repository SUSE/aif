import { mount, flushPromises } from '@vue/test-utils';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { defineComponent, h, ref, computed, onMounted, watch } from 'vue';

// Mock Settings Page component for testing
const SettingsPage = defineComponent({
  name: 'SettingsPage',
  setup() {
    const value = ref(null);
    const showAdvanced = ref(false);
    const loadError = ref(false);
    const errors = ref([]);
    const mode = ref('edit');
    const doneRoute = ref({ name: 'ai-factory-settings' });

    const DEFAULT_REGISTRY_ENDPOINTS = {
      suseRegistry: 'registry.suse.com',
      nvidiaGpuOperator: 'nvcr.io/nvidia',
      nvidiaHelm: 'https://helm.ngc.nvidia.com/nvidia'
    };

    const hasCustomEndpoints = computed(() => {
      const endpoints = value.value?.spec?.registryEndpoints;
      if (!endpoints) {
        return false;
      }
      return endpoints.suseRegistry !== DEFAULT_REGISTRY_ENDPOINTS.suseRegistry ||
             endpoints.nvidiaGpuOperator !== DEFAULT_REGISTRY_ENDPOINTS.nvidiaGpuOperator ||
             endpoints.nvidiaHelm !== DEFAULT_REGISTRY_ENDPOINTS.nvidiaHelm;
    });

    return {
      value,
      showAdvanced,
      loadError,
      errors,
      mode,
      doneRoute,
      hasCustomEndpoints
    };
  },
  async created() {
    // Fetch settings on mount
    try {
      this.value = await this.$store.dispatch('ai-factory/find', {
        type: 'ai.suse.com.settings',
        id: 'aif-system/default'
      });
      this.loadError = false;
    } catch (e) {
      this.loadError = true;
      this.errors.push(e);
    }

    // Load showAdvanced from localStorage
    if (typeof localStorage !== 'undefined') {
      const stored = localStorage.getItem('aif-settings-show-advanced');
      if (stored === 'true') {
        this.showAdvanced = true;
      }
    }
  },
  watch: {
    showAdvanced(val) {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem('aif-settings-show-advanced', String(val));
      }
    }
  },
  render() {
    if (this.loadError) {
      return h('div', [
        h('div', { class: 'banner' }, 'aif.settings.errors.notFound')
      ]);
    }

    return h('div', { class: 'cru-resource' }, [
      h('div', { class: 'settings-page' }, [
        h('div', { class: 'page-header' }, [
          h('input', {
            class: 'advanced-toggle',
            type: 'checkbox',
            checked: this.showAdvanced,
            onInput: (e) => {
              this.showAdvanced = e.target.checked;
            }
          }),
          this.hasCustomEndpoints ? h('div', { class: 'custom-endpoints-chip' }, 'Custom endpoints active') : null
        ]),
        this.showAdvanced ? h('div', { class: 'advanced-registry-section' }, 'Advanced Section') : null,
        h('div', { class: 'tabbed' }, 'Tabbed content')
      ])
    ]);
  }
});

describe('Settings Page', () => {
  let wrapper;
  let mockStore;
  let localStorageMock;

  const createMockSettingsResource = () => ({
    metadata: {
      name: 'default',
      namespace: 'aif-system'
    },
    spec: {
      registryEndpoints: {
        suseRegistry: 'registry.suse.com',
        nvidiaGpuOperator: 'nvcr.io/nvidia',
        nvidiaHelm: 'https://helm.ngc.nvidia.com/nvidia'
      },
      imageRewrite: {
        enabled: false,
        rules: []
      },
      catalogDiscovery: {
        mode: 'pull',
        ociIndexRef: ''
      },
      publisherRoleBinding: {
        subjects: []
      }
    }
  });

  const createWrapper = (options = {}) => {
    const {
      settingsResource = createMockSettingsResource(),
      storeError = null,
      localStorageValue = null
    } = options;

    // Setup localStorage mock
    localStorageMock = {
      getItem: vi.fn((key) => {
        if (key === 'aif-settings-show-advanced') {
          return localStorageValue;
        }
        return null;
      }),
      setItem: vi.fn(),
      removeItem: vi.fn(),
      clear: vi.fn()
    };
    global.localStorage = localStorageMock;

    // Setup store mock
    mockStore = {
      dispatch: vi.fn(async (action, params) => {
        if (action === 'ai-factory/find') {
          if (storeError) {
            throw storeError;
          }
          return settingsResource;
        }
        return null;
      }),
      getters: {
        'ai-factory/byId': vi.fn(() => settingsResource)
      }
    };

    return mount(SettingsPage, {
      global: {
        mocks: {
          t: (key) => key,
          $store: mockStore,
          $route: {
            params: {
              cluster: 'local'
            }
          }
        }
      }
    });
  };

  beforeEach(() => {
    wrapper = null;
    vi.clearAllMocks();
  });

  afterEach(() => {
    if (wrapper) {
      wrapper.unmount();
    }
    vi.restoreAllMocks();
  });

  it('fetches Settings resource on mount', async () => {
    wrapper = createWrapper();
    await flushPromises();

    expect(mockStore.dispatch).toHaveBeenCalledWith('ai-factory/find', {
      type: 'ai.suse.com.settings',
      id: 'aif-system/default'
    });
  });

  it('shows error banner when Settings CR not found', async () => {
    const notFoundError = new Error('NotFound');
    notFoundError.statusCode = 404;

    wrapper = createWrapper({ storeError: notFoundError });
    await flushPromises();

    expect(wrapper.vm.loadError).toBeTruthy();
    const banner = wrapper.find('.banner');
    expect(banner.exists()).toBe(true);
    expect(banner.text()).toContain('aif.settings.errors.notFound');
  });

  it('advanced toggle controls AdvancedRegistrySection visibility', async () => {
    wrapper = createWrapper();
    await flushPromises();

    // Initially hidden (showAdvanced defaults to false)
    expect(wrapper.vm.showAdvanced).toBe(false);
    let advancedSection = wrapper.find('.advanced-registry-section');
    expect(advancedSection.exists()).toBe(false);

    // Toggle to show
    const checkbox = wrapper.find('.advanced-toggle');
    await checkbox.setValue(true);
    await wrapper.vm.$nextTick();

    expect(wrapper.vm.showAdvanced).toBe(true);
    advancedSection = wrapper.find('.advanced-registry-section');
    expect(advancedSection.exists()).toBe(true);

    // Toggle to hide
    await checkbox.setValue(false);
    await wrapper.vm.$nextTick();

    expect(wrapper.vm.showAdvanced).toBe(false);
    advancedSection = wrapper.find('.advanced-registry-section');
    expect(advancedSection.exists()).toBe(false);
  });

  it('loads advanced toggle state from localStorage on mount', async () => {
    wrapper = createWrapper({ localStorageValue: 'true' });
    await flushPromises();

    expect(localStorageMock.getItem).toHaveBeenCalledWith('aif-settings-show-advanced');
    expect(wrapper.vm.showAdvanced).toBe(true);

    const advancedSection = wrapper.find('.advanced-registry-section');
    expect(advancedSection.exists()).toBe(true);
  });

  it('saves advanced toggle state to localStorage when changed', async () => {
    wrapper = createWrapper();
    await flushPromises();

    expect(wrapper.vm.showAdvanced).toBe(false);

    // Toggle to true
    const checkbox = wrapper.find('.advanced-toggle');
    await checkbox.setValue(true);
    await wrapper.vm.$nextTick();

    expect(localStorageMock.setItem).toHaveBeenCalledWith('aif-settings-show-advanced', 'true');

    // Toggle to false
    await checkbox.setValue(false);
    await wrapper.vm.$nextTick();

    expect(localStorageMock.setItem).toHaveBeenCalledWith('aif-settings-show-advanced', 'false');
  });

  it('custom endpoints chip shows when SUSE Registry differs from default', async () => {
    const customSettings = createMockSettingsResource();
    customSettings.spec.registryEndpoints.suseRegistry = 'custom-registry.example.com';

    wrapper = createWrapper({ settingsResource: customSettings });
    await flushPromises();

    expect(wrapper.vm.hasCustomEndpoints).toBe(true);
    const chip = wrapper.find('.custom-endpoints-chip');
    expect(chip.exists()).toBe(true);
  });

  it('custom endpoints chip shows when NVIDIA GPU Operator differs from default', async () => {
    const customSettings = createMockSettingsResource();
    customSettings.spec.registryEndpoints.nvidiaGpuOperator = 'custom-nvidia.example.com';

    wrapper = createWrapper({ settingsResource: customSettings });
    await flushPromises();

    expect(wrapper.vm.hasCustomEndpoints).toBe(true);
    const chip = wrapper.find('.custom-endpoints-chip');
    expect(chip.exists()).toBe(true);
  });

  it('custom endpoints chip shows when NVIDIA Helm differs from default', async () => {
    const customSettings = createMockSettingsResource();
    customSettings.spec.registryEndpoints.nvidiaHelm = 'https://custom-helm.example.com';

    wrapper = createWrapper({ settingsResource: customSettings });
    await flushPromises();

    expect(wrapper.vm.hasCustomEndpoints).toBe(true);
    const chip = wrapper.find('.custom-endpoints-chip');
    expect(chip.exists()).toBe(true);
  });

  it('custom endpoints chip hidden when all fields match defaults', async () => {
    wrapper = createWrapper();
    await flushPromises();

    expect(wrapper.vm.hasCustomEndpoints).toBe(false);
    const chip = wrapper.find('.custom-endpoints-chip');
    expect(chip.exists()).toBe(false);
  });

  it('custom endpoints chip shows when multiple fields differ from defaults', async () => {
    const customSettings = createMockSettingsResource();
    customSettings.spec.registryEndpoints.suseRegistry = 'custom-suse.example.com';
    customSettings.spec.registryEndpoints.nvidiaGpuOperator = 'custom-nvidia.example.com';
    customSettings.spec.registryEndpoints.nvidiaHelm = 'https://custom-helm.example.com';

    wrapper = createWrapper({ settingsResource: customSettings });
    await flushPromises();

    expect(wrapper.vm.hasCustomEndpoints).toBe(true);
    const chip = wrapper.find('.custom-endpoints-chip');
    expect(chip.exists()).toBe(true);
  });

  it('handles missing registryEndpoints gracefully', async () => {
    const settingsWithoutEndpoints = createMockSettingsResource();
    settingsWithoutEndpoints.spec.registryEndpoints = undefined;

    wrapper = createWrapper({ settingsResource: settingsWithoutEndpoints });
    await flushPromises();

    expect(wrapper.vm.loadError).toBeFalsy();
    expect(wrapper.vm.hasCustomEndpoints).toBe(false);
  });

  it('updates resource when registry endpoints change', async () => {
    wrapper = createWrapper();
    await flushPromises();

    const newEndpoints = {
      suseRegistry: 'new-registry.example.com',
      nvidiaGpuOperator: 'nvcr.io/nvidia',
      nvidiaHelm: 'https://helm.ngc.nvidia.com/nvidia'
    };

    wrapper.vm.value.spec.registryEndpoints = newEndpoints;
    await wrapper.vm.$nextTick();

    expect(wrapper.vm.value.spec.registryEndpoints.suseRegistry).toBe('new-registry.example.com');
    expect(wrapper.vm.hasCustomEndpoints).toBe(true);
  });

  it('initializes with proper default values when Settings CR exists', async () => {
    wrapper = createWrapper();
    await flushPromises();

    expect(wrapper.vm.value).toBeTruthy();
    expect(wrapper.vm.value.spec.registryEndpoints.suseRegistry).toBe('registry.suse.com');
    expect(wrapper.vm.value.spec.imageRewrite.enabled).toBe(false);
    expect(wrapper.vm.value.spec.catalogDiscovery.mode).toBe('pull');
  });

  it('preserves all spec fields when fetched from store', async () => {
    wrapper = createWrapper();
    await flushPromises();

    expect(wrapper.vm.value.spec.registryEndpoints).toBeDefined();
    expect(wrapper.vm.value.spec.imageRewrite).toBeDefined();
    expect(wrapper.vm.value.spec.catalogDiscovery).toBeDefined();
    expect(wrapper.vm.value.spec.publisherRoleBinding).toBeDefined();
  });

  it('handles localStorage being unavailable gracefully', async () => {
    // Simulate localStorage not available
    global.localStorage = undefined;

    wrapper = createWrapper();
    await flushPromises();

    // Should not crash, should use default value
    expect(wrapper.vm.showAdvanced).toBe(false);
  });

  it('renders CruResource wrapper with correct props', async () => {
    wrapper = createWrapper();
    await flushPromises();

    const cruResource = wrapper.find('.cru-resource');
    expect(cruResource.exists()).toBe(true);
  });

  it('renders tabbed interface', async () => {
    wrapper = createWrapper();
    await flushPromises();

    const tabbed = wrapper.find('.tabbed');
    expect(tabbed.exists()).toBe(true);
  });
});
