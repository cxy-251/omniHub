import { useCallback, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { playableCodecs } from './codecs';
import { feedPath, feedQuery, newSeed, sendEvent, trash as trashItem, like, useGroups, type Domain } from './feed';
import { useAudio } from './AudioEngine';
import { chip, select, store, stored } from './ui';

// 多联放映（短视频与相册共用，与桌面端一致）：2/3 屏并排，只放竖屏内容。
//   shortvideo：视频（只取本浏览器能解码的编码），播完换下一条；可焦点出声、静音
//   gallery：图片，按设定秒数自动轮播
// 每屏独立频道；顶栏操作焦点屏（点屏或 1/2/3 选屏）。点赞、回收站、伴听（全局音声，伴听时视频静音）、记住每屏位置。
type Item = { id: string; library_id: string; liked?: boolean; creator_id?: string; title?: string; published?: string };
type Slot = { channel: string; shuffle: boolean; muted: boolean; seed: number; index: number; itemId: string; pos: number };
type Prefs = { screens: number; focusAudio: boolean; slideSeconds: number; slots: Slot[] };
type Album = { id: string; title: string };

const newSlot = (): Slot => ({ channel: 'recommend', shuffle: true, muted: false, seed: newSeed(), index: 0, itemId: '', pos: 0 });
const shortcuts: [string, string, boolean?][] = [
  ['1 / 2 / 3', '选焦点屏'], ['Z', '焦点屏点赞 / 取消'], ['← / →', '焦点屏上一条 / 下一条'], ['↑ / ↓', '焦点屏后退 / 前进 5 秒', true],
  ['空格', '焦点屏暂停 / 继续'], ['P', '全部播放 / 全部暂停'], ['Delete', '焦点屏移到回收站（3 秒内按两次）'], ['M', '焦点屏静音 / 取消', true],
  ['B', '伴听暂停 / 继续'], ['Tab', '固定 / 收起顶栏'], ['F', '全屏'], ['? 或 H', '显示 / 隐藏快捷键说明'], ['Esc', '关闭说明 / 退出全屏 / 返回浏览'],
];

type SlotHandle = { step(delta: number, auto?: boolean): void; togglePause(): void; toggleLike(): void; trash(): void; seekBy(s: number): void; item?: Item };

function Screen({ domain, slot, index, focused, playing, audible, slideSeconds, onFocus, onChange, register }: {
  domain: Domain; slot: Slot; index: number; focused: boolean; playing: boolean; audible: boolean; slideSeconds: number;
  onFocus(): void; onChange(change: Partial<Slot>): void; register(i: number, h: SlotHandle | undefined): void;
}) {
  const isVideo = domain === 'shortvideo';
  const video = useRef<HTMLVideoElement>(null);
  const [item, setItem] = useState<Item>();
  const [total, setTotal] = useState(0);
  const [error, setError] = useState('');
  const [paused, setPaused] = useState(!playing);
  const [loadedImage, setLoadedImage] = useState('');
  const [reload, setReload] = useState(0);   // 移到回收站后重取同一序号（序列少了一条，就是下一条）
  const shownAt = useRef(0);
  const live = useRef({ slot, item, total, paused });
  live.current = { slot, item, total, paused };
  useEffect(() => setPaused(!playing), [playing]);

  // 频道、顺序、序号或种子变化时重新取。
  useEffect(() => {
    const controller = new AbortController();
    const resume = slot.itemId && slot.pos > 1 ? { id: slot.itemId, pos: slot.pos } : undefined;
    setError(''); setItem(undefined);
    const q = feedQuery({ channel: slot.channel, seed: slot.seed, index: slot.index, portrait: true,
      kind: isVideo ? 'video' : undefined, codecs: isVideo ? playableCodecs().join(',') : undefined,
      order: slot.shuffle && slot.channel !== 'recommend' ? 'shuffle' : undefined });
    api<{ total: number; item: Item }>(`${feedPath(domain, 'sequence')}?${q}`, { signal: controller.signal }).then(r => {
      setTotal(r.total); setItem(r.item); shownAt.current = Date.now();
      sendEvent(domain, r.item.id, 'view');
      const v = video.current;
      if (isVideo && v) {
        v.src = `/api/media/shortvideo/stream?${new URLSearchParams({ library_id: r.item.library_id, id: r.item.id })}`;
        v.onloadedmetadata = () => { if (resume && resume.id === r.item.id) v.currentTime = resume.pos; };
        if (!live.current.paused) v.play().catch(() => undefined);
      }
    }).catch((e: Error) => { if (!controller.signal.aborted) setError(e.message); });
    return () => { controller.abort(); const v = video.current; if (v) { v.pause(); v.removeAttribute('src'); v.load(); } };
  }, [domain, isVideo, slot.channel, slot.shuffle, slot.seed, slot.index, reload]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { const v = video.current; if (v && v.src) { if (paused) v.pause(); else v.play().catch(() => undefined); } }, [paused]);
  // 记住位置（每 10 秒、以及卸载时）
  useEffect(() => {
    const keep = () => { const { item: it } = live.current; if (it) onChange({ itemId: it.id, pos: Math.floor(video.current?.currentTime ?? 0) }); };
    const timer = window.setInterval(keep, 10000);
    return () => { window.clearInterval(timer); keep(); };
  }, [item?.id]); // eslint-disable-line react-hooks/exhaustive-deps
  // 图片轮播
  useEffect(() => {
    if (isVideo || paused || !item || loadedImage !== item.id) return;
    const timer = window.setTimeout(() => step(1, true), slideSeconds * 1000);
    return () => window.clearTimeout(timer);
  }, [isVideo, paused, item, loadedImage, slideSeconds]); // eslint-disable-line react-hooks/exhaustive-deps

  const step = useCallback((delta: number, auto = false) => {
    const { slot: s, item: it, total: n } = live.current;
    if (it && !auto && delta > 0 && Date.now() - shownAt.current < 2000) sendEvent(domain, it.id, 'skip');
    const count = Math.max(1, n);
    onChange({ index: (s.index + delta + count) % count, itemId: '', pos: 0 });
  }, [domain, onChange]);
  useEffect(() => {
    register(index, {
      item,
      step,
      togglePause: () => setPaused(p => !p),
      seekBy: s => { if (video.current) video.current.currentTime += s; },
      toggleLike: () => { const it = live.current.item; if (it) like(domain, it.id, !it.liked).then(() => setItem(cur => cur && cur.id === it.id ? { ...cur, liked: !it.liked } : cur)).catch(() => undefined); },
      trash: () => {
        const it = live.current.item;
        if (!it) return;
        video.current?.removeAttribute('src');
        trashItem(domain, it.id).then(() => { onChange({ itemId: '', pos: 0 }); setReload(n => n + 1); })
          .catch((e: Error) => setError(e.message));
      },
    });
    return () => register(index, undefined);
  }, [index, item, step, domain, onChange, register]);

  const caption = !item ? (error || '加载中…') : isVideo
    ? `${item.creator_id?.split('/')[1]} · ${item.published} · ${item.title}（${slot.index + 1}/${total}）` : `${slot.index + 1} / ${total}`;
  const original = item ? `/api/media/gallery/original?${new URLSearchParams({ library_id: item.library_id, id: item.id })}` : '';
  return <div className={`relative min-w-0 flex-1 overflow-hidden rounded border-2 bg-black ${focused ? 'border-primary' : 'border-transparent'}`} onClick={onFocus}>
    {isVideo
      ? <video ref={video} muted={!audible} playsInline preload="metadata" className="h-full w-full object-contain" onEnded={() => { sendEvent(domain, live.current.item?.id, 'complete'); step(1, true); }} />
      : item ? <img key={item.id} className="h-full w-full object-contain" src={original} alt="" onLoad={() => setLoadedImage(item.id)} /> : null}
    <p className={`absolute inset-x-0 bottom-0 truncate bg-black/60 px-2 py-0.5 text-xs ${error ? 'text-danger' : 'text-white'}`}>
      {isVideo && !audible ? '🔇 ' : ''}{item?.liked ? '♥ ' : ''}{caption}
    </p>
  </div>;
}

export default function Matrix({ domain, onExit }: { domain: Domain; onExit(): void }) {
  const isVideo = domain === 'shortvideo';
  const storeKey = isVideo ? 'omnihub.matrix' : 'omnihub.matrix.gallery';
  const [prefs, setPrefs] = useState<Prefs>(() => {
    const saved = stored<Prefs | null>(storeKey, null);
    const slots = (saved?.slots ?? []).map(s => ({ ...newSlot(), ...s, channel: s.channel || 'recommend' })).slice(0, 3);
    while (slots.length < 3) slots.push(newSlot());
    return { screens: saved?.screens ?? 3, focusAudio: saved?.focusAudio ?? true, slideSeconds: saved?.slideSeconds ?? 5, slots };
  });
  useEffect(() => store(storeKey, prefs), [storeKey, prefs]);
  const audio = useAudio();
  const accompanying = !!audio?.playing;
  const { groups, pins } = useGroups(domain, true);
  const [albums, setAlbums] = useState<Album[]>([]);
  useEffect(() => { api<Album[]>('/api/media/audio/albums?limit=100').then(setAlbums).catch(() => undefined); }, []);
  const [focused, setFocused] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [help, setHelp] = useState(false);
  const [toast, setToast] = useState(() => stored('omnihub.matrix.seenHelp', false) ? '' : '按 ? 查看快捷键');
  useEffect(() => store('omnihub.matrix.seenHelp', true), []);
  const [trashArmed, setTrashArmed] = useState(false);
  const [barShown, setBarShown] = useState(true);
  const [pinned, setPinned] = useState(false);
  const hideTimer = useRef<number | undefined>(undefined);
  const revealBar = (ms = 1500) => { setBarShown(true); window.clearTimeout(hideTimer.current); hideTimer.current = window.setTimeout(() => setBarShown(false), ms); };
  useEffect(() => { revealBar(3000); return () => window.clearTimeout(hideTimer.current); }, []);
  useEffect(() => { if (!toast) return; const t = window.setTimeout(() => setToast(''), 2200); return () => window.clearTimeout(t); }, [toast]);
  useEffect(() => { if (!trashArmed) return; const t = window.setTimeout(() => setTrashArmed(false), 3000); return () => window.clearTimeout(t); }, [trashArmed]);
  const container = useRef<HTMLDivElement>(null);
  const handles = useRef<(SlotHandle | undefined)[]>([]);
  const [, setRevision] = useState(0);
  const register = useCallback((i: number, h: SlotHandle | undefined) => { handles.current[i] = h; setRevision(r => r + 1); }, []);
  const setSlot = useCallback((i: number, change: Partial<Slot>) => setPrefs(p => ({ ...p, slots: p.slots.map((s, j) => j === i ? { ...s, ...change } : s) })), []);
  const changers = useRef<((c: Partial<Slot>) => void)[]>([]);
  for (let i = 0; i < 3; i++) changers.current[i] ??= (c: Partial<Slot>) => setSlot(i, c);
  const cfg = prefs.slots[focused];
  const focusedHandle = handles.current[focused];

  const channels = [{ value: 'recommend', label: '推荐' }, { value: 'liked', label: '♥ 我的点赞' }];
  if (isVideo) for (const p of [...new Set(groups.map(g => g.platform))]) if (p) channels.push({ value: `tag:platform:${p}`, label: p });
  const groupLabel = (g: { label: string; platform?: string; count: number }) => `${g.platform ? `${g.platform} · ` : ''}${g.label}（${g.count}）`;
  for (const id of pins) { const g = groups.find(x => x.id === id); if (g) channels.push({ value: `group:${g.id}`, label: `📌 ${groupLabel(g)}` }); }
  for (const g of groups) if (!pins.includes(g.id)) channels.push({ value: `group:${g.id}`, label: groupLabel(g) });

  const trashFocused = () => {
    if (!focusedHandle?.item) return;
    if (trashArmed) { setTrashArmed(false); focusedHandle.trash(); setToast('已移到回收站'); } else { setTrashArmed(true); setToast('再按一次移到回收站'); }
  };
  const toggleFullscreen = () => { if (document.fullscreenElement) void document.exitFullscreen(); else void container.current?.requestFullscreen(); };
  const keys = useRef<(e: KeyboardEvent) => void>(() => undefined);
  keys.current = (e: KeyboardEvent) => {
    const h = handles.current[focused];
    const k = e.key;
    if (k === 'Escape') { if (help) setHelp(false); else if (!document.fullscreenElement) onExit(); return; }
    if (k === '?' || k === 'h' || k === 'H' || k === '/') setHelp(v => !v);
    else if (k === 'Tab') setPinned(v => !v);
    else if (k === 'f' || k === 'F') toggleFullscreen();
    else if (k >= '1' && k <= String(prefs.screens)) setFocused(Number(k) - 1);
    else if (k === 'p' || k === 'P') setPlaying(v => !v);
    else if (k === 'b' || k === 'B') { if (audio?.track) audio.toggle(); else setToast('先在顶栏选一张伴听专辑'); }
    else if (!h) return;
    else if (k === ' ') h.togglePause();
    else if (k === 'ArrowRight') h.step(1);
    else if (k === 'ArrowLeft') h.step(-1);
    else if ((k === 'ArrowUp' || k === 'ArrowDown') && isVideo) h.seekBy(k === 'ArrowUp' ? -5 : 5);
    else if (k === 'z' || k === 'Z') { h.toggleLike(); if (h.item) setToast(h.item.liked ? '已取消点赞' : `♥ 已点赞（屏 ${focused + 1}）`); }
    else if (k === 'Delete') trashFocused();
    else if ((k === 'm' || k === 'M') && isVideo) setSlot(focused, { muted: !prefs.slots[focused].muted });
    else return;
    e.preventDefault();
  };
  useEffect(() => {
    const handler = (e: KeyboardEvent) => { if ((e.target as HTMLElement)?.tagName === 'SELECT' || (e.target as HTMLElement)?.tagName === 'INPUT') return; keys.current(e); };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, []);

  return <div ref={container} className="relative flex h-[calc(100vh-6rem)] flex-col bg-black">
    <div className="absolute inset-x-0 top-0 z-10 h-3" onMouseEnter={() => revealBar()} onClick={() => revealBar()} />
    <div className={`absolute inset-x-0 top-0 z-20 flex flex-wrap items-center gap-1.5 bg-panel/95 px-2 py-1 ${pinned || barShown ? '' : 'hidden'}`}
      onMouseEnter={() => { setBarShown(true); window.clearTimeout(hideTimer.current); }} onMouseLeave={() => revealBar()}>
      <button className={chip(false)} onClick={onExit}>浏览</button>
      <button className={chip(prefs.screens === 2)} onClick={() => { setPrefs(p => ({ ...p, screens: 2 })); setFocused(f => Math.min(f, 1)); }}>2 屏</button>
      <button className={chip(prefs.screens === 3)} onClick={() => setPrefs(p => ({ ...p, screens: 3 }))}>3 屏</button>
      {isVideo ? <button className={chip(prefs.focusAudio)} onClick={() => setPrefs(p => ({ ...p, focusAudio: !p.focusAudio }))}>焦点出声</button>
        : <button className={chip(false)} onClick={() => setPrefs(p => ({ ...p, slideSeconds: [3, 5, 8, 15][([3, 5, 8, 15].indexOf(p.slideSeconds) + 1) % 4] }))}>{prefs.slideSeconds} 秒/张</button>}
      <button className={chip(playing)} onClick={() => setPlaying(v => !v)} title="P">{playing ? '⏸ 全部暂停' : '▶ 全部播放'}</button>
      <span className="text-sm text-primary">屏 {focused + 1}</span>
      <select className={`${select} max-w-60`} value={cfg.channel} onChange={e => setSlot(focused, { channel: e.target.value, index: 0, itemId: '', pos: 0, seed: newSeed() })}>
        {channels.map(c => <option key={c.value} value={c.value}>{c.label}</option>)}
      </select>
      {cfg.channel !== 'recommend' ? <button className={chip(false)} onClick={() => setSlot(focused, { shuffle: !cfg.shuffle, index: 0, itemId: '', pos: 0 })}>{cfg.shuffle ? '随机' : '顺序'}</button> : null}
      <button className={chip(false)} onClick={() => focusedHandle?.step(-1)} title="←">◀</button>
      <button className={chip(false)} onClick={() => focusedHandle?.togglePause()} title="空格">⏯</button>
      <button className={chip(false)} onClick={() => focusedHandle?.step(1)} title="→">▶▶</button>
      {isVideo ? <button className={chip(cfg.muted)} onClick={() => setSlot(focused, { muted: !cfg.muted })} title="M">{cfg.muted ? '🔇' : '🔊'}</button> : null}
      <button className={chip(!!focusedHandle?.item?.liked)} disabled={!focusedHandle?.item} onClick={() => focusedHandle?.toggleLike()} title="Z">{focusedHandle?.item?.liked ? '♥ 已赞' : '♡ 点赞'}</button>
      <button className={chip(trashArmed)} disabled={!focusedHandle?.item} onClick={trashFocused} title="Delete 按两次">{trashArmed ? '再点一次确认' : '🗑 回收站'}</button>
      {albums.length ? <select className={`${select} max-w-44`} value={accompanying ? audio!.albumId : ''} title="伴听：选一张音声专辑（B 暂停/继续）" onChange={e => {
        const a = albums.find(x => x.id === e.target.value);
        if (!a) audio?.track && audio.playing && audio.toggle();
        else if (audio?.albumId === a.id && audio.track) { if (!audio.playing) audio.toggle(); }
        else audio?.continueAlbum(a.id, a.title);
      }}>
        <option value="">🎧 伴听：关</option>
        {albums.map(a => <option key={a.id} value={a.id}>🎧 {a.title}</option>)}
      </select> : null}
      <button className={`${chip(help)} ml-auto`} onClick={() => setHelp(v => !v)} title="? 或 H">⌨ 快捷键</button>
      <button className={chip(false)} onClick={toggleFullscreen} title="F">全屏</button>
      <button className={chip(pinned)} onClick={() => setPinned(v => !v)} title="Tab">📌</button>
    </div>
    <div className="flex min-h-0 flex-1 gap-0.5">
      {prefs.slots.slice(0, prefs.screens).map((s, i) => <Screen key={i} domain={domain} slot={s} index={i} focused={focused === i} playing={playing}
        audible={isVideo && !accompanying && !s.muted && (!prefs.focusAudio || focused === i)} slideSeconds={prefs.slideSeconds}
        onFocus={() => setFocused(i)} onChange={changers.current[i]} register={register} />)}
    </div>
    {toast ? <div className="absolute bottom-10 left-1/2 z-30 -translate-x-1/2 rounded-full border border-primary bg-panel px-4 py-1.5 text-sm">{toast}</div> : null}
    {help ? <div className="absolute inset-0 z-30 flex items-center justify-center bg-black/40" onClick={() => setHelp(false)}>
      <div className="rounded-lg border border-line bg-background p-5">
        <p className="mb-3 font-semibold">快捷键（作用于焦点屏，蓝框的那一屏）</p>
        {shortcuts.filter(s => isVideo || !s[2]).map(([key, desc]) => <p key={key} className="flex items-center gap-3 py-0.5 text-sm">
          <kbd className="w-24 rounded border border-line bg-panel py-0.5 text-center font-mono text-primary">{key}</kbd>{desc}</p>)}
        <p className="mt-2 text-xs text-muted">按 Esc 或点击任意处关闭</p>
      </div>
    </div> : null}
  </div>;
}
