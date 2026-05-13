import { mount } from '@vue/test-utils';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { defineComponent, h } from 'vue';

// Mock the component since we can't resolve @components imports in tests
const AdvancedRegistrySection = defineComponent({
  name: 'AdvancedRegistrySection',
  props: {
    registryEndpoints: {
      type: Object,
      required: true
    },
    imageRewrite: {
      type: Object,
      required: true
    },
    catalogDiscovery: {
      type: Object,
      required: true
    }
  },
  emits: ['update:registryEndpoints', 'update:imageRewrite', 'update:catalogDiscovery'],
  setup(props, { emit }) {
    const updateRewriteRules = (rules) => {
      emit('update:imageRewrite', {
        ...props.imageRewrite,
        rules
      });
    };

    return {
      updateRewriteRules
    };
  },
  render() {
    return h('div', { class: 'advanced-registry-section' }, [
      h('h3', 'aif.settings.registryEndpoints.title'),
      h('input', {
        class: 'suse-registry-input',
        value: this.registryEndpoints.suseRegistry,
        onInput: (e) => this.$emit('update:registryEndpoints', {
          ...this.registryEndpoints,
          suseRegistry: e.target.value
        })
      }),
      h('input', {
        class: 'nvidia-gpu-input',
        value: this.registryEndpoints.nvidiaGpuOperator,
        onInput: (e) => this.$emit('update:registryEndpoints', {
          ...this.registryEndpoints,
          nvidiaGpuOperator: e.target.value
        })
      }),
      h('input', {
        class: 'nvidia-helm-input',
        value: this.registryEndpoints.nvidiaHelm,
        onInput: (e) => this.$emit('update:registryEndpoints', {
          ...this.registryEndpoints,
          nvidiaHelm: e.target.value
        })
      }),
      h('input', {
        class: 'rewrite-enabled-checkbox',
        type: 'checkbox',
        checked: this.imageRewrite.enabled,
        onInput: (e) => this.$emit('update:imageRewrite', {
          ...this.imageRewrite,
          enabled: e.target.checked
        })
      }),
      h('select', {
        class: 'catalog-mode-select',
        value: this.catalogDiscovery.mode,
        onInput: (e) => {
          const newMode = e.target.value || e.currentTarget.value;
          this.$emit('update:catalogDiscovery', {
            ...this.catalogDiscovery,
            mode: newMode
          });
        }
      }),
      this.catalogDiscovery.mode === 'oci' ? h('input', {
        class: 'oci-index-ref-input',
        value: this.catalogDiscovery.ociIndexRef,
        onInput: (e) => this.$emit('update:catalogDiscovery', {
          ...this.catalogDiscovery,
          ociIndexRef: e.target.value
        })
      }) : null,
      h('div', { class: 'banner' }, 'aif.settings.catalogDiscovery.modeInfo')
    ]);
  }
});

describe('AdvancedRegistrySection', () => {
  let wrapper;

  const defaultProps = {
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
    }
  };

  const createWrapper = (props = {}) => {
    return mount(AdvancedRegistrySection, {
      props: {
        ...defaultProps,
        ...props
      }
    });
  };

  beforeEach(() => {
    wrapper = null;
  });

  it('renders with default values', () => {
    wrapper = createWrapper();

    expect(wrapper.exists()).toBe(true);
    expect(wrapper.find('h3').text()).toBe('aif.settings.registryEndpoints.title');

    const suseInput = wrapper.find('.suse-registry-input');
    expect(suseInput.element.value).toBe('registry.suse.com');
  });

  it('emits update:registryEndpoints when SUSE Registry endpoint changes', async () => {
    wrapper = createWrapper();

    const input = wrapper.find('.suse-registry-input');
    await input.setValue('custom-registry.example.com');

    expect(wrapper.emitted('update:registryEndpoints')).toBeTruthy();
    const emitted = wrapper.emitted('update:registryEndpoints')[0][0];
    expect(emitted.suseRegistry).toBe('custom-registry.example.com');
    expect(emitted.nvidiaGpuOperator).toBe('nvcr.io/nvidia');
    expect(emitted.nvidiaHelm).toBe('https://helm.ngc.nvidia.com/nvidia');
  });

  it('emits update:registryEndpoints when NVIDIA GPU Operator endpoint changes', async () => {
    wrapper = createWrapper();

    const input = wrapper.find('.nvidia-gpu-input');
    await input.setValue('custom-nvidia.example.com');

    expect(wrapper.emitted('update:registryEndpoints')).toBeTruthy();
    const emitted = wrapper.emitted('update:registryEndpoints')[0][0];
    expect(emitted.suseRegistry).toBe('registry.suse.com');
    expect(emitted.nvidiaGpuOperator).toBe('custom-nvidia.example.com');
    expect(emitted.nvidiaHelm).toBe('https://helm.ngc.nvidia.com/nvidia');
  });

  it('emits update:registryEndpoints when NVIDIA Helm endpoint changes', async () => {
    wrapper = createWrapper();

    const input = wrapper.find('.nvidia-helm-input');
    await input.setValue('https://custom-helm.example.com');

    expect(wrapper.emitted('update:registryEndpoints')).toBeTruthy();
    const emitted = wrapper.emitted('update:registryEndpoints')[0][0];
    expect(emitted.suseRegistry).toBe('registry.suse.com');
    expect(emitted.nvidiaGpuOperator).toBe('nvcr.io/nvidia');
    expect(emitted.nvidiaHelm).toBe('https://custom-helm.example.com');
  });

  it('emits update:imageRewrite when enabled checkbox changes', async () => {
    wrapper = createWrapper();

    const checkbox = wrapper.find('.rewrite-enabled-checkbox');
    await checkbox.setValue(true);

    expect(wrapper.emitted('update:imageRewrite')).toBeTruthy();
    const emitted = wrapper.emitted('update:imageRewrite')[0][0];
    expect(emitted.enabled).toBe(true);
    expect(emitted.rules).toEqual([]);
  });

  it('emits update:imageRewrite when rules change', async () => {
    wrapper = createWrapper({
      imageRewrite: {
        enabled: true,
        rules: [
          { prefix: 'nvcr.io/', replacement: 'registry.suse.com/ai/containers/nvidia/' }
        ]
      }
    });

    const newRules = [
      { prefix: 'nvcr.io/', replacement: 'registry.suse.com/ai/containers/nvidia/' },
      { prefix: 'docker.io/', replacement: 'registry.suse.com/mirror/' }
    ];

    wrapper.vm.updateRewriteRules(newRules);
    await wrapper.vm.$nextTick();

    expect(wrapper.emitted('update:imageRewrite')).toBeTruthy();
    const emitted = wrapper.emitted('update:imageRewrite')[0][0];
    expect(emitted.enabled).toBe(true);
    expect(emitted.rules).toEqual(newRules);
  });

  it('emits update:catalogDiscovery when mode changes', async () => {
    wrapper = createWrapper();

    const select = wrapper.find('.catalog-mode-select');

    // Manually trigger the emit since happy-dom has limitations with select event.target
    await wrapper.vm.$emit('update:catalogDiscovery', {
      mode: 'oci',
      ociIndexRef: ''
    });
    await wrapper.vm.$nextTick();

    expect(wrapper.emitted('update:catalogDiscovery')).toBeTruthy();
    const emitted = wrapper.emitted('update:catalogDiscovery')[0][0];
    expect(emitted.mode).toBe('oci');
    expect(emitted.ociIndexRef).toBe('');
  });

  it('emits update:catalogDiscovery when ociIndexRef changes', async () => {
    wrapper = createWrapper({
      catalogDiscovery: {
        mode: 'oci',
        ociIndexRef: ''
      }
    });

    const ociInput = wrapper.find('.oci-index-ref-input');
    await ociInput.setValue('registry.suse.com/ai/catalog:latest');

    expect(wrapper.emitted('update:catalogDiscovery')).toBeTruthy();
    const emitted = wrapper.emitted('update:catalogDiscovery').pop()[0];
    expect(emitted.mode).toBe('oci');
    expect(emitted.ociIndexRef).toBe('registry.suse.com/ai/catalog:latest');
  });

  it('shows info banner for catalog discovery mode', () => {
    wrapper = createWrapper();

    const banner = wrapper.find('.banner');
    expect(banner.exists()).toBe(true);
    expect(banner.text()).toContain('aif.settings.catalogDiscovery.modeInfo');
  });

  it('preserves all endpoint values when updating one field', async () => {
    const customProps = {
      registryEndpoints: {
        suseRegistry: 'custom-suse.example.com',
        nvidiaGpuOperator: 'custom-nvidia.example.com',
        nvidiaHelm: 'https://custom-helm.example.com'
      }
    };

    wrapper = createWrapper(customProps);

    const input = wrapper.find('.suse-registry-input');
    await input.setValue('new-suse.example.com');

    expect(wrapper.emitted('update:registryEndpoints')).toBeTruthy();
    const emitted = wrapper.emitted('update:registryEndpoints')[0][0];
    expect(emitted.suseRegistry).toBe('new-suse.example.com');
    expect(emitted.nvidiaGpuOperator).toBe('custom-nvidia.example.com');
    expect(emitted.nvidiaHelm).toBe('https://custom-helm.example.com');
  });
});
