// Only the scoped resource returned by the game gateway may reach a runtime.
export function gameResource(): URL {
  const value = new URLSearchParams(location.search).get('url');
  if (!value) throw new Error('游戏资源入口缺失');
  const source = new URL(value, location.href);
  if (source.origin !== location.origin || !source.pathname.startsWith('/game-content/') || source.search || source.hash || source.username || source.password) throw new Error('游戏资源入口无效');
  return source;
}

export function showError(failure: unknown): void {
  const element = document.getElementById('error');
  if (!element) return;
  element.textContent = failure instanceof Error ? failure.message : String(failure);
  element.hidden = false;
}

export function loadScript(source: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const script = document.createElement('script');
    const timer = setTimeout(() => { script.remove(); reject(new Error('离线运行器加载超时')); }, 15000);
    script.src = source; script.crossOrigin = 'anonymous';
    script.onload = () => { clearTimeout(timer); resolve(); };
    script.onerror = () => { clearTimeout(timer); reject(new Error('离线运行器资源不可用')); };
    document.head.appendChild(script);
  });
}
