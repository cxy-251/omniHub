import { useEffect, useRef, useState } from 'react';
import { api, request } from './api';

type Game = { id: string; name: string; category: string; library_id: string; library_name: string; target: 'web_only' };
type Grant = { token: string; kind: 'html' | 'retro' | 'flash'; url: string; expires_at: string };
const categories = [['', '全部'], ['rpg', 'RPG'], ['slg', 'SLG'], ['retro', '模拟器'], ['flash', 'Flash']] as const;
// Stable per browser tab, also on LAN HTTP where randomUUID is unavailable.
const clientID = Array.from(crypto.getRandomValues(new Uint8Array(16)), value => value.toString(16).padStart(2, '0')).join('');
const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';

// Keep one catalog page and one sandboxed game; leaving releases the frame,
// pending fetches and its scoped server authorization, including late responses.
export default function GameLibrary() {
  const [category, setCategory] = useState('');
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const [games, setGames] = useState<Game[]>([]);
  const [more, setMore] = useState(false);
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
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setGames([]); setMore(false); setError('');
    const query = new URLSearchParams({ limit: '24', offset: String(offset) });
    if (category) query.set('category', category);
    request(`/api/games/list?${query}`, { signal: controller.signal }).then(async response => {
      const values: unknown = await response.json();
      if (!Array.isArray(values) || values.length > 24 || values.some(value => !value || typeof value.id !== 'string' || typeof value.library_id !== 'string' || typeof value.name !== 'string' || value.target !== 'web_only')) throw new Error('游戏清单响应无效');
      if (controller.signal.aborted) return;
      setGames(values as Game[]); setMore(response.headers.get('X-Has-More') === 'true');
    }).catch((failure: Error) => { if (!controller.signal.aborted) setError(failure.message); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [category, offset, revision]);

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
      if (!grant.token || !['html', 'retro', 'flash'].includes(grant.kind) || !grant.url.startsWith(`/game-content/${grant.token}/`)) throw new Error('游戏入口响应无效');
      if (controller.signal.aborted || !mounted.current) { revoke(grant); return; }
      activeGrant.current = grant; setPlaying({ game, grant });
    } catch (failure) { if (!controller.signal.aborted && mounted.current) setError((failure as Error).message); }
    finally { if (openRequest.current === controller && mounted.current) setPending(false); }
  }
  const source = playing?.grant.kind === 'html' ? playing.grant.url : playing ? `/players/${playing.grant.kind}.html?url=${encodeURIComponent(playing.grant.url)}` : '';
  return <section className="space-y-4">
    <div className="flex flex-wrap items-center gap-2">
      {categories.map(([value, label]) => <button key={value} className={`${button} ${category === value ? 'border-primary text-primary' : ''}`} onClick={() => { setCategory(value); setOffset(0); }}>{label}</button>)}
      <button className={`${button} ml-auto`} disabled={loading} onClick={() => setRevision(value => value + 1)}>刷新</button>
    </div>
    {error ? <p role="alert" className="whitespace-pre-wrap text-danger">{error}</p> : null}
    {pending ? <p role="status">正在打开游戏… <button className={button} onClick={close}>取消</button></p> : null}
    {playing ? <div ref={player} className="space-y-3 rounded-lg border border-line bg-background p-3">
      <div className="flex items-center gap-3"><h2 className="min-w-0 flex-1 truncate">{playing.game.name}</h2><button className={button} onClick={() => { void player.current?.requestFullscreen().catch((failure: Error) => setError(failure.message)); }}>全屏</button><button className={button} onClick={close}>退出游戏</button></div>
      <iframe key={playing.grant.token} title={playing.game.name} src={source} sandbox="allow-scripts allow-pointer-lock allow-modals allow-forms allow-downloads" allow="autoplay; fullscreen" referrerPolicy="no-referrer" className="h-[70vh] w-full border-0 bg-black" />
    </div> : null}
    {loading ? <p role="status">正在加载游戏…</p> : <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {games.map(game => <article key={game.id} className="flex flex-col gap-3 rounded-lg border border-line bg-panel p-4"><h3 className="truncate font-semibold" title={game.name}>{game.name}</h3><p className="truncate text-sm text-muted">{game.library_name} · {game.category} · {game.id}</p><button className={`${button} text-primary`} disabled={pending} onClick={() => { void open(game); }}>启动</button></article>)}
      {!games.length ? <p className="text-muted">当前分类没有游戏。</p> : null}
    </div>}
    <div className="flex items-center justify-center gap-3"><button className={button} disabled={loading || offset === 0} onClick={() => setOffset(value => Math.max(0, value - 24))}>上一页</button><span className="text-sm text-muted">第 {Math.floor(offset / 24) + 1} 页</span><button className={button} disabled={loading || !more} onClick={() => setOffset(value => value + 24)}>下一页</button></div>
  </section>;
}
