// @components/* is a webpack alias resolved to @rancher/shell/rancher-components at build time.
// TypeScript does not need to type-check the shell's source tree for this alias.
declare module '@components/*';

// @shell/components/*.vue files exist in node_modules but have no .d.ts — suppress TS errors.
declare module '@shell/components/*';

// Side-effect CSS imports (e.g. `import './style/brand.css'`) carry no value
// for the type system; webpack handles them at build time.
declare module '*.css';
