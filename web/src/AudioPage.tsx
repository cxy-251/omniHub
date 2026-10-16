import { useEffect, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import { formatTime, useAudio, type Track } from './AudioEngine';
import { chip, stored, store } from './ui';
import VirtualList from './VirtualList';

type Album = { id: string; title: string; track_count: number; cover_path?: string; library_name?: string };
type Recent = { audio_id: string; album_id: string; title: string; album_title: string; offset: number; duration: number };

// 音声页：「继续收听」与专辑列表，所选专辑的分集（可搜索、按页懒加载、显示断点）。播放交给全局 AudioEngine。
export default function AudioPage({ active }: { active: boolean }) {
  const engine = useAudio();
  const [albums, setAlbums] = useState<Album[]>([]);
  const [recent, setRecent] = useState<Recent[]>([]);
  const [selected, setSelected] = useState<string>(() => stored('omnihub.audio.album', ''));
  const [query, setQuery] = useState('');
  const [search, setSearch] = useState('');
  const [error, setError] = useState('');
  const album = albums.find(a => a.id === selected);

  useEffect(() => store('omnihub.audio.album', selected), [selected]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Album[]>('/api/media/audio/albums?limit=100', { signal: controller.signal })
      .then(list => { setAlbums(list); if (!list.some(a => a.id === selected) && list.length) setSelected(list[0].id); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    api<Recent[]>('/api/media/audio/recent?limit=6', { signal: controller.signal }).then(setRecent).catch(() => undefined);
    return () => controller.abort();
    // 播放的分集变化时刷新「继续收听」
  }, [active, engine?.track?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const t = window.setTimeout(() => setSearch(query.trim()), 300); return () => window.clearTimeout(t); }, [query]);

  const endpoint = active && album ? `/api/media/audio/tracks?${new URLSearchParams({ path: album.id, ...(search ? { q: search } : {}) })}` : '';
  const paged = usePaged<Track>(endpoint, 100);
  const playId = (a: { id: string; title: string }, audioId: string) => {
    const name = audioId.slice(audioId.lastIndexOf('/') + 1);
    const stem = name.slice(0, name.lastIndexOf('.'));
    api<Track[]>(`/api/media/audio/tracks?${new URLSearchParams({ path: a.id, limit: '100', q: stem })}`)
      .then(list => { const hit = list.find(t => t.id === audioId); engine?.open(a.id, a.title, hit ? hit.index - 1 : 0, true); }).catch(() => undefined);
  };
  if (!active) return null;
  const done = (t: { offset: number; duration: number }) => t.duration > 0 && t.offset >= t.duration - 5;

  return <section className="grid gap-3 md:grid-cols-[16rem_1fr]">
    <aside className="space-y-1 md:max-h-[calc(100vh-9rem)] md:overflow-y-auto">
      {error ? <p className="text-sm text-danger">{error}</p> : null}
      {recent.length ? <p className="px-1 text-xs text-muted">继续收听</p> : null}
      {recent.map(r => <button key={r.audio_id} className="block w-full rounded px-2 py-1 text-left hover:bg-panel" onClick={() => {
        const a = albums.find(x => x.id === r.album_id);
        if (a) { setSelected(a.id); playId(a, r.audio_id); }
      }}>
        <p className="truncate text-sm">{r.title}</p>
        <p className="truncate text-xs text-muted">{r.album_title} · {done(r) ? '已听完' : `听到 ${formatTime(r.offset)}`}</p>
      </button>)}
      <p className="px-1 pt-2 text-xs text-muted">专辑</p>
      {albums.map(a => <button key={a.id} className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left ${a.id === selected ? 'bg-panel text-primary' : 'hover:bg-panel'}`} onClick={() => setSelected(a.id)}>
        {a.cover_path
          ? <img className="h-10 w-10 shrink-0 rounded object-cover" src={`/api/media/audio/cover?path=${encodeURIComponent(a.cover_path)}`} alt="" />
          : <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-panel text-xl">♪</span>}
        <span className="min-w-0"><span className="block truncate text-sm">{a.title}</span><span className="block text-xs text-muted">{a.track_count} 集{a.library_name ? ` · ${a.library_name}` : ''}</span></span>
      </button>)}
    </aside>
    <div className="min-w-0 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="truncate text-lg font-semibold">{album?.title ?? '没有音声专辑'}</h2>
        {album ? <span className="text-sm text-muted">{paged.total} 集</span> : null}
        {album ? <button className={chip(engine?.albumId === album.id && !!engine?.playing)} onClick={() => {
          if (engine?.albumId === album.id && engine.track) engine.toggle(); else engine?.continueAlbum(album.id, album.title);
        }}>{engine?.albumId === album.id && engine?.playing ? '⏸ 暂停' : '▶ 继续收听'}</button> : null}
        <input className="ml-auto w-48 rounded border border-line bg-background px-2 py-1 text-sm" placeholder="搜索分集" value={query} onChange={e => setQuery(e.target.value)} />
      </div>
      {paged.error ? <p className="text-sm text-danger">{paged.error}</p> : null}
      <VirtualList total={paged.total} rowHeight={40} className="h-[calc(100vh-13rem)] min-h-64 rounded border border-line" render={i => {
        const t = paged.item(i);
        const current = !!t && engine?.track?.id === t.id;
        return <button className={`flex h-full w-full items-center gap-3 border-b border-line px-3 text-left hover:bg-panel ${current ? 'text-primary' : ''}`} disabled={!t}
          onClick={() => t && album && engine?.open(album.id, album.title, t.index - 1, true)}>
          <span className="w-8 shrink-0 text-right text-xs text-muted">{t?.index ?? ''}</span>
          {current ? <span className="text-xs">{engine?.playing ? '🔊' : '⏸'}</span> : null}
          <span className={`min-w-0 flex-1 truncate text-sm ${t && done(t) && !current ? 'text-muted' : ''}`}>{t?.title ?? '…'}</span>
          {t && t.offset > 0 && t.duration > 0 ? <span className="hidden h-1 w-20 overflow-hidden rounded bg-line sm:block"><span className={`block h-full ${done(t) ? 'bg-[#3fb950]' : 'bg-[#58a6ff]'}`} style={{ width: `${Math.min(100, t.offset / t.duration * 100)}%` }} /></span> : null}
          <span className="w-24 shrink-0 text-right text-xs text-muted">{!t ? '' : done(t) ? '已听完' : t.offset > 0 ? `听到 ${formatTime(t.offset)}` : ''}</span>
        </button>;
      }} />
      {!paged.total && album ? <p className="text-sm text-muted">{search ? '没有匹配的分集' : '加载中…'}</p> : null}
    </div>
  </section>;
}
