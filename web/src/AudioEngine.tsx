import { createContext, useCallback, useContext, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api, post, request } from './api';
import { chip, iconButton, store, stored } from './ui';
import { MusicRatingButtons } from './MusicFeedback';
import { useHash } from './useHash';

// 音声播放引擎（全局一份，切换页面不中断；与桌面端同一套断点接口）。
// 断点按账号存服务端：听了不到 3 秒不记，离结尾不到 5 秒算听完。分集按专辑内序号向服务端逐条取。
// 模式：list 列表循环 / single 单集循环 / random 随机。浏览器的 Media Session 接入锁屏与耳机按键（手机上有用）。
export type Track = { index: number; title: string; id: string; ext: string; offset: number; duration: number; lyrics_status?: string; rating?: number; artist?: string; album?: string };
export type MusicState = { ratings: Record<string, number>; can_rate: boolean; can_trash: boolean };
type Mode = 'list' | 'single' | 'random';
const speeds = [1, 1.25, 1.5, 1.75, 2, 0.75];
const sleepChoices = [0, 15, 30, 45, 60];

export type Engine = {
  albumId: string; albumTitle: string; track?: Track; position: number; total: number;
  playing: boolean; current: number; duration: number; rate: number; mode: Mode; sleepMinutes: number; error: string;
  open(album: string, title: string, pos: number, autoplay: boolean): void;
  continueAlbum(album: string, title: string): void;
  toggle(): void; seek(seconds: number): void; seekBy(delta: number): void;
  next(): void; previous(): void; stop(skipSave?: boolean): void;
  cycleRate(): void; cycleMode(): void; cycleSleep(): void; setMode(m: Mode): void;
  music: MusicState; musicVersion: number; musicCatalogVersion: number;
  refreshMusic(catalog?: boolean): Promise<void>;
  rateMusic(id: string, rating: number): Promise<void>;
};

const Context = createContext<Engine | undefined>(undefined);
export const useAudio = () => useContext(Context);

export function formatTime(s: number) {
  s = Math.max(0, Math.floor(s || 0));
  const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), sec = s % 60;
  return `${h > 0 ? `${h}:${String(m).padStart(2, '0')}` : m}:${String(sec).padStart(2, '0')}`;
}

export function AudioProvider({ children }: { children: ReactNode }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [albumId, setAlbumId] = useState('');
  const [albumTitle, setAlbumTitle] = useState('');
  const [track, setTrack] = useState<Track>();
  const [position, setPosition] = useState(-1);
  const [total, setTotal] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [current, setCurrent] = useState(0);
  const [duration, setDuration] = useState(0);
  const [rate, setRate] = useState(() => stored('omnihub.audio.rate', 1));
  const [mode, setMode] = useState<Mode>(() => stored('omnihub.audio.mode', 'list'));
  const [sleepMinutes, setSleepMinutes] = useState(0);
  const [sleepAt, setSleepAt] = useState(0);
  const [error, setError] = useState('');
  const [music, setMusic] = useState<MusicState>({ ratings: {}, can_rate: false, can_trash: false });
  const [musicVersion, setMusicVersion] = useState(0);
  const [musicCatalogVersion, setMusicCatalogVersion] = useState(0);
  const saved = useRef(-1);
  const state = useRef({ albumId, albumTitle, position, total, mode, track });
  state.current = { albumId, albumTitle, position, total, mode, track };

  useEffect(() => store('omnihub.audio.rate', rate), [rate]);
  useEffect(() => store('omnihub.audio.mode', mode), [mode]);
  useEffect(() => { if (audio.current) audio.current.playbackRate = rate; }, [rate, track]);

  const save = useCallback(() => {
    const a = audio.current, t = state.current.track;
    if (!a || !t || !a.src) return;
    const pos = a.currentTime, dur = Number.isFinite(a.duration) ? a.duration : 0;
    const done = dur > 0 && pos >= dur - 5;
    if (pos < 3 && !done) return;
    const offset = done ? dur : Math.floor(pos);
    if (saved.current === offset) return;
    saved.current = offset;
    post('/api/media/audio/progress', { audio_id: t.id, offset, duration: dur }).catch(() => undefined);
  }, []);

  const load = useCallback((t: Track, album: string, title: string, pos: number, count: number, autoplay: boolean) => {
    const a = audio.current;
    if (!a) return;
    save();
    setAlbumId(album); setAlbumTitle(title); setPosition(pos); setTotal(count); setTrack(t); setError('');
    saved.current = -1;
    a.src = `/api/media/audio/stream?path=${encodeURIComponent(t.id)}`;
    const resume = t.offset > 3 && !(t.duration > 0 && t.offset >= t.duration - 5) ? t.offset : 0;
    a.onloadedmetadata = () => { if (resume) a.currentTime = resume; a.playbackRate = rate; };
    if (autoplay) a.play().catch(() => undefined);
    store('omnihub.audio.last', { album, title, pos });
  }, [rate, save]);

  const open = useCallback((album: string, title: string, pos: number, autoplay: boolean) => {
    const q = new URLSearchParams({ path: album, limit: '1', offset: String(Math.max(0, pos)) });
    request(`/api/media/audio/tracks?${q}`).then(async response => {
      const list = await response.json() as Track[];
      if (!list.length) { setError('所选列表没有可播放的音频'); return; }
      load(list[0], album, title, Math.max(0, pos), Number(response.headers.get('X-Total-Count') ?? 1), autoplay);
    }).catch((e: Error) => setError(e.message));
  }, [load]);

  // 继续一张专辑：从最近听过的那集的断点接着放；没听过就从第一集开始。
  const continueAlbum = useCallback((album: string, title: string) => {
    type Recent = { audio_id: string; album_id: string };
    api<Recent[]>('/api/media/audio/recent?limit=50').then(recent => {
      const last = recent.find(r => r.album_id === album);
      if (!last) { open(album, title, 0, true); return; }
      const name = last.audio_id.slice(last.audio_id.lastIndexOf('/') + 1);
      const stem = name.slice(0, name.lastIndexOf('.'));
      return api<Track[]>(`/api/media/audio/tracks?${new URLSearchParams({ path: album, limit: '100', q: stem })}`)
        .then(list => { const hit = list.find(t => t.id === last.audio_id); open(album, title, hit ? hit.index - 1 : 0, true); });
    }).catch((e: Error) => setError(e.message));
  }, [open]);

  const next = useCallback(() => {
    const s = state.current;
    if (!s.total) return;
    if (s.mode === 'random' && s.total > 1) {
      const r = Math.floor(Math.random() * (s.total - 1));
      open(s.albumId, s.albumTitle, r >= s.position ? r + 1 : r, true);
    } else open(s.albumId, s.albumTitle, (s.position + 1) % s.total, true);
  }, [open]);
  const previous = useCallback(() => {
    const a = audio.current, s = state.current;
    if (!a) return;
    if (a.currentTime > 5 || s.position <= 0) a.currentTime = 0;
    else open(s.albumId, s.albumTitle, s.position - 1, true);
  }, [open]);
  const ended = useCallback(() => {
    save();
    if (state.current.mode === 'single' && audio.current) { audio.current.currentTime = 0; void audio.current.play(); } else next();
  }, [next, save]);
  const stop = useCallback((skipSave = false) => {
    if (!skipSave) save();
    state.current = { ...state.current, track: undefined };
    const a = audio.current;
    if (a) { a.pause(); a.removeAttribute('src'); a.load(); }
    setTrack(undefined); setPosition(-1); setSleepMinutes(0); setSleepAt(0);
    store('omnihub.audio.last', null);
  }, [save]);
  const refreshMusic = useCallback(async (catalog = false) => {
    const data = await api<MusicState>('/api/media/audio/music/state');
    setMusic(data); setMusicVersion(v => v + 1);
    if (catalog) setMusicCatalogVersion(v => v + 1);
    const s = state.current;
    if (!s.track?.id.includes(':media_library/audio/music/')) return;
    const id = s.track.id;
    const response = await request(`/api/media/audio/tracks?${new URLSearchParams({ path: s.albumId, locate: id, limit: '1' })}`);
    const rows = await response.json() as Track[];
    if (state.current.track?.id !== id) return;
    setTotal(Number(response.headers.get('X-Total-Count') ?? 0));
    if (rows[0]) setPosition(rows[0].index - 1); else stop();
  }, [stop]);
  const rateMusic = useCallback(async (id: string, rating: number) => {
    try { await post('/api/media/audio/music/rating', { id, rating }); await refreshMusic(); }
    catch (e) { setError((e as Error).message); throw e; }
  }, [refreshMusic]);
  useEffect(() => { if (track?.id.includes(':media_library/audio/music/')) void refreshMusic().catch(() => undefined); }, [track?.id, refreshMusic]);

  // 启动时恢复上次的专辑与分集（暂停状态）。
  useEffect(() => {
    const last = stored<{ album: string; title: string; pos: number } | null>('omnihub.audio.last', null);
    if (last) open(last.album, last.title, last.pos, false);
  }, [open]);
  // 播放中每 15 秒存一次断点；离开页面时也存。
  useEffect(() => {
    if (!playing) return;
    const timer = window.setInterval(save, 15000);
    return () => window.clearInterval(timer);
  }, [playing, save]);
  useEffect(() => {
    window.addEventListener('pagehide', save);
    return () => window.removeEventListener('pagehide', save);
  }, [save]);
  // 定时关闭
  useEffect(() => {
    if (!sleepAt) return;
    const timer = window.setInterval(() => { if (Date.now() >= sleepAt) { audio.current?.pause(); setSleepMinutes(0); setSleepAt(0); } }, 5000);
    return () => window.clearInterval(timer);
  }, [sleepAt]);
  // 锁屏 / 耳机按键（Media Session）
  useEffect(() => {
    const ms = navigator.mediaSession;
    if (!ms || !track) return;
    ms.metadata = new MediaMetadata({ title: track.title, album: albumTitle, artist: 'omniHub' });
    ms.setActionHandler('play', () => void audio.current?.play());
    ms.setActionHandler('pause', () => audio.current?.pause());
    ms.setActionHandler('previoustrack', previous);
    ms.setActionHandler('nexttrack', next);
    ms.setActionHandler('seekbackward', () => { if (audio.current) audio.current.currentTime -= 10; });
    ms.setActionHandler('seekforward', () => { if (audio.current) audio.current.currentTime += 30; });
  }, [track, albumTitle, next, previous]);

  const engine = useMemo<Engine>(() => ({
    albumId, albumTitle, track, position, total, playing, current, duration, rate, mode, sleepMinutes, error,
    open, continueAlbum, next, previous, stop,
    music, musicVersion, musicCatalogVersion, refreshMusic, rateMusic,
    toggle: () => { const a = audio.current; if (!a) return; if (a.paused) void a.play(); else a.pause(); },
    seek: s => { if (audio.current) audio.current.currentTime = s; },
    seekBy: d => { if (audio.current) audio.current.currentTime = Math.max(0, audio.current.currentTime + d); },
    cycleRate: () => setRate(r => speeds[(speeds.indexOf(r) + 1) % speeds.length]),
    setMode,
    cycleMode: () => setMode(m => (['list', 'single', 'random'] as Mode[])[(['list', 'single', 'random'].indexOf(m) + 1) % 3]),
    cycleSleep: () => {
      const n = sleepChoices[(sleepChoices.indexOf(sleepMinutes) + 1) % sleepChoices.length];
      setSleepMinutes(n); setSleepAt(n ? Date.now() + n * 60000 : 0);
    },
  }), [albumId, albumTitle, track, position, total, playing, current, duration, rate, mode, sleepMinutes, error, open, continueAlbum, next, previous, stop, music, musicVersion, musicCatalogVersion, refreshMusic, rateMusic]);

  return <Context.Provider value={engine}>
    <audio ref={audio} preload="metadata"
      onPlay={() => setPlaying(true)} onPause={() => { setPlaying(false); save(); }} onEnded={ended}
      onTimeUpdate={e => setCurrent(e.currentTarget.currentTime)} onDurationChange={e => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : 0)}
      onError={() => setError('音频无法播放')} />
    {children}
  </Context.Provider>;
}

const modeLabel: Record<Mode, string> = { list: '🔁 列表', single: '🔂 单集', random: '🔀 随机' };

// 曲目标签：艺人、专辑、风格，以及下载音乐时写进文件的 Last.fm 听众、播放、重播率。
type Tags = { has_cover?: boolean; artist?: string; album?: string; genre?: string; lastfm_listeners?: number; lastfm_playcount?: number; lastfm_replay?: number;
  source_views?: number; source_likes?: number; release_date?: string; download_date?: string };
export function useTrackTags(id?: string) {
  const [tags, setTags] = useState<Tags>({});
  useEffect(() => {
    setTags({});
    if (!id) return;
    const controller = new AbortController();
    api<Tags>(`/api/media/audio/metadata?path=${encodeURIComponent(id)}`, { signal: controller.signal }).then(setTags).catch(() => undefined);
    return () => controller.abort();
  }, [id]);
  return tags;
}

// AudioBar：底部播放条，音声播放时在所有页面显示。第二行有标签时显示 艺人 · 风格 · 重播率，悬停看专辑与 Last.fm 数据。
export function AudioBar({ onOpen }: { onOpen(): void }) {
  const hash = useHash();
  return hash.split('?')[0] === '#audio/listen' ? null : <CompactAudioBar onOpen={onOpen} />;
}

function CompactAudioBar({ onOpen }: { onOpen(): void }) {
  const e = useAudio();
  const tags = useTrackTags(e?.track?.id);
  const bar = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const element = bar.current; if (!element) return;
    const update = () => document.documentElement.style.setProperty('--audio-bar-height', `${element.getBoundingClientRect().height}px`);
    update(); const observer = new ResizeObserver(update); observer.observe(element);
    return () => { observer.disconnect(); document.documentElement.style.removeProperty('--audio-bar-height'); };
  }, [!!e?.track]);
  if (!e?.track) return null;
  const tagLine = [tags.artist, tags.genre?.split(';').slice(0, 3).map(t => t.trim()).join(' · '), tags.lastfm_replay ? `重播 ${tags.lastfm_replay.toFixed(1)}` : '']
    .filter(Boolean).join('  ·  ');
  const tip = [tags.album ? `专辑《${tags.album}》` : '', tags.genre ? `风格：${tags.genre}` : '',
    tags.lastfm_listeners ? `Last.fm：听众 ${tags.lastfm_listeners}，播放 ${tags.lastfm_playcount}${tags.lastfm_replay ? `，听过的人平均听 ${tags.lastfm_replay.toFixed(1)} 遍` : ''}` : '',
    tags.source_views ? `来源：播放 ${tags.source_views}${tags.source_likes ? `，点赞 ${tags.source_likes}` : ''}` : '',
    tags.release_date ? `发布 ${tags.release_date}${tags.download_date ? `  ·  下载 ${tags.download_date}` : ''}` : '',
    `${e.albumTitle}（${e.position + 1}/${e.total}）`].filter(Boolean).join('\n');
  return <div ref={bar} className="audio-bar" aria-label="全局播放器">
    <button className="audio-bar-title" onClick={onOpen} title={tip} aria-label="打开正在收听">
      <span>{e.track.title}</span><small className={e.error ? 'text-danger' : ''}>{e.error || tagLine || `${e.albumTitle}（${e.position + 1}/${e.total}）`}</small>
    </button>
    <div className="audio-bar-transport">
      <button className={iconButton} onClick={e.previous} title="上一首" aria-label="上一首">⏮</button>
      <button className={`${iconButton} text-2xl`} onClick={e.toggle} aria-label={e.playing ? '暂停' : '播放'}>{e.playing ? 'Ⅱ' : '▶'}</button>
      <button className={iconButton} onClick={e.next} title="下一首" aria-label="下一首">⏭</button>
    </div>
    <div className="audio-bar-progress"><span>{formatTime(e.current)}</span><input type="range" aria-label="播放进度" min={0} max={Math.max(1, e.duration)} step={1} value={e.current} onChange={ev => e.seek(Number(ev.target.value))} /><span>{formatTime(e.duration)}</span></div>
    <details className="audio-bar-more"><summary>更多</summary><div className="audio-bar-menu">
      <MusicRatingButtons id={e.track.id} engine={e} />
      <button className={chip(false)} onClick={() => e.seekBy(-10)}>后退 10 秒</button>
      <button className={chip(false)} onClick={() => e.seekBy(30)}>前进 30 秒</button>
      <button className={chip(false)} onClick={e.cycleRate} title="倍速">{e.rate}×</button>
      <button className={chip(false)} onClick={e.cycleMode}>{modeLabel[e.mode]}</button>
      <button className={chip(e.sleepMinutes > 0)} onClick={e.cycleSleep} title="定时关闭">{e.sleepMinutes ? `${e.sleepMinutes} 分钟后关闭` : '定时关闭'}</button>
      <button className={chip(false)} onClick={() => e.stop()} title="停止并收起">停止并收起</button>
    </div></details>
  </div>;
}
