interface CompDep { chartName: string; dependsOn?: string[] }

export function validateDependsOn(components: CompDep[]): string | null {
  const known = new Set(components.map(c => c.chartName));
  const graph: Record<string, string[]> = {};
  for (const c of components) {
    for (const d of c.dependsOn || []) {
      if (d === c.chartName) return `Component "${c.chartName}" cannot depend on itself.`;
      if (!known.has(d)) return `Component "${c.chartName}" depends on unknown component "${d}".`;
    }
    graph[c.chartName] = c.dependsOn || [];
  }
  const state: Record<string, number> = {}; // 0 unvisited, 1 on-stack, 2 done
  const visit = (n: string): boolean => {
    state[n] = 1;
    for (const m of graph[n] || []) {
      if (state[m] === 1) return true;
      if (!state[m] && visit(m)) return true;
    }
    state[n] = 2;
    return false;
  };
  for (const c of components) {
    if (!state[c.chartName] && visit(c.chartName)) {
      return `Component dependency cycle detected involving "${c.chartName}".`;
    }
  }
  return null;
}
