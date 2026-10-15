import { useEffect, useRef, useState } from 'react';
import { api } from './api';
import { playableCodecs } from './codecs';

type Creator = { id: string; platform: string; name: string; posts: number };
type Post = { id: string; creator_id: string; title: string; published: string; library_id: string };
type Slot = { channel: string; shuffle: boolean; muted: boolean; seed: number; index: number };
type Prefs = { screens: number; focusAudio: boolean; slots: Slot[] };
type Album = { id: string; title: string; track_count: number };
type Track = { title: string; rel_path: string };

const chip = (on: boolean) => `shrink-0 rounded-full border px-2.5 py-0.5 text-sm ${on ? 'border-primary text-primary' : 'border-line text-muted hover:text-foreground'}`;
const select = 'rounded border border-line bg-background px-2 py-0.5 text-sm';
const STORE = 'omnihub.matrix';
const newSlot = (): Slot => ({ channel: '', shuffle: true, muted: false, seed: Math.floor(Math.random() * 1e9), index: 0 });

// 偏好只是本浏览器的便利设置；读写失败（隐私模式等）时使用默认值。
function loadPrefs(): Prefs {
  try {
    const saved = JSON.parse(localStorage.getItem(STORE) ?? '') as Prefs;
    if (saved.slots?.length >= 3) return saved;
  } catch { /* 使用默认值 */ }
  return { screens: 3, focusAudio: true, slots: [newSlot(), newSlot(), newSlot()] };
}

// 只取竖屏，且只取本浏览器能解码的编码（否则会黑屏）。
function sequenceURL(slot: Slot) {
  const q = new URLSearchParams({ index: String(slot.index), seed: String(slot.seed), order: slot.shuffle ? 'shuffle' : 'sequential', portrait: '1', codecs: playableCodecs().join(',') });
  if (slot.channel.includes('/')) q.set('creator', slot.channel); else if (slot.channel) q.set('platform', slot.channel);
  return `/api/media/shortvideo/sequence?${q}`;
}

type SlotState = { post?: Post; total: number; error: string };

function VideoSlot({ active, playing, slot, audible, focused, onFocus, onEnded, onState }: { active: boolean; playing: boolean; slot: Slot; audible: boolean; focused: boolean; onFocus(): void; onEnded(): void; onState(s: SlotState): void }) {
  const video = useRef<HTMLVideoElement>(null);
  const report = useRef(onState);
  report.current = onState;
  const [state, setState] = useState<SlotState>({ total: 0, error: '' });
  const autoplay = useRef(playing);
  autoplay.current = playing;

  useEffect(() => {
    const player = video.current;
    if (!player || !active) return;
    const controller = new AbortController();
    const update = (s: SlotState) => { setState(s); report.current(s); };
    api<{ total: number; post: Post }>(sequenceURL(slot), { signal: controller.signal }).then(({ total, post }) => {
      player.src = `/api/media/shortvideo/stream?${new URLSearchParams({ library_id: post.library_id, id: post.id })}`;
      update({ post, total, error: '' });
      // 默认暂停停在第一帧；「全部播放」后换片继续播放。浏览器拦截自动播放时保持暂停。
      if (autoplay.current) player.play().catch(() => undefined);
    }).catch((e: Error) => { if (!controller.signal.aborted) update({ total: 0, error: e.message }); });
    return () => { controller.abort(); player.pause(); player.removeAttribute('src'); player.load(); };
  }, [active, slot.channel, slot.shuffle, slot.seed, slot.index]);

  const p = state.post;
  return <div className={`relative overflow-hidden rounded border-2 bg-black ${focused ? 'border-primary' : 'border-transparent'}`} onClick={onFocus}>
    <video ref={video} muted={!audible} playsInline preload="metadata" className="h-[calc(100vh-7rem)] w-full object-contain" onEnded={onEnded} />
    <p className={`absolute inset-x-0 bottom-0 truncate bg-black/60 px-2 py-0.5 text-xs ${state.error ? 'text-danger' : 'text-white'}`}>
      {audible ? '' : '🔇 '}{state.error || (p ? `${p.creator_id.split('/')[1]} · ${p.published} · ${p.title}（${slot.index + 1}/${state.total}）` : '加载中…')}
    </p>
  </div>;
}

// Accompany：音频伴听。选一个音频专辑后按顺序循环播放，期间所有视频静音。
function Accompany({ active, album }: { active: boolean; album?: Album }) {
  const audio = useRef<HTMLAudioElement>(null);
  const [track, setTrack] = useState(0);
  const [title, setTitle] = useState('');
  useEffect(() => setTrack(0), [album?.id]);
  useEffect(() => {
    const player = audio.current;
    if (!player || !active || !album) return;
    const controller = new AbortController();
    api<{ tracks: Track[] }>(`/api/media/audio/album?${new URLSearchParams({ path: album.id, limit: '1', offset: String(track) })}`, { signal: controller.signal })
      .then(({ tracks }) => {
        if (!tracks.length) return;
        setTitle(tracks[0].title);
        player.src = `/api/media/audio/stream?path=${encodeURIComponent(tracks[0].rel_path)}`;
        player.play().catch(() => undefined);
      }).catch(() => undefined);
    return () => { controller.abort(); player.pause(); player.removeAttribute('src'); player.load(); };
  }, [active, album, track]);
  if (!album) return null;
  return <span className="flex min-w-0 items-center gap-2 text-xs text-muted">
    <audio ref={audio} onEnded={() => setTrack(t => (t + 1) % Math.max(1, album.track_count))} />
    <button className={chip(false)} onClick={() => { const a = audio.current; if (a) { if (a.paused) void a.play(); else a.pause(); } }}>⏯</button>
    <span className="truncate">{track + 1}/{album.track_count} {title}</span>
  </span>;
}

// VideoMatrix：2/3 屏并排，顶栏操作焦点屏。点屏或按 1/2/3 选焦点；空格 暂停/继续，←/→ 上/下一条，M 静音。
// 隐藏时各屏释放解码器。
export default function VideoMatrix({ active }: { active: boolean }) {
  const [creators, setCreators] = useState<Creator[]>([]);
  const [albums, setAlbums] = useState<Album[]>([]);
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs);
  const [focused, setFocused] = useState(0);
  const [accompany, setAccompany] = useState<Album>();
  const [states, setStates] = useState<SlotState[]>([]);
  const [playing, setPlaying] = useState(false);
  // 顶栏：进入后显示 3 秒；之后鼠标移到顶边出现，离开 1.5 秒收起；📌 固定后常显。
  const [barShown, setBarShown] = useState(true);
  const [pinned, setPinned] = useState(false);
  const hideTimer = useRef<number | undefined>(undefined);
  const revealBar = (ms = 1500) => { setBarShown(true); window.clearTimeout(hideTimer.current); hideTimer.current = window.setTimeout(() => setBarShown(false), ms); };
  useEffect(() => { revealBar(3000); return () => window.clearTimeout(hideTimer.current); }, []);
  const container = useRef<HTMLDivElement>(null);

  useEffect(() => { try { localStorage.setItem(STORE, JSON.stringify(prefs)); } catch { /* 忽略 */ } }, [prefs]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Creator[]>('/api/media/shortvideo/creators', { signal: controller.signal }).then(setCreators).catch(() => undefined);
    api<Album[]>('/api/media/audio/albums?limit=100', { signal: controller.signal }).then(setAlbums).catch(() => undefined);
    return () => controller.abort();
  }, [active]);

  const setSlot = (i: number, change: Partial<Slot>) => setPrefs(p => ({ ...p, slots: p.slots.map((s, j) => j === i ? { ...s, ...change } : s) }));
  const step = (i: number, delta: number) => setPrefs(p => {
    const n = Math.max(1, states[i]?.total ?? 1);
    return { ...p, slots: p.slots.map((s, j) => j === i ? { ...s, index: (s.index + delta + n) % n } : s) };
  });
  const videoAt = (i: number) => container.current?.querySelectorAll('video')[i];
  const togglePause = (i: number) => { const v = videoAt(i); if (v) { if (v.paused) void v.play(); else v.pause(); } };
  const setAll = (on: boolean) => {
    setPlaying(on);
    container.current?.querySelectorAll('video').forEach(v => { if (on) void v.play().catch(() => undefined); else v.pause(); });
  };

  useEffect(() => {
    if (!active) return;
    const handler = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
      if (e.key >= '1' && e.key <= String(prefs.screens)) setFocused(Number(e.key) - 1);
      else if (e.key === ' ') togglePause(focused);
      else if (e.key === 'ArrowRight') step(focused, 1);
      else if (e.key === 'ArrowLeft') step(focused, -1);
      else if (e.key === 'm' || e.key === 'M') setSlot(focused, { muted: !prefs.slots[focused].muted });
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  });

  const cfg = prefs.slots[focused];
  const platforms = [...new Set(creators.map(c => c.platform))];
  return <section className="relative">
    <div className="absolute inset-x-0 top-0 z-20 h-3" onMouseEnter={() => revealBar()} onClick={() => revealBar()} />
    <div className={`absolute inset-x-0 top-0 z-30 flex flex-wrap items-center gap-1.5 bg-panel/90 px-2 py-1 transition-opacity ${pinned || barShown ? 'opacity-100' : 'pointer-events-none opacity-0'}`}
      onMouseEnter={() => { setBarShown(true); window.clearTimeout(hideTimer.current); }} onMouseLeave={() => revealBar()}>
      <button className={chip(prefs.screens === 2)} onClick={() => { setPrefs(p => ({ ...p, screens: 2 })); setFocused(f => Math.min(f, 1)); }}>2 屏</button>
      <button className={chip(prefs.screens === 3)} onClick={() => setPrefs(p => ({ ...p, screens: 3 }))}>3 屏</button>
      <button className={chip(prefs.focusAudio)} onClick={() => setPrefs(p => ({ ...p, focusAudio: !p.focusAudio }))}>焦点出声</button>
      <button className={chip(playing)} onClick={() => setAll(!playing)}>{playing ? '⏸ 全部暂停' : '▶ 全部播放'}</button>
      <span className="mx-1 h-4 w-px bg-line" />
      <span className="text-sm text-primary">屏 {focused + 1}</span>
      <select className={`${select} max-w-60`} value={cfg.channel} onChange={e => setSlot(focused, { channel: e.target.value, index: 0, seed: Math.floor(Math.random() * 1e9) })}>
        <option value="">全部</option>
        {platforms.map(p => <option key={p} value={p}>{p}</option>)}
        {creators.map(c => <option key={c.id} value={c.id}>{c.platform} · {c.name}（{c.posts}）</option>)}
      </select>
      <button className={chip(false)} onClick={() => setSlot(focused, { shuffle: !cfg.shuffle, index: 0 })}>{cfg.shuffle ? '随机' : '顺序'}</button>
      <button className={chip(false)} onClick={() => step(focused, -1)}>◀ 上一条</button>
      <button className={chip(false)} onClick={() => togglePause(focused)}>⏯</button>
      <button className={chip(false)} onClick={() => step(focused, 1)}>下一条 ▶</button>
      <button className={chip(cfg.muted)} onClick={() => setSlot(focused, { muted: !cfg.muted })}>{cfg.muted ? '🔇 已静音' : '🔊 声音'}</button>
      <span className="mx-1 h-4 w-px bg-line" />
      <select className={`${select} max-w-48`} value={accompany?.id ?? ''} onChange={e => setAccompany(albums.find(a => a.id === e.target.value))} title="音频伴听：播放专辑，视频全部静音">
        <option value="">伴听：关</option>
        {albums.map(a => <option key={a.id} value={a.id}>伴听：{a.title}（{a.track_count}）</option>)}
      </select>
      <Accompany active={active} album={accompany} />
      <button className={`${chip(pinned)} ml-auto`} onClick={() => setPinned(p => !p)} title="固定顶栏">📌</button>
    </div>
    <div ref={container} className={`grid gap-1 ${prefs.screens === 2 ? 'grid-cols-2' : 'grid-cols-3'}`}>
      {prefs.slots.slice(0, prefs.screens).map((slot, i) => <VideoSlot key={i} active={active} playing={playing} slot={slot} focused={focused === i}
        audible={!accompany && !slot.muted && (!prefs.focusAudio || focused === i)}
        onFocus={() => setFocused(i)} onEnded={() => step(i, 1)}
        onState={s => setStates(old => { const next = [...old]; next[i] = s; return next; })} />)}
    </div>
  </section>;
}
