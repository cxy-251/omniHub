import { useEffect, useRef, useState } from 'react';
import { api, post, request } from './api';
import { button, chip, explain, notify, select, store, stored } from './ui';

type Game = { id: string; name: string; category: string; library_id: string; favorite?: boolean; last_played_at?: string };
type Grant = { token: string; kind: 'html' | 'retro'; url: string; expires_at: string };
const categories = [['', '全部'], ['fav', '★ 收藏'], ['rpg', 'RPG'], ['slg', 'SLG'], ['retro', '模拟器']] as const;
const art = (g: Game) => `/api/games/art?${new URLSearchParams({ library_id: g.library_id, id: g.id })}`;
const playUrl = (g: Game) => `/#play/${encodeURIComponent(g.library_id)}/${encodeURIComponent(g.id)}`;

// 网页游戏（RPG/SLG 的 HTML 版与复古模拟器）：服务端一次返回全部（几百个），这里按分类、收藏、名字筛选。
// 点卡片在新标签页打开（GamePlayer），每个标签各自申请、各自释放游戏授权，可以同时开几个。
export default function GameLibrary() {
  const [category, setCategory] = useState(() => stored('omnihub.games.category', ''));
  const [query, setQuery] = useState('');
  const [revision, setRevision] = useState(0);
  const [games, setGames] = useState<Game[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  useEffect(() => store('omnihub.games.category', category), [category]);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setError('');
    api<Game[]>('/api/games/list', { signal: controller.signal })
      .then(list => { if (!Array.isArray(list)) throw new Error('游戏清单响应无效'); setGames(list); })
      .catch((failure: Error) => { if (!controller.signal.aborted) setError(failure.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [revision]);
  const favorite = (game: Game) => {
    const next = !game.favorite;
    post('/api/games/favorite', { id: game.id, favorite: next })
      .then(() => setGames(list => list.map(g => g.id === game.id ? { ...g, favorite: next } : g)))
      .catch((failure: Error) => notify(explain(failure, '收藏')));
  };
  const needle = query.trim().toLowerCase();
  const shown = games.filter(g => (category === 'fav' ? g.favorite : !category || g.category === category) && (!needle || g.name.toLowerCase().includes(needle)));
  return <section className="space-y-3">
    <div className="flex flex-wrap items-center gap-1.5">
      {categories.map(([value, label]) => <button key={value} className={chip(category === value)} onClick={() => setCategory(value)}>{label}</button>)}
      <input className={`${select} ml-auto w-44`} placeholder="搜索游戏" value={query} onChange={e => setQuery(e.target.value)} />
      <button className={button} disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新</button>
    </div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {loading ? <p role="status">正在加载游戏…</p> : <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6">
      {shown.map(game => <article key={game.id} className="group relative overflow-hidden rounded-lg border border-line bg-panel hover:border-primary">
        <a className="block" href={playUrl(game)} target="_blank" rel="noopener" title={`在新标签页打开 ${game.name}`}>
          <div className="relative aspect-[4/3] bg-background">
            <span className="absolute inset-0 flex items-center justify-center p-2 text-center text-sm text-muted">{game.name}</span>
            <img className="absolute inset-0 h-full w-full object-cover" src={art(game)} alt="" loading="lazy" decoding="async" onError={e => { e.currentTarget.style.display = 'none'; }} />
          </div>
          <p className="truncate px-2 pt-1 text-sm">{game.name}</p>
          <p className="px-2 pb-1.5 text-xs text-muted">{game.category === 'retro' ? '模拟器' : game.category.toUpperCase()}</p>
        </a>
        <button className={`absolute right-1 top-1 rounded-full bg-black/60 px-1.5 text-sm ${game.favorite ? 'text-[#e3b341]' : 'text-white/70 opacity-60 hover:opacity-100'}`} onClick={() => favorite(game)} title={game.favorite ? '取消收藏' : '收藏'}>{game.favorite ? '★' : '☆'}</button>
      </article>)}
      {!shown.length ? <p className="col-span-full text-muted">{category === 'fav' ? '还没有收藏的游戏。' : '没有匹配的游戏。'}</p> : null}
    </div>}
  </section>;
}

// GamePlayer：新标签页里的游戏本体，铺满窗口。打开时申请授权，标签关闭（pagehide）时释放。
// 游戏在沙箱 iframe 里运行（不透明源），拿不到 omniHub 的登录凭证和接口。
export function GamePlayer({ libraryId, id }: { libraryId: string; id: string }) {
  const [grant, setGrant] = useState<Grant>();
  const [error, setError] = useState('');
  const frame = useRef<HTMLIFrameElement>(null);
  const name = id.slice(id.indexOf('/') + 1);
  useEffect(() => {
    document.title = name;
    const clientID = Array.from(crypto.getRandomValues(new Uint8Array(16)), v => v.toString(16).padStart(2, '0')).join('');
    let current: Grant | undefined;
    const revoke = () => { if (current) void request(`/game-content/${encodeURIComponent(current.token)}/`, { method: 'DELETE', keepalive: true }).catch(() => undefined); current = undefined; };
    const controller = new AbortController();
    api<Grant>('/api/games/web/open', { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ library_id: libraryId, id, client_id: clientID }) })
      .then(g => {
        if (!g.token || !['html', 'retro'].includes(g.kind) || !g.url.startsWith(`/game-content/${g.token}/`)) throw new Error('游戏入口响应无效');
        current = g; setGrant(g);
      }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    window.addEventListener('pagehide', revoke);
    return () => { controller.abort(); window.removeEventListener('pagehide', revoke); revoke(); };
  }, [libraryId, id, name]);
  // 游戏加载后把键盘焦点交给它（不用先点一下画面）。
  const focus = () => frame.current?.focus();
  if (error) return <div className="flex h-screen items-center justify-center bg-black p-6 text-center text-[#ff7b72]">打不开「{name}」：{error}</div>;
  if (!grant) return <div className="flex h-screen items-center justify-center bg-black text-[#8b949e]">正在打开「{name}」…</div>;
  const source = grant.kind === 'html' ? grant.url : `/players/${grant.kind}.html?url=${encodeURIComponent(grant.url)}`;
  return <>
    <iframe ref={frame} title={name} src={source} onLoad={focus} className="fixed inset-0 h-full w-full border-0 bg-black"
      sandbox="allow-scripts allow-pointer-lock allow-modals allow-forms allow-downloads" allow="autoplay; fullscreen; screen-wake-lock *; gamepad *" referrerPolicy="no-referrer" />
    {grant.kind === 'retro' ? <KeyHint /> : null}
  </>;
}

// 模拟器键位（EmulatorJS 默认，可在模拟器右下角菜单的「控制设置」里改）。开始 10 秒显示，之后收起。
function KeyHint() {
  const [shown, setShown] = useState(true);
  useEffect(() => { const t = window.setTimeout(() => setShown(false), 10000); return () => window.clearTimeout(t); }, []);
  if (!shown) return null;
  return <div className="pointer-events-none fixed left-1/2 top-3 z-10 -translate-x-1/2 rounded-lg bg-black/75 px-4 py-2 text-center text-sm text-white">
    方向键 移动 · <b>Z</b> = A（确定）· <b>X</b> = B（取消）· <b>Enter</b> = START · <b>V</b> = SELECT · <b>A</b>/<b>S</b> = X/Y · <b>Q</b>/<b>E</b> = L/R
    <div className="text-xs text-[#8b949e]">手柄直接可用；键位可在模拟器菜单的「控制设置」里改</div>
  </div>;
}
