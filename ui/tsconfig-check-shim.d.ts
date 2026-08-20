// Ambient declarations used only by `yarn typecheck` (tsconfig.check.json).
// The webpack build resolves these for real; tsc only needs them to not error.
declare module '*.vue' {
  const component: any;
  export default component;
}
declare module '@rancher/auto-import' {
  export function importTypes(plugin: any): void;
}
declare module '@shell/*';
declare module '@components/*';
