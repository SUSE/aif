/**
 * Documentation Links Configuration
 * Following standard patterns for documentation management
 * Centralizes all documentation and help links
 */

// === Base Documentation URLs ===
export const DOC_BASE_URLS = {
  SUSE: 'https://documentation.suse.com',
  RANCHER: 'https://rancher.com/docs',
  KUBERNETES: 'https://kubernetes.io/docs',
  HELM: 'https://helm.sh/docs',
  GITHUB: 'https://github.com/suse/suse-ai-lifecycle-manager'
} as const;

// === Documentation Categories ===
export const DOC_CATEGORIES = {
  GETTING_STARTED: 'getting-started',
  USER_GUIDE: 'user-guide',
  ADMIN_GUIDE: 'admin-guide',
  API_REFERENCE: 'api-reference',
  TROUBLESHOOTING: 'troubleshooting',
  FAQ: 'faq',
  TUTORIALS: 'tutorials',
  BEST_PRACTICES: 'best-practices'
} as const;

export type DocCategory = typeof DOC_CATEGORIES[keyof typeof DOC_CATEGORIES];

// === Documentation Link Interface ===
export interface DocLink {
  id: string;
  title: string;
  description?: string;
  url: string;
  category: DocCategory;
  tags: string[];
  external?: boolean;
  version?: string;
  priority?: number;
}

// === Main Documentation Links ===
export const DOCUMENTATION_LINKS: Record<string, DocLink> = {
  // Getting Started
  overview: {
    id: 'overview',
    title: 'SUSE AI Extension Overview',
    description: 'Introduction to SUSE AI Extension for Rancher',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/overview`,
    category: DOC_CATEGORIES.GETTING_STARTED,
    tags: ['overview', 'introduction'],
    priority: 1
  },

  installation: {
    id: 'installation',
    title: 'Installation Guide',
    description: 'How to install and configure SUSE AI Extension',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/installation`,
    category: DOC_CATEGORIES.GETTING_STARTED,
    tags: ['installation', 'setup', 'configuration'],
    priority: 2
  },

  quickStart: {
    id: 'quick-start',
    title: 'Quick Start Guide',
    description: 'Get started with SUSE AI Extension in 5 minutes',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/quick-start`,
    category: DOC_CATEGORIES.GETTING_STARTED,
    tags: ['quick-start', 'tutorial'],
    priority: 3
  },

  // User Guide
  managingApps: {
    id: 'managing-apps',
    title: 'Managing Applications',
    description: 'How to install, upgrade, and uninstall applications',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/user-guide/managing-apps`,
    category: DOC_CATEGORIES.USER_GUIDE,
    tags: ['apps', 'install', 'upgrade', 'uninstall'],
    priority: 1
  },

  repositories: {
    id: 'repositories',
    title: 'Working with Repositories',
    description: 'How to add and manage chart repositories',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/user-guide/repositories`,
    category: DOC_CATEGORIES.USER_GUIDE,
    tags: ['repositories', 'helm', 'charts'],
    priority: 2
  },

  multiCluster: {
    id: 'multi-cluster',
    title: 'Multi-Cluster Operations',
    description: 'Managing applications across multiple clusters',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/user-guide/multi-cluster`,
    category: DOC_CATEGORIES.USER_GUIDE,
    tags: ['multi-cluster', 'clusters'],
    priority: 3
  },

  bulkOperations: {
    id: 'bulk-operations',
    title: 'Bulk Operations',
    description: 'Performing operations on multiple applications at once',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/user-guide/bulk-operations`,
    category: DOC_CATEGORIES.USER_GUIDE,
    tags: ['bulk', 'operations', 'efficiency'],
    priority: 4
  },

  // Admin Guide
  configuration: {
    id: 'configuration',
    title: 'Configuration Options',
    description: 'Advanced configuration and settings',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/admin-guide/configuration`,
    category: DOC_CATEGORIES.ADMIN_GUIDE,
    tags: ['configuration', 'settings', 'admin'],
    priority: 1
  },

  security: {
    id: 'security',
    title: 'Security Considerations',
    description: 'Security best practices and RBAC configuration',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/admin-guide/security`,
    category: DOC_CATEGORIES.ADMIN_GUIDE,
    tags: ['security', 'rbac', 'permissions'],
    priority: 2
  },

  monitoring: {
    id: 'monitoring',
    title: 'Monitoring and Alerting',
    description: 'Setting up monitoring and alerts for applications',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/admin-guide/monitoring`,
    category: DOC_CATEGORIES.ADMIN_GUIDE,
    tags: ['monitoring', 'alerts', 'health'],
    priority: 3
  },

  // Troubleshooting
  commonIssues: {
    id: 'common-issues',
    title: 'Common Issues',
    description: 'Solutions to frequently encountered problems',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/troubleshooting/common-issues`,
    category: DOC_CATEGORIES.TROUBLESHOOTING,
    tags: ['troubleshooting', 'issues', 'problems'],
    priority: 1
  },

  diagnostics: {
    id: 'diagnostics',
    title: 'Diagnostic Tools',
    description: 'Tools and commands for diagnosing issues',
    url: `${DOC_BASE_URLS.SUSE}/suse-ai-lifecycle-manager/troubleshooting/diagnostics`,
    category: DOC_CATEGORIES.TROUBLESHOOTING,
    tags: ['diagnostics', 'debugging', 'tools'],
    priority: 2
  },

  // External Links
  rancherDocs: {
    id: 'rancher-docs',
    title: 'Rancher Documentation',
    description: 'Official Rancher documentation',
    url: `${DOC_BASE_URLS.RANCHER}`,
    category: DOC_CATEGORIES.API_REFERENCE,
    tags: ['rancher', 'external'],
    external: true,
    priority: 1
  },

  kubernetesDocs: {
    id: 'kubernetes-docs',
    title: 'Kubernetes Documentation',
    description: 'Official Kubernetes documentation',
    url: `${DOC_BASE_URLS.KUBERNETES}`,
    category: DOC_CATEGORIES.API_REFERENCE,
    tags: ['kubernetes', 'external'],
    external: true,
    priority: 2
  },

  helmDocs: {
    id: 'helm-docs',
    title: 'Helm Documentation',
    description: 'Official Helm documentation',
    url: `${DOC_BASE_URLS.HELM}`,
    category: DOC_CATEGORIES.API_REFERENCE,
    tags: ['helm', 'external'],
    external: true,
    priority: 3
  },

  githubRepo: {
    id: 'github-repo',
    title: 'GitHub Repository',
    description: 'Source code and issue tracking',
    url: `${DOC_BASE_URLS.GITHUB}`,
    category: DOC_CATEGORIES.API_REFERENCE,
    tags: ['github', 'source', 'issues', 'external'],
    external: true,
    priority: 4
  }
};

// === Context-Specific Link Groups ===
export const CONTEXTUAL_LINKS = {
  apps: {
    title: 'Application Management',
    links: ['managingApps', 'repositories', 'bulkOperations']
  },
  install: {
    title: 'Installation Help',
    links: ['managingApps', 'repositories', 'configuration']
  },
  manage: {
    title: 'Management Help',
    links: ['managingApps', 'multiCluster', 'monitoring']
  },
  repositories: {
    title: 'Repository Management',
    links: ['repositories', 'configuration', 'security']
  },
  troubleshooting: {
    title: 'Troubleshooting',
    links: ['commonIssues', 'diagnostics', 'githubRepo']
  }
};

// === Support Links ===
export const SUPPORT_LINKS = {
  community: {
    title: 'Community Forum',
    description: 'Get help from the community',
    url: 'https://community.suse.com',
    icon: 'forum'
  },
  support: {
    title: 'Enterprise Support',
    description: 'Contact SUSE support team',
    url: 'https://www.suse.com/support',
    icon: 'support'
  },
  feedback: {
    title: 'Feedback',
    description: 'Share your feedback and suggestions',
    url: `${DOC_BASE_URLS.GITHUB}/issues/new?template=feedback.md`,
    icon: 'feedback'
  },
  bugReport: {
    title: 'Report a Bug',
    description: 'Report issues and bugs',
    url: `${DOC_BASE_URLS.GITHUB}/issues/new?template=bug_report.md`,
    icon: 'bug'
  }
};

// === Helper Functions ===

/**
 * Get documentation links by category
 */
export function getLinksByCategory(category: DocCategory): DocLink[] {
  return Object.values(DOCUMENTATION_LINKS)
    .filter(link => link.category === category)
    .sort((a, b) => (a.priority || 999) - (b.priority || 999));
}

/**
 * Get contextual links for a page
 */
export function getContextualLinks(context: keyof typeof CONTEXTUAL_LINKS): DocLink[] {
  const contextConfig = CONTEXTUAL_LINKS[context];
  if (!contextConfig) return [];

  return contextConfig.links
    .map(linkId => DOCUMENTATION_LINKS[linkId])
    .filter(Boolean)
    .sort((a, b) => (a.priority || 999) - (b.priority || 999));
}

/**
 * Search documentation links
 */
export function searchDocLinks(query: string): DocLink[] {
  const searchTerm = query.toLowerCase();
  
  return Object.values(DOCUMENTATION_LINKS).filter(link => 
    link.title.toLowerCase().includes(searchTerm) ||
    link.description?.toLowerCase().includes(searchTerm) ||
    link.tags.some(tag => tag.toLowerCase().includes(searchTerm))
  );
}

/**
 * Get documentation link by ID
 */
export function getDocLink(id: string): DocLink | null {
  return DOCUMENTATION_LINKS[id] || null;
}

/**
 * Get all categories with link counts
 */
export function getCategoriesWithCounts(): Array<{ category: DocCategory; count: number; name: string }> {
  const counts: Record<DocCategory, number> = {} as any;
  
  Object.values(DOCUMENTATION_LINKS).forEach(link => {
    counts[link.category] = (counts[link.category] || 0) + 1;
  });

  const categoryNames: Record<DocCategory, string> = {
    [DOC_CATEGORIES.GETTING_STARTED]: 'Getting Started',
    [DOC_CATEGORIES.USER_GUIDE]: 'User Guide',
    [DOC_CATEGORIES.ADMIN_GUIDE]: 'Admin Guide',
    [DOC_CATEGORIES.API_REFERENCE]: 'Reference',
    [DOC_CATEGORIES.TROUBLESHOOTING]: 'Troubleshooting',
    [DOC_CATEGORIES.FAQ]: 'FAQ',
    [DOC_CATEGORIES.TUTORIALS]: 'Tutorials',
    [DOC_CATEGORIES.BEST_PRACTICES]: 'Best Practices'
  };

  return Object.entries(counts).map(([category, count]) => ({
    category: category as DocCategory,
    count,
    name: categoryNames[category as DocCategory]
  }));
}

export default {
  DOCUMENTATION_LINKS,
  CONTEXTUAL_LINKS,
  SUPPORT_LINKS,
  DOC_CATEGORIES,
  getLinksByCategory,
  getContextualLinks,
  searchDocLinks,
  getDocLink
};