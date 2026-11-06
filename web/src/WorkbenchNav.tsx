import { useEffect, useRef, type ReactNode } from 'react';

export const tabs = [
  ['shortvideo', '短视频'], ['gallery', '相册'], ['audio', '音声'], ['text', '文本'],
  ['manga', '漫画'], ['games', '游戏'], ['weblab', '实验室'], ['tools', '工具'], ['admin', '设置'],
] as const;
export type Tab = typeof tabs[number][0];
type NavProps = { visible: readonly (typeof tabs[number])[]; current?: Tab; account?: ReactNode; onChoose(id: Tab): void };
const paths: Record<Tab, React.ReactNode> = {
  shortvideo: <path d="m8 4 13 8-13 8Z" />,
  gallery: <><rect x="3" y="3" width="18" height="18" rx="3" /><circle cx="8" cy="8" r="1" /><path d="m3 17 5-5 4 4 4-7 5 7" /></>,
  audio: <><path d="M4 14v-3a8 8 0 0 1 16 0v3" /><rect x="3" y="12" width="4" height="9" rx="2" /><rect x="17" y="12" width="4" height="9" rx="2" /></>,
  text: <><rect x="3" y="3" width="18" height="18" rx="3" /><path d="M7 8h10M7 12h10M7 16h7" /></>,
  manga: <><path d="M12 5c-3-2-6-2-10-1v15c4-1 7-1 10 1 3-2 6-2 10-1V4c-4-1-7-1-10 1Zm0 0v15" /></>,
  games: <><path d="M7 6h10c3 0 4 5 5 11 0 3-3 3-6-1H8c-3 4-6 4-6 1 1-6 2-11 5-11Z" /><path d="M5 11h6M8 8v6M16 10h.01M19 13h.01" /></>,
  weblab: <><path d="M12 2 3 7l9 5 9-5-9-5Z" /><path d="m3 17 9 5 9-5M3 12l9 5 9-5" /></>,
  tools: <path d="m14 6 4 4 4-4a7 7 0 0 1-9 9l-7 7-4-4 7-7a7 7 0 0 1 9-9Z" />,
  admin: <><path d="m10 2 4 0 1 3 3 1 3-1 2 4-2 3 0 2 1 3-3 3-3-1-3 1-1 3-4-1-1-3-3-1-3 1-2-4 2-3 0-2-1-3 3-3 3 1Z" transform="translate(1 0) scale(.9)" /><circle cx="12" cy="12" r="3" /></>,
};

function NavLinks({ visible, current, onChoose }: NavProps) {
  return <>{visible.map(([id, label]) => <a key={id} href={`#${id}`} aria-current={current === id ? 'page' : undefined}
    className={`workbench-link ${id === 'tools' ? 'nav-divider' : ''}`} title={label} onClick={event => {
      if (!event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && event.button === 0) onChoose(id);
    }}>
    <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{paths[id]}</svg>
    <span>{label}</span>
  </a>)}</>;
}

/** Persistent navigation contains no feature data or media instances. */
export function WorkbenchNav(props: NavProps & { onCollapse(): void }) {
  return <aside id="workbench-sidebar" className="workbench-sidebar">
    <div className="sidebar-heading"><a className="workbench-brand" href={`#${props.visible[0]?.[0] ?? ''}`}>omniHub</a>
      <button id="sidebar-toggle" className="sidebar-toggle" aria-label="收起侧栏" title="收起侧栏" aria-expanded="true" aria-controls="workbench-sidebar" onClick={props.onCollapse}>‹</button>
    </div>
    <p className="nav-caption">媒体库</p>
    <nav aria-label="主要导航"><NavLinks {...props} /></nav>
    <div className="sidebar-account">{props.account}</div>
  </aside>;
}

export function CompactNavigation(props: NavProps) {
  const ref = useRef<HTMLDetailsElement>(null);
  useEffect(() => {
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape' && ref.current?.open) { ref.current.open = false; ref.current.querySelector('summary')?.focus(); } };
    window.addEventListener('keydown', close);
    return () => window.removeEventListener('keydown', close);
  }, []);
  return <details ref={ref} className="compact-navigation">
    <summary aria-label="展开导航"><span aria-hidden="true">☰</span> 导航</summary>
    <nav aria-label="紧凑导航" onClick={event => { if ((event.target as HTMLElement).closest('button') && ref.current) ref.current.open = false; }}>
      <NavLinks {...props} onChoose={id => { if (ref.current) ref.current.open = false; props.onChoose(id); }} />
      <div className="compact-account">{props.account}</div>
    </nav>
  </details>;
}
