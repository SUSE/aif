export function formatDate(iso?: string): string {
  if (!iso) {
    return '—';
  }
  const d = new Date(iso);

  return isNaN(d.getTime()) ? '—' : d.toLocaleDateString();
}
