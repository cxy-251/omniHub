import { Component, lazy, type ComponentType, type ErrorInfo, type ReactNode } from 'react';

class PageLoadError extends Error {}
let recovery: Promise<boolean> | undefined;

// Only a confirmed deployment change may refresh automatically. Offline or
// broken modules stay recoverable in-place; never create a reload loop.
async function recoverDeployment(): Promise<boolean> {
  try {
    const response = await fetch('./index.html', { cache: 'no-store', signal: AbortSignal.timeout(5000) });
    if (!response.ok) return false;
    const html = new DOMParser().parseFromString(await response.text(), 'text/html');
    const entry = (doc: Document) => doc.querySelector<HTMLScriptElement>('script[type="module"][src]')?.getAttribute('src');
    const next = entry(html), current = entry(document);
    if (!next || !current || next === current) return false;
    // Keep active playback and in-progress edits intact; the recovery button
    // remains available when refreshing automatically would interrupt work.
    if ([...document.querySelectorAll<HTMLMediaElement>('audio,video')].some(media => !media.paused)) return false;
    if (document.querySelector('form[data-dirty="true"]')) return false;
    const key = 'omnihub.recovered-build';
    if (sessionStorage.getItem(key) === next) return false;
    sessionStorage.setItem(key, next);
    window.location.reload();
    return true;
  } catch { return false; }
}

/** Lazy feature loading with bounded recovery across embedded Web releases. */
export function lazyPage<T extends ComponentType<any>>(loader: () => Promise<{ default: T }>) {
  return lazy(async () => {
    try { return await loader(); }
    catch (error) {
      if (!recovery) recovery = recoverDeployment().finally(() => { recovery = undefined; });
      if (await recovery) return await new Promise<{ default: T }>(() => {});
      throw new PageLoadError('页面文件未能加载，请检查连接后重新加载。', { cause: error });
    }
  });
}

/** Isolates one page failure so navigation and ongoing audio remain usable. */
export class PageBoundary extends Component<{ children: ReactNode; name?: string }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error(`[omniHub] ${this.props.name ?? '页面'}`, error, info.componentStack); }
  render() {
    if (!this.state.error) return this.props.children;
    const loading = this.state.error instanceof PageLoadError;
    return <section className="page-recovery" role="alert">
      <h2>{loading ? '页面暂时无法加载' : '这个页面遇到了问题'}</h2>
      <p>{loading ? '连接可能已中断，或服务端刚刚更新。你可以继续切换其他页面。' : '其他页面仍然可以使用。重新加载后可以再次尝试。'}</p>
      <button className="workbench-button primary" onClick={() => window.location.reload()}>重新加载</button>
      <details><summary>错误详情</summary><p>{this.state.error.message}</p></details>
    </section>;
  }
}

export function PageLoading() {
  return <div className="page-loading" role="status"><span />正在加载页面…</div>;
}
