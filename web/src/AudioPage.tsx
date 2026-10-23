import { useEffect, useRef, useState } from 'react';
import { api } from './api';
import { usePaged } from './paged';
import { formatTime, useAudio, type Track } from './AudioEngine';
import { chip, stored, store } from './ui';
import VirtualList from './VirtualList';

type Album = { id: string; title: string; track_count: number; cover_path?: string; library_name?: string };
type LyricLine = { time: number; text: string };
type LyricsData = { synced: boolean; lines: LyricLine[] };
const lyricStatusText: Record<string, string> = { plain: '只有纯文本歌词', search: '歌词是搜索来的，待核对', none: '查不到歌词', error: '歌词查询失败', unknown: '没查过歌词' };

// 正在播放那首的歌词：带时间轴的跟着进度高亮、滚到中间（手动滚动后 4 秒内不跟随），点一行跳过去；纯文本整段显示。
function LyricsPanel() {
  const engine = useAudio();
  const id = engine?.track?.id;
  const [data, setData] = useState<LyricsData>({ synced: false, lines: [] });
  const box = useRef<HTMLDivElement>(null);
  const userScrolled = useRef(0);
  useEffect(() => {
    setData({ synced: false, lines: [] });
    if (!id) return;
    const controller = new AbortController();
    api<LyricsData>(`/api/media/audio/lyrics?path=${encodeURIComponent(id)}`, { signal: controller.signal })
      .then(r => setData(r?.lines ? r : { synced: false, lines: [] })).catch(() => undefined);
    return () => controller.abort();
  }, [id]);
  const t = engine?.current ?? 0;
  let current = -1;
  if (data.synced) for (let k = 0; k < data.lines.length && data.lines[k].time <= t + 0.2; k++) current = k;
  useEffect(() => {
    if (current < 0 || Date.now() - userScrolled.current < 4000) return;
    const el = box.current?.children[current] as HTMLElement | undefined;
    const parent = box.current;
    if (el && parent) parent.scrollTo({ top: el.offsetTop - parent.clientHeight / 2 + el.clientHeight / 2, behavior: 'smooth' });
  }, [current]);
  if (!data.lines.length) return null;
  return <aside className="min-w-0 space-y-2">
    <p className="truncate text-xs text-muted">{engine?.track?.title}{data.synced ? '' : '（纯文本歌词）'}</p>
    <div ref={box} className="relative max-h-[50vh] space-y-1.5 overflow-y-auto rounded border border-line p-3 md:max-h-[calc(100vh-11rem)]"
      onWheel={() => { userScrolled.current = Date.now(); }} onTouchMove={() => { userScrolled.current = Date.now(); }}>
      {data.lines.map((l, i) => <p key={i} className={`whitespace-pre-wrap ${!data.synced ? 'text-sm' : i === current ? 'text-base text-primary' : 'cursor-pointer text-sm text-muted hover:text-foreground'}`}
        onClick={() => { if (data.synced) engine?.seek(l.time); }}>{l.text || '\u00a0'}</p>)}
    </div>
  </aside>;
}

type Artist = { id: string; title: string; single_count: number; album_count: number; track_count: number; cover_path?: string; library_name?: string };
type Section = { album: string; id: string; offset: number; count: number; cover_path?: string };
const cover = (p?: string) => p ? `/api/media/audio/cover?path=${encodeURIComponent(p)}` : '';

// 音乐右栏：一个歌手的单曲与专辑，点专辑进去是专辑里的歌。点任何一首都按歌手播放列表放（单曲在前，再接各张专辑），
// 播完接着下一首单曲或下一张专辑；「循环这个歌手 / 这张专辑」把播放范围定在那里并切到列表循环。
function MusicPane({ artist }: { artist?: Artist }) {
  const engine = useAudio();
  const [sections, setSections] = useState<Section[]>([]);
  const [album, setAlbum] = useState<Section>();
  const [review, setReview] = useState(false);
  useEffect(() => {
    setSections([]); setAlbum(undefined);
    if (!artist) return;
    const controller = new AbortController();
    api<{ sections: Section[] }>(`/api/media/audio/artist?path=${encodeURIComponent(artist.id)}`, { signal: controller.signal })
      .then(r => setSections(r.sections)).catch(() => undefined);
    return () => controller.abort();
  }, [artist?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  const scope = album?.id ?? artist?.id ?? '';
  const paged = usePaged<Track>(scope ? `/api/media/audio/tracks?${new URLSearchParams({ path: scope, ...(review ? { lyrics: 'unsynced' } : {}) })}` : '', 100);
  if (!artist) return <p className="text-sm text-muted">还没有音乐（在桌面端「下载 → 音乐」下载，移入资源库后在这里）</p>;
  const singles = sections[0]?.count ?? 0;
  const albums = sections.slice(1);
  const rows = album || review ? paged.total : Math.min(paged.total, singles);
  const loop = (id: string, title: string) => { engine?.setMode('list'); engine?.open(id, title, 0, true); };
  const play = (t: Track) => {
    if (album) engine?.open(artist.id, artist.title, album.offset + t.index - 1, true);   // 专辑里点歌：播完接着歌手的下一张专辑
    else engine?.open(artist.id, artist.title, t.index - 1, true);
  };
  return <div className="min-w-0 space-y-2">
    <div className="flex flex-wrap items-center gap-2">
      {album ? <button className={chip(false)} onClick={() => setAlbum(undefined)}>← {artist.title}</button> : null}
      <h2 className="truncate text-lg font-semibold">{album ? album.album : artist.title}</h2>
      <span className="text-sm text-muted">{album ? `${album.count} 首` : `${singles} 首单曲 · ${albums.length} 张专辑`}</span>
      <button className={chip(engine?.albumId === scope && engine?.mode === 'list')} onClick={() => album ? loop(album.id, album.album) : loop(artist.id, artist.title)}>
        {album ? '🔁 循环这张专辑' : '🔁 循环这个歌手'}</button>
      <button className={`ml-auto ${chip(review)}`} onClick={() => setReview(v => !v)}>歌词待处理</button>
    </div>
    <p className="text-xs text-muted">{engine?.albumId === artist.id ? '正在按歌手播放：播完一首接下一首单曲，单曲完了接各张专辑'
      : album && engine?.albumId === album.id ? '正在循环这张专辑' : '点一首歌：按歌手播放列表接着往下放（单曲 → 各张专辑）'}</p>
    {!album && !review && singles ? <p className="text-xs text-muted">单曲</p> : null}
    {rows ? <VirtualList total={rows} rowHeight={38} className="max-h-[calc(100vh-16rem)] min-h-24 rounded border border-line" render={i => {
      const t = paged.item(i);
      const current = !!t && engine?.track?.id === t.id;
      return <button className={`flex h-full w-full items-center gap-3 border-b border-line px-3 text-left hover:bg-panel ${current ? 'text-primary' : ''}`} disabled={!t} onClick={() => t && play(t)}>
        <span className="w-8 shrink-0 text-right text-xs text-muted">{t ? (album ? t.index : i + 1) : ''}</span>
        {current ? <span className="text-xs">{engine?.playing ? '🔊' : '⏸'}</span> : null}
        <span className="min-w-0 flex-1 truncate text-sm">{t?.title ?? '…'}</span>
        {t?.lyrics_status ? <span className="shrink-0 text-xs text-[#e3b341]">{lyricStatusText[t.lyrics_status] ?? t.lyrics_status}</span> : null}
      </button>;
    }} /> : null}
    {!album && !review && albums.length ? <>
      <p className="pt-2 text-xs text-muted">专辑</p>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(9rem,1fr))] gap-3">
        {albums.map(a => <button key={a.id} className="space-y-1 rounded p-1 text-left hover:bg-panel" onClick={() => setAlbum(a)}>
          {a.cover_path ? <img className="aspect-square w-full rounded object-cover" src={cover(a.cover_path)} alt="" />
            : <span className="flex aspect-square w-full items-center justify-center rounded bg-panel text-3xl">♪</span>}
          <span className="block truncate text-sm">{a.album}</span>
          <span className="block text-xs text-muted">{a.count} 首</span>
        </button>)}
      </div>
    </> : null}
  </div>;
}

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
  const [unsyncedOnly, setUnsyncedOnly] = useState(false);
  const [section, setSection] = useState<'music' | 'book'>(() => stored('omnihub.audio.section', 'music'));
  const [artists, setArtists] = useState<Artist[]>([]);
  const [artistId, setArtistId] = useState<string>(() => stored('omnihub.audio.artist', ''));
  const artist = artists.find(a => a.id === artistId);   // 只看没有带时间轴歌词的（以后单独补）
  const album = albums.find(a => a.id === selected);

  useEffect(() => store('omnihub.audio.album', selected), [selected]);
  useEffect(() => store('omnihub.audio.section', section), [section]);
  useEffect(() => store('omnihub.audio.artist', artistId), [artistId]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Album[]>('/api/media/audio/albums?limit=100', { signal: controller.signal })
      .then(list => { setAlbums(list); if (!list.some(a => a.id === selected) && list.length) setSelected(list[0].id); })
      .catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    api<Recent[]>('/api/media/audio/recent?limit=6', { signal: controller.signal }).then(setRecent).catch(() => undefined);
    api<Artist[]>('/api/media/audio/artists', { signal: controller.signal })
      .then(list => { setArtists(list); if (!list.some(a => a.id === artistId) && list.length) setArtistId(list[0].id); }).catch(() => undefined);
    return () => controller.abort();
    // 播放的分集变化时刷新「继续收听」
  }, [active, engine?.track?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const t = window.setTimeout(() => setSearch(query.trim()), 300); return () => window.clearTimeout(t); }, [query]);

  const endpoint = active && album ? `/api/media/audio/tracks?${new URLSearchParams({ path: album.id, ...(search ? { q: search } : {}), ...(unsyncedOnly ? { lyrics: 'unsynced' } : {}) })}` : '';
  const paged = usePaged<Track>(endpoint, 100);
  const playId = (a: { id: string; title: string }, audioId: string) => {
    const name = audioId.slice(audioId.lastIndexOf('/') + 1);
    const stem = name.slice(0, name.lastIndexOf('.'));
    api<Track[]>(`/api/media/audio/tracks?${new URLSearchParams({ path: a.id, limit: '100', q: stem })}`)
      .then(list => { const hit = list.find(t => t.id === audioId); engine?.open(a.id, a.title, hit ? hit.index - 1 : 0, true); }).catch(() => undefined);
  };
  if (!active) return null;
  const done = (t: { offset: number; duration: number }) => t.duration > 0 && t.offset >= t.duration - 5;

  return <section className="grid gap-3 md:grid-cols-[16rem_1fr] xl:grid-cols-[16rem_1fr_20rem]">
    <aside className="space-y-1 md:max-h-[calc(100vh-9rem)] md:overflow-y-auto">
      <div className="flex gap-2 pb-1">
        <button className={chip(section === 'music')} onClick={() => setSection('music')}>音乐</button>
        <button className={chip(section === 'book')} onClick={() => setSection('book')}>有声书</button>
      </div>
      {error ? <p className="text-sm text-danger">{error}</p> : null}
      {recent.length ? <p className="px-1 text-xs text-muted">继续收听</p> : null}
      {recent.map(r => <button key={r.audio_id} className="block w-full rounded px-2 py-1 text-left hover:bg-panel" onClick={() => {
        // 音乐的专辑不在有声书列表里：直接按目录放
        const a = albums.find(x => x.id === r.album_id);
        if (a) setSelected(a.id);
        playId(a ?? { id: r.album_id, title: r.album_title }, r.audio_id);
      }}>
        <p className="truncate text-sm">{r.title}</p>
        <p className="truncate text-xs text-muted">{r.album_title} · {done(r) ? '已听完' : `听到 ${formatTime(r.offset)}`}</p>
      </button>)}
      <p className="px-1 pt-2 text-xs text-muted">{section === 'music' ? '歌手' : '专辑'}</p>
      {section === 'music' ? artists.map(a => <button key={a.id} className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left ${a.id === artistId ? 'bg-panel text-primary' : 'hover:bg-panel'}`} onClick={() => setArtistId(a.id)}>
        {a.cover_path
          ? <img className="h-10 w-10 shrink-0 rounded object-cover" src={cover(a.cover_path)} alt="" />
          : <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-panel text-xl">♪</span>}
        <span className="min-w-0"><span className="block truncate text-sm">{a.title}</span><span className="block text-xs text-muted">{a.single_count} 首单曲 · {a.album_count} 张专辑</span></span>
      </button>) : albums.map(a => <button key={a.id} className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left ${a.id === selected ? 'bg-panel text-primary' : 'hover:bg-panel'}`} onClick={() => setSelected(a.id)}>
        {a.cover_path
          ? <img className="h-10 w-10 shrink-0 rounded object-cover" src={`/api/media/audio/cover?path=${encodeURIComponent(a.cover_path)}`} alt="" />
          : <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded bg-panel text-xl">♪</span>}
        <span className="min-w-0"><span className="block truncate text-sm">{a.title}</span><span className="block text-xs text-muted">{a.track_count} 集{a.library_name ? ` · ${a.library_name}` : ''}</span></span>
      </button>)}
    </aside>
    {section === 'music' ? <MusicPane artist={artist} /> : <div className="min-w-0 space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <h2 className="truncate text-lg font-semibold">{album?.title ?? '没有音声专辑'}</h2>
        {album ? <span className="text-sm text-muted">{paged.total} 集</span> : null}
        {album ? <button className={chip(engine?.albumId === album.id && !!engine?.playing)} onClick={() => {
          if (engine?.albumId === album.id && engine.track) engine.toggle(); else engine?.continueAlbum(album.id, album.title);
        }}>{engine?.albumId === album.id && engine?.playing ? '⏸ 暂停' : '▶ 继续收听'}</button> : null}
        {album ? <button className={`ml-auto ${chip(unsyncedOnly)}`} onClick={() => setUnsyncedOnly(v => !v)}>歌词待处理</button> : null}
        <input className="w-48 rounded border border-line bg-background px-2 py-1 text-sm" placeholder="搜索分集" value={query} onChange={e => setQuery(e.target.value)} />
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
          {t?.lyrics_status ? <span className="shrink-0 text-xs text-[#e3b341]">{lyricStatusText[t.lyrics_status] ?? t.lyrics_status}</span> : null}
          <span className="w-24 shrink-0 text-right text-xs text-muted">{!t ? '' : done(t) ? '已听完' : t.offset > 0 ? `听到 ${formatTime(t.offset)}` : ''}</span>
        </button>;
      }} />
      {!paged.total && album ? <p className="text-sm text-muted">{search || unsyncedOnly ? '没有匹配的分集' : '加载中…'}</p> : null}
    </div>}
    <LyricsPanel />
  </section>;
}
