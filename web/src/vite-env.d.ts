/// <reference types="vite/client" />

// katex 0.16.9 自带的类型不在 exports 里，只声明用到的部分。
declare module 'katex' {
  export function renderToString(tex: string, options?: { displayMode?: boolean; throwOnError?: boolean; strict?: boolean }): string;
  const katex: { renderToString: typeof renderToString };
  export default katex;
}
