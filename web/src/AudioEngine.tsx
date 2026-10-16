import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api, post, request } from './api';
import { chip, iconButton, store, stored } from './ui';

// 音声播放引擎（全局一份，切换页面不中断；与桌面端同一套断点接口）。
// 断点按账号存服务端：听了不到 3 秒不记，离结尾不到 5 秒算听完。分集按专辑内序号向服务端逐条取。
// 模式：list 列表循环 / single 单集循环 / random 随机。浏览器的 Media Session 接入锁屏与耳机按键（手机上有用）。
export type Track = { index: number; title: string; id: string; ext: string; offset: number; duration: number };
type Mode = 'list' | 'single' | 'random';
const speeds = [1, 1.25, 1.5, 1.75, 2, 0.75];
const sleepChoices = [0, 15, 30, 45, 60];

type Engine = {
  albumId: string; albumTitle: string; track?: Track; position: number; total: number;
  playing: boolean; current: number; duration: number; rate: number; mode: Mode; sleepMinutes: number; error: string;
  open(album: string, title: string, pos: number, autoplay: boolean): void;
  continueAlbum(album: string, title: string): void;
  toggle(): void; seek(seconds: number): void; seekBy(delta: number): void;
  next(): void; previous(): void; stop(): void;
  cycleRate(): void; cycleMode(): void; cycleSleep(): void;
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
      if (!list.length) { setError('这张专辑没有可播放的分集'); return; }
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
  const stop = useCallback(() => {
    save();
    const a = audio.current;
    if (a) { a.pause(); a.removeAttribute('src'); a.load(); }
    setTrack(undefined); setPosition(-1); setSleepMinutes(0); setSleepAt(0);
    store('omnihub.audio.last', null);
  }, [save]);

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
    toggle: () => { const a = audio.current; if (!a) return; if (a.paused) void a.play(); else a.pause(); },
    seek: s => { if (audio.current) audio.current.currentTime = s; },
    seekBy: d => { if (audio.current) audio.current.currentTime = Math.max(0, audio.current.currentTime + d); },
    cycleRate: () => setRate(r => speeds[(speeds.indexOf(r) + 1) % speeds.length]),
    cycleMode: () => setMode(m => (['list', 'single', 'random'] as Mode[])[(['list', 'single', 'random'].indexOf(m) + 1) % 3]),
    cycleSleep: () => {
      const n = sleepChoices[(sleepChoices.indexOf(sleepMinutes) + 1) % sleepChoices.length];
      setSleepMinutes(n); setSleepAt(n ? Date.now() + n * 60000 : 0);
    },
  }), [albumId, albumTitle, track, position, total, playing, current, duration, rate, mode, sleepMinutes, error, open, continueAlbum, next, previous, stop]);

  return <Context.Provider value={engine}>
    <audio ref={audio} preload="metadata"
      onPlay={() => setPlaying(true)} onPause={() => { setPlaying(false); save(); }} onEnded={ended}
      onTimeUpdate={e => setCurrent(e.currentTarget.currentTime)} onDurationChange={e => setDuration(Number.isFinite(e.currentTarget.duration) ? e.currentTarget.duration : 0)}
      onError={() => setError('音频无法播放')} />
    {children}
  </Context.Provider>;
}

const modeLabel: Record<Mode, string> = { list: '🔁 列表', single: '🔂 单集', random: '🔀 随机' };

// AudioBar：底部播放条，音声播放时在所有页面显示。
export function AudioBar({ onOpen }: { onOpen(): void }) {
  const e = useAudio();
  if (!e?.track) return null;
  return <div className="fixed inset-x-0 bottom-0 z-40 flex flex-wrap items-center gap-x-2 gap-y-1 border-t border-line bg-panel px-3 py-1.5">
    <button className="min-w-0 max-w-64 flex-1 text-left" onClick={onOpen}>
      <p className="truncate text-sm">{e.track.title}</p>
      <p className={`truncate text-xs ${e.error ? 'text-danger' : 'text-muted'}`}>{e.error || `${e.albumTitle}（${e.position + 1}/${e.total}）`}</p>
    </button>
    <div className="flex items-center">
      <button className={iconButton} onClick={e.previous} title="上一集">⏮</button>
      <button className={iconButton} onClick={() => e.seekBy(-10)} title="后退 10 秒">↺</button>
      <button className={`${iconButton} text-2xl`} onClick={e.toggle}>{e.playing ? '⏸' : '▶'}</button>
      <button className={iconButton} onClick={() => e.seekBy(30)} title="前进 30 秒">↻</button>
      <button className={iconButton} onClick={e.next} title="下一集">⏭</button>
    </div>
    <span className="font-mono text-xs text-muted">{formatTime(e.current)}</span>
    <input type="range" className="min-w-24 flex-[2] accent-[#58a6ff]" min={0} max={Math.max(1, e.duration)} step={1} value={e.current} onChange={ev => e.seek(Number(ev.target.value))} />
    <span className="font-mono text-xs text-muted">{formatTime(e.duration)}</span>
    <button className={chip(false)} onClick={e.cycleRate} title="倍速">{e.rate}×</button>
    <button className={chip(false)} onClick={e.cycleMode}>{modeLabel[e.mode]}</button>
    <button className={chip(e.sleepMinutes > 0)} onClick={e.cycleSleep} title="定时关闭">🌙{e.sleepMinutes ? ` ${e.sleepMinutes}′` : ''}</button>
    <button className={iconButton} onClick={e.stop} title="停止并收起">✕</button>
  </div>;
}
