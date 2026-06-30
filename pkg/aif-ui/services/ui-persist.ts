// Ephemeral, UI-only persistence with TTL. Safe to no-op if storage is blocked.

export interface PersistWrap<T extends Record<string, unknown> | string | number | boolean | null> { ts: number; v: T }
const PREFIX = 'suseai.ui.';
const safe = <T>(fn: () => T, fallback: T): T => { try { return fn(); } catch { return fallback; } };

/** Load a value or fallback. Expired entries are auto-removed. */
export function persistLoad<T extends Record<string, unknown> | string | number | boolean | null>(key: string, fallback: T, ttlMs = 1000 * 60 * 60): T {
  const raw = safe(() => localStorage.getItem(PREFIX + key), null as string | null);
  if (!raw) return fallback;
  try {
    const obj = JSON.parse(raw) as PersistWrap<T>;
    if (!obj || typeof obj.ts !== 'number') return fallback;
    if (ttlMs > 0 && (Date.now() - obj.ts) > ttlMs) {
      safe(() => localStorage.removeItem(PREFIX + key), undefined);
      return fallback;
    }
    return (obj.v ?? fallback);
  } catch {
    return fallback;
  }
}

/** Save a value with a timestamp. */
export function persistSave<T extends Record<string, unknown> | string | number | boolean | null>(key: string, value: T): void {
  // Not storing aything in the browser localstorage. May want to completely remove this file and it's reference in the future
  // safe(() => localStorage.setItem(PREFIX + key, JSON.stringify({ ts: Date.now(), v: value } as PersistWrap<T>)), undefined);
}

/** Clear a key. */
export function persistClear(key: string): void {
  safe(() => localStorage.removeItem(PREFIX + key), undefined);
}
