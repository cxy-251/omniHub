// 共用的控件样式（Tailwind 类名），各页面一致。
export const chip = (on: boolean) => `shrink-0 rounded-full border px-2.5 py-0.5 text-sm ${on ? 'border-primary text-primary' : 'border-line text-muted hover:text-foreground'}`;
export const button = 'rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary disabled:opacity-50';
export const primaryButton = 'rounded-md bg-[#1f6feb] px-4 py-1.5 text-sm text-white hover:bg-[#2f81f7] disabled:bg-panel disabled:text-muted';
export const select = 'rounded border border-line bg-background px-2 py-1 text-sm';
export const iconButton = 'flex h-9 w-9 items-center justify-center rounded-full text-lg hover:bg-white/15 disabled:opacity-40';

// localStorage 只做本浏览器的便利设置（上次的频道、模式等）；读写失败（隐私模式等）时用默认值。
export function stored<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw === null ? fallback : JSON.parse(raw) as T;
  } catch { return fallback; }
}
export function store(key: string, value: unknown) {
  try { localStorage.setItem(key, JSON.stringify(value)); } catch { /* 忽略 */ }
}

// 全局提示：notify('…') 在页面底部弹出 2.5 秒（App 里的 <Toast /> 显示）。
export function notify(message: string) {
  window.dispatchEvent(new CustomEvent('omnihub-notice', { detail: message }));
}
// 把接口错误换成用户看得懂的话：未登录时提示去登录。
export function explain(error: Error, action: string) {
  return /login required|logged in|403/i.test(error.message) ? `登录后才能${action}` : `${action}失败：${error.message}`;
}
