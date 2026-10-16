import { useEffect, useRef, useState } from 'react';
import { api, post, request } from './api';
import { button, chip, select, store, stored } from './ui';

type Game = { id: string; name: string; category: string; library_id: string; favorite?: boolean; last_played_at?: string };
type Grant = { token: string; kind: 'html' | 'retro'; url: string; expires_at: string };
const categories = [['', '全部'], ['fav', '★ 收藏'], ['rpg', 'RPG'], ['slg', 'SLG'], ['retro', '模拟器']] as const;
// Stable per browser tab, also on LAN HTTP where randomUUID is unavailable.
const clientID = Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('');
const art = (g: Game) => `/api/games/art?${new URLSearchParams({ library_id: g.library_id, id: g.id })}`;

// 网页游戏（RPG/SLG 的 HTML 版与复古模拟器）：服务端一次返回全部（几百个），这里按分类、收藏、名字筛选。
// 同时只开一个沙箱里的游戏；离开时释放 iframe、未完成的请求和服务端的临时授权（包括迟到的响应）。
export default function GameLibrary() {
  const [category, setCategory] = useState(() => stored('omnihub.games.category', ''));
  const [query, setQuery] = useState('');
  const [revision, setRevision] = useState(0);
  const [games, setGames] = useState<Game[]>([]);
  const [loading, setLoading] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState('');
  const [playing, setPlaying] = useState<{ game: Game; grant: Grant }>();
  const activeGrant = useRef<Grant | undefined>(undefined);
  const openRequest = useRef<AbortController | undefined>(undefined);
  const player = useRef<HTMLDivElement>(null);
  const mounted = useRef(false);

  function revoke(grant: Grant | undefined) {
    if (!grant) return;
    void request(`/game-content/${encodeURIComponent(grant.token)}/`, { method: 'DELETE', keepalive: true }).catch((failure: Error) => {
      if (mounted.current) setError(`关闭游戏授权失败：${failure.message}`);
    });
  }
  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; openRequest.current?.abort(); revoke(activeGrant.current); activeGrant.current = undefined; };
  }, []);
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
      .catch((failure: Error) => setError(failure.message.includes('logged in') ? '登录后才能收藏' : failure.message));
  };
  const needle = query.trim().toLowerCase();
  const shown = games.filter(g => (category === 'fav' ? g.favorite : !category || g.category === category) && (!needle || g.name.toLowerCase().includes(needle)));

  function close() {
    openRequest.current?.abort(); openRequest.current = undefined;
    revoke(activeGrant.current); activeGrant.current = undefined;
    setPlaying(undefined); setPending(false);
    if (document.fullscreenElement === player.current) void document.exitFullscreen().catch((failure: Error) => {
      // Removing the player may have already exited fullscreen successfully.
      if (mounted.current && document.fullscreenElement) setError(failure.message);
    });
  }
  async function open(game: Game) {
    close(); setPending(true); setError('');
    const controller = new AbortController(); openRequest.current = controller;
    try {
      const grant = await api<Grant>('/api/games/web/open', { method: 'POST', signal: controller.signal, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ library_id: game.library_id, id: game.id, client_id: clientID }) });
      if (!grant.token || !['html', 'retro'].includes(grant.kind) || !grant.url.startsWith(`/game-content/${grant.token}/`)) throw new Error('游戏入口响应无效');
      if (controller.signal.aborted || !mounted.current) { revoke(grant); return; }
      activeGrant.current = grant; setPlaying({ game, grant });
    } catch (failure) { if (!controller.signal.aborted && mounted.current) setError((failure as Error).message); }
    finally { if (openRequest.current === controller && mounted.current) setPending(false); }
  }
  const source = playing?.grant.kind === 'html' ? playing.grant.url : playing ? `/players/${playing.grant.kind}.html?url=${encodeURIComponent(playing.grant.url)}` : '';
  return <section className="space-y-3">
    <div className="flex flex-wrap items-center gap-1.5">
      {categories.map(([value, label]) => <button key={value} className={chip(category === value)} onClick={() => setCategory(value)}>{label}</button>)}
      <input className={`${select} ml-auto w-44`} placeholder="搜索游戏" value={query} onChange={e => setQuery(e.target.value)} />
      <button className={button} disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新</button>
    </div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {pending ? <p role="status">正在打开游戏… <button className={button} onClick={close}>取消</button></p> : null}
    {playing ? <div ref={player} className="space-y-2 rounded-lg border border-line bg-background p-2">
      <div className="flex items-center gap-2"><h2 className="min-w-0 flex-1 truncate">{playing.game.name}</h2><button className={button} onClick={() => { void player.current?.requestFullscreen().catch((failure: Error) => setError(failure.message)); }}>全屏</button><button className={button} onClick={close}>退出游戏</button></div>
      <iframe key={playing.grant.token} title={playing.game.name} src={source} sandbox="allow-scripts allow-pointer-lock allow-modals allow-forms allow-downloads" allow="autoplay; fullscreen; screen-wake-lock *; gamepad *" referrerPolicy="no-referrer" className="h-[calc(100vh-9rem)] min-h-80 w-full border-0 bg-black" />
    </div> : null}
    {loading ? <p role="status">正在加载游戏…</p> : <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-6">
      {shown.map(game => <article key={game.id} className="group relative overflow-hidden rounded-lg border border-line bg-panel hover:border-primary">
        <button className="block w-full text-left" disabled={pending} onClick={() => { void open(game); }} title={`启动 ${game.name}`}>
          <div className="relative aspect-[4/3] bg-background">
            <span className="absolute inset-0 flex items-center justify-center p-2 text-center text-sm text-muted">{game.name}</span>
            <img className="absolute inset-0 h-full w-full object-cover" src={art(game)} alt="" loading="lazy" decoding="async" onError={e => { e.currentTarget.style.display = 'none'; }} />
          </div>
          <p className="truncate px-2 pt-1 text-sm">{game.name}</p>
          <p className="px-2 pb-1.5 text-xs text-muted">{game.category === 'retro' ? '模拟器' : game.category.toUpperCase()}</p>
        </button>
        <button className={`absolute right-1 top-1 rounded-full bg-black/60 px-1.5 text-sm ${game.favorite ? 'text-[#e3b341]' : 'text-white/70 opacity-60 hover:opacity-100'}`} onClick={() => favorite(game)} title={game.favorite ? '取消收藏' : '收藏'}>{game.favorite ? '★' : '☆'}</button>
      </article>)}
      {!shown.length ? <p className="col-span-full text-muted">{category === 'fav' ? '还没有收藏的游戏。' : '没有匹配的游戏。'}</p> : null}
    </div>}
  </section>;
}
