// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount } from '@vue/test-utils';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import yaml from 'js-yaml';
import AppDetailPanel from '../AppDetailPanel.vue';
import type { AppCollectionItem } from '../../services/app-collection';

vi.mock('@shell/components/Markdown.vue', () => ({
  default: {
    props:    ['value'],
    template: '<div class="md">{{ value }}</div>',
  },
}));

const translations = yaml.load(readFileSync(path.resolve(__dirname, '../../l10n/en-us.yaml'), 'utf8'));

function translate(key: string, args: Record<string, string> = {}): string {
  if (!key.startsWith('suseai.')) return key;
  const value = key.split('.').reduce<any>((current, part) => current?.[part], translations);

  if (typeof value !== 'string') throw new Error(`Missing translation: ${ key }`);

  return value.replace(/\{(\w+)\}/g, (_match, name) => args[name] ?? '');
}

function app(overrides: Partial<AppCollectionItem> = {}): AppCollectionItem {
  return {
    name:      'Boltz2 NIM',
    slug_name: 'boltz2-nim',
    ...overrides,
  };
}

const settingsLink = { name: 'c-cluster-suseai-settings', params: { cluster: 'local' } };

const mounted: ReturnType<typeof mount>[] = [];

afterEach(() => mounted.splice(0).forEach((wrapper) => wrapper.unmount()));

function setup(props: Partial<InstanceType<typeof AppDetailPanel>['$props']> = {}) {
  const store = { getters: { 'i18n/t': translate } };
  const wrapper = mount(AppDetailPanel, {
    props: {
      app:                 app(),
      unavailable:         false,
      availabilityMessage: '',
      settingsLink,
      ...props,
    },
    global: {
      mocks: { $store: store },
      stubs: {
        AppLabels:  true,
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  });

  mounted.push(wrapper);

  return { wrapper };
}

describe('AppDetailPanel', () => {
  it('separates the header from the sections with a divider', () => {
    const { wrapper } = setup({ app: app({ description: 'D' }) });

    expect(wrapper.find('.app-detail-header + hr.app-detail-divider').exists()).toBe(true);
  });

  it('shows the short description and facts under an Overview heading', () => {
    const { wrapper } = setup({ app: app({ description: 'D', repository_name: 'my-repo' }) });
    const overview = wrapper.get('[data-testid="app-detail-overview"]');

    expect(overview.get('h4').text()).toBe('Overview');
    expect(overview.get('.app-detail-summary').text()).toBe('D');
    expect(overview.get('.app-detail-facts').text()).toContain('my-repo');
  });

  it('renders the full description as markdown under a Description heading', () => {
    const { wrapper } = setup({ app: app({ description: 'D', overview: '## O' }) });
    const description = wrapper.get('[data-testid="app-detail-description"]');

    expect(description.get('h4').text()).toBe('Description');
    expect(description.get('.md').text()).toBe('## O');
    expect(wrapper.get('.app-detail-summary').text()).toBe('D');
  });

  it('omits the Description section when there is no full description', () => {
    const { wrapper } = setup({ app: app({ description: 'D' }) });

    expect(wrapper.find('[data-testid="app-detail-description"]').exists()).toBe(false);
    expect(wrapper.find('.md').exists()).toBe(false);
  });

  it('omits the summary line when only the full description exists', () => {
    const { wrapper } = setup({ app: app({ overview: '## O' }) });

    expect(wrapper.find('.app-detail-summary').exists()).toBe(false);
  });

  it('falls back to an em dash summary when there is neither description nor full description', () => {
    const { wrapper } = setup({ app: app() });

    expect(wrapper.get('.app-detail-summary').text()).toBe('—');
  });

  it('no longer shows latest-version or last-updated facts, even when the app has them', () => {
    const { wrapper } = setup({ app: app({ latest_version: '9.9.9', last_updated_at: '2025-07-30T12:00:00Z' }) });

    expect(wrapper.get('[data-testid="app-detail-overview"]').text()).not.toContain('9.9.9');
    expect(wrapper.get('[data-testid="app-detail-overview"]').text()).not.toContain('Jul 30, 2025');
  });

  describe('versions', () => {
    const versions = (n: number) => Array.from({ length: n }, (_v, i) => ({ version: `1.0.${ n - 1 - i }` }));

    it('lists each version verbatim with its date in the format Rancher uses for charts', () => {
      // Midday UTC keeps the calendar date stable in any test-runner timezone.
      const { wrapper } = setup({
        versions: [
          { version: 'v2.6.0', created: '2025-07-30T12:00:00Z' },
          { version: '2.5.0', created: 'not-a-date' },
          { version: '2.4.0', created: '0001-01-01T00:00:00Z' },
        ],
      });
      const section = wrapper.get('[data-testid="app-detail-versions"]');
      const rows = section.findAll('[data-testid="app-detail-version"]');

      expect(section.get('h4').text()).toBe('Versions');
      expect(rows.map((r) => r.get('.app-detail-version-number').text())).toEqual(['v2.6.0', '2.5.0', '2.4.0']);
      expect(rows[0].text()).toContain('Jul 30, 2025');
      expect(rows[1].find('.app-detail-version-date').exists()).toBe(false);
      expect(rows[2].find('.app-detail-version-date').exists()).toBe(false);
      expect(section.text()).not.toContain('vv2.6.0');
    });

    it('places the Versions section after the Description section', () => {
      const { wrapper } = setup({ app: app({ overview: '## O' }), versions: versions(1) });
      const html = wrapper.html();

      expect(html.indexOf('app-detail-description')).toBeLessThan(html.indexOf('app-detail-versions'));
    });

    it('emits select-version with the clicked version', async () => {
      const { wrapper } = setup({ versions: versions(2) });

      await wrapper.findAll('[data-testid="app-detail-version"]')[1].trigger('click');

      expect(wrapper.emitted('select-version')).toEqual([['1.0.0']]);
    });

    it('shows the newest ten and toggles the rest', async () => {
      const { wrapper } = setup({ versions: versions(12) });
      const rows = () => wrapper.findAll('[data-testid="app-detail-version"]');
      const toggle = () => wrapper.get('[data-testid="app-detail-versions-toggle"]');

      expect(rows()).toHaveLength(10);
      expect(toggle().text()).toBe('Show all 12 versions');

      await toggle().trigger('click');
      expect(rows()).toHaveLength(12);
      expect(toggle().text()).toBe('Show fewer');
    });

    it('has no toggle for ten versions or fewer', () => {
      const { wrapper } = setup({ versions: versions(10) });

      expect(wrapper.find('[data-testid="app-detail-versions-toggle"]').exists()).toBe(false);
    });

    it('shows a loading state while versions load', () => {
      const { wrapper } = setup({ versionsLoading: true });

      expect(wrapper.get('[data-testid="app-detail-versions"]').find('.app-detail-versions-loading i.icon-spinner').exists()).toBe(true);
    });

    it('says versions are unavailable when loading failed or none were found', () => {
      expect(setup({ versionsError: true }).wrapper.get('[data-testid="app-detail-versions"]').text()).toContain('Versions unavailable');
      expect(setup({ versions: [] }).wrapper.get('[data-testid="app-detail-versions"]').text()).toContain('Versions unavailable');
    });

    it('hides the Versions section when nothing was requested or the app is unavailable', () => {
      expect(setup().wrapper.find('[data-testid="app-detail-versions"]').exists()).toBe(false);
      expect(setup({ unavailable: true, versions: versions(2) }).wrapper.find('[data-testid="app-detail-versions"]').exists()).toBe(false);
    });
  });

  it('shows a View on NGC link for an NGC repository URL', () => {
    const { wrapper } = setup({
      app: app({ slug_name: 'k8s-nim-operator', repository_url: 'https://helm.ngc.nvidia.com/nvidia' }),
    });
    const link = wrapper.findAll('a').find((a) => a.text() === translate('suseai.apps.detail.viewOnNgc'));

    expect(link?.attributes('href')).toBe('https://catalog.ngc.nvidia.com/orgs/nvidia/-/helm-charts/k8s-nim-operator/-');
    expect(link?.attributes('target')).toBe('_blank');
    expect(link?.attributes('rel')).toBe('noopener noreferrer');
  });

  it('hides the View on NGC link for a non-NGC repository URL', () => {
    const { wrapper } = setup({ app: app({ repository_url: 'https://charts.example.com' }) });
    const link = wrapper.findAll('a').find((a) => a.text() === translate('suseai.apps.detail.viewOnNgc'));

    expect(link).toBeUndefined();
  });

  it('places each link icon before its text', () => {
    const { wrapper } = setup({
      app: app({
        documentation_url: 'https://docs.example.com',
        project_url:       'https://project.example.com',
        repository_url:    'https://helm.ngc.nvidia.com/nvidia',
      }),
    });
    const links = wrapper.findAll('.app-detail-links a');

    expect(links.map((a) => a.text())).toEqual(['Documentation', 'Project page', 'View on NGC']);
    links.forEach((a) => expect(a.element.firstElementChild?.matches('i.icon')).toBe(true));
    links.forEach((a) => expect(a.element.lastChild?.nodeType).toBe(Node.TEXT_NODE));
  });

  it('shows documentation and project links only when present', () => {
    const { wrapper } = setup({ app: app({ documentation_url: 'https://docs.example.com' }) });

    const docLink = wrapper.findAll('a').find((a) => a.text() === translate('suseai.apps.detail.documentation'));
    expect(docLink?.attributes('href')).toBe('https://docs.example.com');
    expect(docLink?.attributes('target')).toBe('_blank');
    expect(docLink?.attributes('rel')).toBe('noopener noreferrer');
    expect(docLink?.find('i.icon.icon-document').exists()).toBe(true);
    expect(docLink?.find('i.icon.icon-external-link').exists()).toBe(false);

    const projectLink = wrapper.findAll('a').find((a) => a.text() === translate('suseai.apps.detail.project'));
    expect(projectLink).toBeUndefined();
  });

  it('shows the repository fact as repository_name, falling back to repository_url', () => {
    const { wrapper: byName } = setup({ app: app({ repository_name: 'application-collection' }) });
    expect(byName.text()).toContain('application-collection');

    const { wrapper: byUrl } = setup({ app: app({ repository_url: 'oci://dp.apps.rancher.io/charts' }) });
    expect(byUrl.text()).toContain('oci://dp.apps.rancher.io/charts');
  });

  it('shows the availability message and settings link when unavailable', () => {
    const { wrapper } = setup({ unavailable: true, availabilityMessage: 'Repository unavailable' });

    expect(wrapper.text()).toContain('Repository unavailable');
    expect(wrapper.find('a[href]').exists()).toBe(true);
  });

  it('has no Install button, matching the Blueprints drawer', () => {
    const { wrapper } = setup({ app: app({ description: 'D' }) });

    expect(wrapper.find('[data-testid="app-detail-install"]').exists()).toBe(false);
    expect(wrapper.findAll('button').map((b) => b.attributes('data-testid'))).toEqual(['app-detail-close']);
  });

  it('emits close from the close button', async () => {
    const { wrapper } = setup();

    await wrapper.get('[data-testid="app-detail-close"]').trigger('click');
    expect(wrapper.emitted('close')).toHaveLength(1);
  });
});
