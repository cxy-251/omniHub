import { useEffect, useRef, useState } from 'react';
import { api } from './api';

type Creator = { id: string; platform: string; name: string; posts: number };
type Post = { id: string; creator_id: string; title: string; published: string; library_id: string };
type Slot = { channel: string; shuffle: boolean; muted: boolean; seed: number; index: number };
type Prefs = { screens: number; focusAudio: boolean; slots: Slot[] };

const button = 'rounded-md border border-line px-3 py-2 text-sm hover:border-primary disabled:opacity-50';
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

function sequenceURL(slot: Slot) {
  const q = new URLSearchParams({ index: String(slot.index), seed: String(slot.seed), order: slot.shuffle ? 'shuffle' : 'sequential' });
  if (slot.channel.includes('/')) q.set('creator', slot.channel); else if (slot.channel) q.set('platform', slot.channel);
  return `/api/media/shortvideo/sequence?${q}`;
}

type SlotState = { post?: Post; total: number; paused: boolean; error: string };

function VideoSlot({ active, slot, audible, focused, onFocus, onEnded, onState }: { active: boolean; slot: Slot; audible: boolean; focused: boolean; onFocus(): void; onEnded(): void; onState(s: SlotState): void }) {
  const video = useRef<HTMLVideoElement>(null);
  const state = useRef(onState);
  state.current = onState;

  useEffect(() => {
    const player = video.current;
    if (!player || !active) return;
    const controller = new AbortController();
    api<{ total: number; post: Post }>(sequenceURL(slot), { signal: controller.signal }).then(async ({ total, post }) => {
      const q = new URLSearchParams({ library_id: post.library_id, id: post.id });
      player.src = `/api/media/shortvideo/stream?${q}`;
      state.current({ post, total, paused: false, error: '' });
      await player.play().catch(() => state.current({ post, total, paused: true, error: '' }));
    }).catch((e: Error) => { if (!controller.signal.aborted) state.current({ total: 0, paused: true, error: e.message }); });
    return () => { controller.abort(); player.pause(); player.removeAttribute('src'); player.load(); };
  }, [active, slot.channel, slot.shuffle, slot.seed, slot.index]);

  return <div className={`relative overflow-hidden rounded-lg border-2 bg-black ${focused ? 'border-primary' : 'border-transparent'}`} onClick={onFocus}>
    <video ref={video} muted={!audible} playsInline className="h-[78vh] w-full object-contain" onEnded={onEnded} />
    {!audible ? <span className="absolute bottom-2 left-2 text-xl">🔇</span> : null}
  </div>;
}

// VideoMatrix：2/3 屏并排，顶栏操作焦点屏。点屏或按 1/2/3 选焦点；空格 暂停/继续，←/→ 上/下一条，M 静音。
// 隐藏时各屏释放解码器。
export default function VideoMatrix({ active, onAccompany }: { active: boolean; onAccompany?: () => void }) {
  const [creators, setCreators] = useState<Creator[]>([]);
  const [prefs, setPrefs] = useState<Prefs>(loadPrefs);
  const [focused, setFocused] = useState(0);
  const [accompany, setAccompany] = useState(false);
  const [states, setStates] = useState<SlotState[]>([]);
  const container = useRef<HTMLDivElement>(null);

  useEffect(() => { try { localStorage.setItem(STORE, JSON.stringify(prefs)); } catch { /* 忽略 */ } }, [prefs]);
  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    api<Creator[]>('/api/media/shortvideo/creators', { signal: controller.signal }).then(setCreators).catch(() => undefined);
    return () => controller.abort();
  }, [active]);

  const setSlot = (i: number, change: Partial<Slot>) => setPrefs(p => ({ ...p, slots: p.slots.map((s, j) => j === i ? { ...s, ...change } : s) }));
  const step = (i: number, delta: number) => setPrefs(p => {
    const n = Math.max(1, states[i]?.total ?? 1);
    return { ...p, slots: p.slots.map((s, j) => j === i ? { ...s, index: (s.index + delta + n) % n } : s) };
  });
  const videoAt = (i: number) => container.current?.querySelectorAll('video')[i];
  const togglePause = (i: number) => { const v = videoAt(i); if (v) { if (v.paused) void v.play(); else v.pause(); } };

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
  const current = states[focused];
  const platforms = [...new Set(creators.map(c => c.platform))];
  return <section className="space-y-3">
    <div className="flex flex-wrap items-center gap-3 rounded-lg border border-line bg-panel p-2 text-sm">
      <select className="rounded border border-line bg-background p-2" value={prefs.screens} onChange={e => { const n = Number(e.target.value); setPrefs(p => ({ ...p, screens: n })); setFocused(f => Math.min(f, n - 1)); }}><option value={2}>2 屏</option><option value={3}>3 屏</option></select>
      <label className="flex items-center gap-2"><input type="checkbox" checked={prefs.focusAudio} onChange={e => setPrefs(p => ({ ...p, focusAudio: e.target.checked }))} />焦点出声</label>
      {onAccompany ? <label className="flex items-center gap-2"><input type="checkbox" checked={accompany} onChange={e => { setAccompany(e.target.checked); if (e.target.checked) onAccompany(); }} />音频伴听（视频静音）</label> : null}
      <span className="text-primary">屏 {focused + 1}：</span>
      <select className="max-w-72 rounded border border-line bg-background p-2" value={cfg.channel} onChange={e => setSlot(focused, { channel: e.target.value, index: 0, seed: Math.floor(Math.random() * 1e9) })}>
        <option value="">全部</option>
        {platforms.map(p => <option key={p} value={p}>{p}</option>)}
        {creators.map(c => <option key={c.id} value={c.id}>{c.platform} · {c.name}（{c.posts}）</option>)}
      </select>
      <button className={button} onClick={() => setSlot(focused, { shuffle: !cfg.shuffle, index: 0 })}>{cfg.shuffle ? '随机' : '顺序'}</button>
      <button className={button} onClick={() => step(focused, -1)}>上一条</button>
      <button className={button} onClick={() => togglePause(focused)}>暂停/播放</button>
      <button className={button} onClick={() => step(focused, 1)}>下一条</button>
      <button className={button} onClick={() => setSlot(focused, { muted: !cfg.muted })}>{cfg.muted ? '取消静音' : '静音'}</button>
      <span className="min-w-0 flex-1 truncate text-muted">{current?.error || (current?.post ? `${current.post.creator_id.split('/')[1]} · ${current.post.published} · ${current.post.title}（${cfg.index + 1}/${current.total}）` : '')}</span>
    </div>
    <div ref={container} className={`grid gap-2 ${prefs.screens === 2 ? 'grid-cols-2' : 'grid-cols-3'}`}>
      {prefs.slots.slice(0, prefs.screens).map((slot, i) => <VideoSlot key={i} active={active} slot={slot} focused={focused === i}
        audible={!accompany && !slot.muted && (!prefs.focusAudio || focused === i)}
        onFocus={() => setFocused(i)} onEnded={() => step(i, 1)}
        onState={s => setStates(old => { const next = [...old]; next[i] = s; return next; })} />)}
    </div>
  </section>;
}
