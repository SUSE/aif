export interface SecretRef { name: string; key: string }

export type CustomRepoType = 'helm' | 'oci' | 'git';

export interface CustomRepoForm {
  name: string;
  displayName: string;
  type: CustomRepoType;
  url: string;
  gitRepo: string;
  gitBranch: string;
  userSecretRef: SecretRef | null;
  tokenSecretRef: SecretRef | null;
  sshKeySecretRef: SecretRef | null;
  caBundleSecretRef: SecretRef | null;
  insecureSkipTLSVerify: boolean;
}

// Mirror of operator/internal/credentials canonical repo names.
export const RESERVED_REPO_NAMES = ['application-collection', 'suse-ai-registry', 'nvidia', 'nvidia-blueprints'];

const DNS1123 = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;
const NAME_MAX = 56;

export function emptyCustomRepo(): CustomRepoForm {
  return {
    name: '', displayName: '', type: 'helm', url: '', gitRepo: '', gitBranch: '',
    userSecretRef: null, tokenSecretRef: null, sshKeySecretRef: null, caBundleSecretRef: null,
    insecureSkipTLSVerify: false,
  };
}

const refOrNull = (r: SecretRef | null) => (r && r.name && r.key ? { name: r.name, key: r.key } : null);

export function buildCustomReposCrd(forms: CustomRepoForm[]): any[] {
  return forms.map((f) => {
    const out: any = { name: f.name, type: f.type };
    if (f.displayName) out.displayName = f.displayName;
    if (f.type === 'git') {
      out.gitRepo = f.gitRepo;
      out.gitBranch = f.gitBranch;
    } else {
      out.url = f.url;
    }
    const user = refOrNull(f.userSecretRef);
    const token = refOrNull(f.tokenSecretRef);
    const ssh = refOrNull(f.sshKeySecretRef);
    const ca = refOrNull(f.caBundleSecretRef);
    if (user) out.userSecretRef = user;
    if (token) out.tokenSecretRef = token;
    if (ssh) out.sshKeySecretRef = ssh;
    if (ca) out.caBundleSecretRef = ca;
    if (f.insecureSkipTLSVerify) out.insecureSkipTLSVerify = true;
    return out;
  });
}

export function buildCustomReposForm(crd: any[] = []): CustomRepoForm[] {
  return (crd || []).map((c) => ({
    ...emptyCustomRepo(),
    name: c.name || '',
    displayName: c.displayName || '',
    type: (c.type as CustomRepoType) || 'helm',
    url: c.url || '',
    gitRepo: c.gitRepo || '',
    gitBranch: c.gitBranch || '',
    userSecretRef: c.userSecretRef || null,
    tokenSecretRef: c.tokenSecretRef || null,
    sshKeySecretRef: c.sshKeySecretRef || null,
    caBundleSecretRef: c.caBundleSecretRef || null,
    insecureSkipTLSVerify: !!c.insecureSkipTLSVerify,
  }));
}

// Returns an error message string, or null when valid. `existingNames` are the
// OTHER repos' names (exclude the one being edited) for duplicate detection.
export function validateCustomRepoForm(f: CustomRepoForm, existingNames: string[]): string | null {
  if (!f.name) return 'Name is required';
  if (f.name.length > NAME_MAX) return `Name must be ${ NAME_MAX } characters or fewer`;
  if (!DNS1123.test(f.name)) return 'Name must be lowercase alphanumeric and dashes (DNS-1123)';
  if (RESERVED_REPO_NAMES.includes(f.name)) return `"${ f.name }" is a reserved name`;
  if (existingNames.includes(f.name)) return `Duplicate repository name "${ f.name }"`;
  if (f.type === 'helm') {
    if (!/^https?:\/\//.test(f.url)) return 'Helm URL must start with http:// or https://';
  } else if (f.type === 'oci') {
    if (!/^oci:\/\//.test(f.url)) return 'OCI URL must start with oci://';
  } else if (f.type === 'git') {
    if (!f.gitRepo) return 'Git repository URL is required';
    if (!f.gitBranch) return 'Git branch is required';
  } else {
    return 'Unknown repository type';
  }
  return null;
}
