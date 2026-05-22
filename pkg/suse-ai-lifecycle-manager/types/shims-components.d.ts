// @components/* is a webpack alias resolved to @rancher/shell/rancher-components at build time.
// TypeScript does not need to type-check the shell's source tree for this alias.
declare module '@components/*';
