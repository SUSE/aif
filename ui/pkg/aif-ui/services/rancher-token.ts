// Mints Rancher API tokens for the operator, as the logged-in user.
//
// Rancher only ever mints a token for the identity making the request: a probe
// that sent `userPrincipal: local://user-xxxxx` had it overwritten with the
// caller's own principal. A ServiceAccount therefore cannot mint one, which is
// why this lives in the UI and not in the operator.

export const TOKEN_EXPIRES_ANNOTATION = 'ai-factory.suse.com/token-expires-at';
export const TOKEN_NAME_ANNOTATION = 'ai-factory.suse.com/token-name';
export const DEFAULT_TOKEN_SECRET_NAME = 'aif-rancher-token';
export const DEFAULT_TOKEN_SECRET_KEY = 'token';

const EXT_TOKENS_URL = '/apis/ext.cattle.io/v1/tokens';
const LEGACY_TOKENS_URL = '/v3/tokens';
const TOKEN_DESCRIPTION = 'AI Factory operator';

export interface MintedToken {
  value: string;
  expiresAt: string;
  tokenName: string;
}

function isNotFound(e: any): boolean {
  const status = e?.status ?? e?.statusCode ?? e?.response?.status;
  return status === 404 || status === 405;
}

async function currentPrincipalId(store: any): Promise<string> {
  const me = await store.dispatch('rancher/request', { url: '/v3/users?me=true' });
  const user = me?.data?.[0] ?? me;
  return user?.principalIds?.[0] || `local://${ user?.id || '' }`;
}

// mintOperatorToken creates a Rancher API token for the logged-in user.
//
// ttl 0 means "as long as Rancher permits", not "never": Rancher clamps it to
// auth-token-max-ttl-minutes, 90 days by default. The returned expiresAt is read
// from the response rather than computed, so a cluster with a different cap is
// reported correctly.
export async function mintOperatorToken(store: any): Promise<MintedToken> {
  const principalId = await currentPrincipalId(store);

  try {
    const created = await store.dispatch('rancher/request', {
      url:     EXT_TOKENS_URL,
      method:  'POST',
      headers: { 'Content-Type': 'application/json' },
      data:    {
        apiVersion: 'ext.cattle.io/v1',
        kind:       'Token',
        metadata:   { generateName: 'aif-operator-' },
        spec:       {
          description:   TOKEN_DESCRIPTION,
          ttl:           0,
          // Rancher overwrites this with the requesting user's principal.
          userPrincipal: { name: principalId },
        },
      },
    });

    return {
      value:     created?.status?.bearerToken,
      expiresAt: created?.status?.expiresAt || '',
      tokenName: created?.metadata?.name || '',
    };
  } catch (e) {
    if (!isNotFound(e)) throw e;
  }

  // Rancher older than 2.13 has no tokens.ext.cattle.io.
  const legacy = await store.dispatch('rancher/request', {
    url:     LEGACY_TOKENS_URL,
    method:  'POST',
    headers: { 'Content-Type': 'application/json' },
    data:    { description: TOKEN_DESCRIPTION, ttl: 0 },
  });

  return {
    value:     legacy?.token,
    expiresAt: legacy?.expiresAt || '',
    tokenName: legacy?.name || legacy?.id || '',
  };
}

// ensureTokenSecret creates or replaces the Secret holding the minted token.
// The expiry annotation lets the UI warn before the token dies without another
// API call; the token-name annotation lets a re-authorization delete the token
// it replaces, so re-minting does not accumulate dead tokens.
export async function ensureTokenSecret(
  store: any,
  namespace: string,
  name: string,
  minted: MintedToken,
): Promise<void> {
  const body = {
    apiVersion: 'v1',
    kind:       'Secret',
    type:       'Opaque',
    metadata:   {
      name,
      namespace,
      annotations: {
        [TOKEN_EXPIRES_ANNOTATION]: minted.expiresAt,
        [TOKEN_NAME_ANNOTATION]:    minted.tokenName,
      },
    },
    data: { [DEFAULT_TOKEN_SECRET_KEY]: btoa(minted.value) },
  };

  const collection = `/k8s/clusters/local/api/v1/namespaces/${ namespace }/secrets`;
  try {
    await store.dispatch('rancher/request', { url: `${ collection }/${ name }` });
  } catch (e) {
    if (!isNotFound(e)) throw e;
    await store.dispatch('rancher/request', {
      url: collection, method: 'POST', headers: { 'Content-Type': 'application/json' }, data: body,
    });
    return;
  }

  await store.dispatch('rancher/request', {
    url:     `${ collection }/${ name }`,
    method:  'PUT',
    headers: { 'Content-Type': 'application/json' },
    data:    body,
  });
}

// deleteToken removes a previously minted token. Best-effort: a token that is
// already gone is not an error.
export async function deleteToken(store: any, tokenName: string): Promise<void> {
  if (!tokenName) return;
  try {
    await store.dispatch('rancher/request', {
      url: `${ EXT_TOKENS_URL }/${ tokenName }`, method: 'DELETE',
    });
  } catch (e) {
    if (isNotFound(e)) return;
    try {
      await store.dispatch('rancher/request', {
        url: `${ LEGACY_TOKENS_URL }/${ tokenName }`, method: 'DELETE',
      });
    } catch (inner) {
      if (!isNotFound(inner)) throw inner;
    }
  }
}
