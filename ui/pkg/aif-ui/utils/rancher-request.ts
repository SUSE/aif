export interface RancherHttpErrorShape {
  _status?: unknown;
  code?: unknown;
  data?: unknown;
  message?: unknown;
  status?: unknown;
  statusCode?: unknown;
  response?: { data?: unknown; status?: unknown };
}

// rancher/request rejects with the parsed response body and attaches the HTTP
// status as a non-enumerable `_status`. Kubernetes and Norman use other fields.
// Ignore non-HTTP numeric codes such as DOMException.code and transport status 0.
export function rancherHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined;

  const candidate = error as RancherHttpErrorShape;
  const rawStatuses = [
    candidate._status,
    candidate.code,
    candidate.status,
    candidate.statusCode,
    candidate.response?.status,
  ];

  for (const rawStatus of rawStatuses) {
    const trimmed = typeof rawStatus === 'string' ? rawStatus.trim() : '';
    const status = /^\d{3}$/.test(trimmed) ? Number(trimmed) : rawStatus;
    if (typeof status === 'number' && Number.isInteger(status) && status >= 100 && status <= 599) {
      return status;
    }
  }

  return undefined;
}

// Rancher rejects requests with the response body rather than an axios error.
export function requestErrorMessage(error: unknown): string {
  if (typeof error !== 'object' || error === null) return String(error);
  const candidate = error as RancherHttpErrorShape;
  if (typeof candidate.message === 'string' && candidate.message) return candidate.message;
  if (typeof candidate.data === 'string' && candidate.data) return candidate.data;
  if (typeof candidate.data === 'object' && candidate.data !== null) {
    const message = (candidate.data as { message?: unknown }).message;
    if (typeof message === 'string' && message) return message;
  }
  return String(error);
}
