import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api } from './api';
import { formatTime, useAudio, type Track } from './AudioEngine';
import { chip, select } from './ui';

type Artist = {
  id: string;
  title: string;
  single_count: number;
  album_count: number;
  track_count: number;
  cover_path?: string;
  main_artist: boolean;
};

type Section = {
  album: string;
  id: string;
  offset: number;
  count: number;
  cover_path?: string;
};

type GalleryRecord = {
  id: string;
  title: string;
  artist: string;
  artistId: string;
  coverUrl: string;
  trackCount: number;
  accentColor: string;
  isAlbum: boolean;
  offset: number;
};

type PlayerTheme = {
  name: string;
  swatch: string;
  background: string;
  disc: string;
  discCore: string;
  ink: 'dark' | 'light';
};

const PLAYER_THEMES: PlayerTheme[] = [
  {
    name: '曜石黑胶',
    swatch: '#221f22',
    background: 'linear-gradient(135deg, #101218 0%, #1f1b24 52%, #0b0d12 100%)',
    disc: 'rgba(18, 18, 20, 0.88)',
    discCore: 'rgba(218, 178, 96, 0.92)',
    ink: 'light',
  },
  {
    name: '暖陶珊瑚',
    swatch: '#e76255',
    background: 'linear-gradient(135deg, #ead4dc 0%, #f6edf0 48%, #e4d5ca 100%)',
    disc: 'rgba(64, 58, 58, 0.72)',
    discCore: 'rgba(232, 197, 116, 0.92)',
    ink: 'dark',
  },
  {
    name: '琥珀流金',
    swatch: '#efb35f',
    background: 'linear-gradient(135deg, #f4d8b9 0%, #edc071 45%, #f8ecd3 100%)',
    disc: 'rgba(148, 96, 27, 0.68)',
    discCore: 'rgba(255, 237, 184, 0.92)',
    ink: 'dark',
  },
  {
    name: '青瓷秘色',
    swatch: '#8db5a9',
    background: 'linear-gradient(135deg, #dee9e4 0%, #aac9bf 50%, #ecf1ed 100%)',
    disc: 'rgba(56, 89, 80, 0.68)',
    discCore: 'rgba(201, 232, 219, 0.92)',
    ink: 'dark',
  },
];

const ACCENT_PALETTE = ['#e76255', '#58a6ff', '#efb35f', '#3fb950', '#a371f7', '#f778ba', '#79c0ff', '#d29922'];

const coverUrlOf = (path?: string) => (path ? `/api/media/audio/cover?path=${encodeURIComponent(path)}` : '');

const clampIndex = (val: number, len: number) => Math.max(0, Math.min(Math.max(0, len - 1), val));

const buildAlbumTransform = (offset: number, playerOpen: boolean) => {
  const clamped = Math.max(-5, Math.min(5, offset));
  const abs = Math.abs(clamped);
  const side = clamped < 0 ? -1 : 1;
  const x = clamped * 176 - side * Math.max(0, abs - 1) * 28;
  const z = -abs * 148;
  const rotateY = Math.abs(clamped) < 0.05 ? 0 : side * -54;
  const presence = playerOpen ? 1 : 0;
  const scale = Math.max(0.54, 1.08 - abs * 0.1) * (1 - presence * (abs < 0.4 ? 0.1 : 0.03));
  const y = abs * 12 + presence * (abs < 0.4 ? -18 : 6);
  return `translate3d(${x}px, ${y}px, ${z}px) rotateY(${rotateY}deg) scale(${scale})`;
};

const VINYL_STYLES = `
  .oh-vinyl-perspective {
    perspective: 1120px;
    transform-style: preserve-3d;
  }
  .oh-vinyl-case {
    transform-style: preserve-3d;
    transition: transform 260ms cubic-bezier(0.22, 1, 0.36, 1), opacity 220ms ease, filter 220ms ease;
  }
  .oh-vinyl-case-box::before {
    position: absolute;
    inset: 4px -14px 8px auto;
    z-index: -1;
    width: 16px;
    content: "";
    transform: rotateY(68deg) translateZ(-8px);
    transform-origin: left center;
    border-radius: 0 6px 6px 0;
    background: linear-gradient(90deg, rgba(255,255,255,0.28), rgba(0,0,0,0.38)), var(--album-accent, #58a6ff);
  }
  .oh-vinyl-disc {
    background:
      radial-gradient(circle at 50% 50%, transparent 0 13%, var(--player-disc-core, rgba(218, 178, 96, 0.92)) 13.4% 28%, transparent 28.5%),
      repeating-radial-gradient(circle at 50% 50%, rgba(255, 255, 255, 0.14) 0 1px, transparent 1px 6px),
      radial-gradient(circle at 36% 26%, rgba(255, 255, 255, 0.25), transparent 24%),
      radial-gradient(circle, rgba(255, 255, 255, 0.12), var(--player-disc, rgba(18, 18, 20, 0.88)) 54%, rgba(0, 0, 0, 0.86) 100%);
    box-shadow:
      0 36px 80px rgba(0, 0, 0, 0.38),
      inset 0 0 0 1px rgba(255, 255, 255, 0.2),
      inset 0 0 36px rgba(255, 255, 255, 0.12);
  }
  .oh-vinyl-highlight {
    background: conic-gradient(from 230deg, transparent 0deg, rgba(255, 255, 255, 0.26) 28deg, transparent 68deg, transparent 360deg);
    mix-blend-mode: screen;
    opacity: 0.56;
  }
  .oh-vinyl-tonearm {
    transform-origin: 74% 16%;
    transition: transform 350ms cubic-bezier(0.22, 1, 0.36, 1);
  }
`;

export default function VinylGallery() {
  const engine = useAudio();
  const [artists, setArtists] = useState<Artist[]>([]);
  const [artistFilter, setArtistFilter] = useState<string>('');
  const [sectionsByArtist, setSectionsByArtist] = useState<Record<string, Section[]>>({});
  const [galleryIndex, setGalleryIndex] = useState(0);
  const [playerOpen, setPlayerOpen] = useState(false);
  const [themeIndex, setThemeIndex] = useState(0);
  const [tracks, setTracks] = useState<Track[]>([]);
  const [loadingTracks, setLoadingTracks] = useState(false);
  const [spinAngle, setSpinAngle] = useState(0);
  const dragRef = useRef({ active: false, moved: false, startX: 0, startIndex: 0 });

  useEffect(() => {
    const controller = new AbortController();
    api<Artist[]>('/api/media/audio/artists', { signal: controller.signal })
      .then(list => setArtists(list ?? []))
      .catch(() => undefined);
    return () => controller.abort();
  }, [engine?.musicCatalogVersion]);

  // 为选中的歌手（或默认常听歌手前 8 位）加载专辑目录，避免全库 N+1 风暴
  const targetArtists = useMemo(() => {
    if (artistFilter) return artists.filter(a => a.id === artistFilter);
    const mains = artists.filter(a => a.main_artist);
    return (mains.length ? mains : artists).slice(0, 12);
  }, [artists, artistFilter]);

  useEffect(() => {
    const missing = targetArtists.filter(a => a.album_count > 0 && !sectionsByArtist[a.id]);
    if (!missing.length) return;
    const controller = new AbortController();
    Promise.all(
      missing.map(a =>
        api<{ sections: Section[] }>(`/api/media/audio/artist?path=${encodeURIComponent(a.id)}`, { signal: controller.signal })
          .then(res => [a.id, (res.sections ?? []).filter(s => s.id && s.album)] as const)
          .catch(() => [a.id, [] as Section[]] as const)
      )
    ).then(entries => {
      if (controller.signal.aborted) return;
      setSectionsByArtist(prev => {
        const next = { ...prev };
        for (const [id, list] of entries) next[id] = list;
        return next;
      });
    });
    return () => controller.abort();
  }, [targetArtists, sectionsByArtist]);

  const records = useMemo<GalleryRecord[]>(() => {
    const list: GalleryRecord[] = [];
    let colorIdx = 0;
    const sourceArtists = artistFilter ? artists.filter(a => a.id === artistFilter) : (artists.filter(a => a.main_artist).length ? artists.filter(a => a.main_artist) : artists);
    for (const artist of sourceArtists) {
      const secs = sectionsByArtist[artist.id] ?? [];
      for (const sec of secs) {
        list.push({
          id: sec.id,
          title: sec.album,
          artist: artist.title,
          artistId: artist.id,
          coverUrl: coverUrlOf(sec.cover_path || artist.cover_path),
          trackCount: sec.count,
          accentColor: ACCENT_PALETTE[colorIdx++ % ACCENT_PALETTE.length],
          isAlbum: true,
          offset: sec.offset,
        });
      }
      if (secs.length === 0 && artist.track_count > 0) {
        list.push({
          id: artist.id,
          title: `${artist.title} · 精选集`,
          artist: artist.title,
          artistId: artist.id,
          coverUrl: coverUrlOf(artist.cover_path),
          trackCount: artist.track_count,
          accentColor: ACCENT_PALETTE[colorIdx++ % ACCENT_PALETTE.length],
          isAlbum: false,
          offset: 0,
        });
      }
    }
    return list;
  }, [artists, artistFilter, sectionsByArtist]);

  const activeIndex = clampIndex(Math.round(galleryIndex), records.length);
  const activeRecord = records[activeIndex];
  const theme = PLAYER_THEMES[themeIndex % PLAYER_THEMES.length];
  const lightMode = theme.ink === 'dark';

  useEffect(() => {
    if (galleryIndex >= records.length && records.length > 0) {
      setGalleryIndex(records.length - 1);
    }
  }, [records.length, galleryIndex]);

  // 打开唱机时加载对应唱片的曲目列表
  useEffect(() => {
    if (!playerOpen || !activeRecord) return;
    const controller = new AbortController();
    setLoadingTracks(true);
    api<Track[]>(`/api/media/audio/tracks?${new URLSearchParams({ path: activeRecord.id, limit: '100' })}`, { signal: controller.signal })
      .then(list => { if (!controller.signal.aborted) setTracks(Array.isArray(list) ? list : []); })
      .catch(() => { if (!controller.signal.aborted) setTracks([]); })
      .finally(() => { if (!controller.signal.aborted) setLoadingTracks(false); });
    return () => controller.abort();
  }, [playerOpen, activeRecord?.id]);

  // 唱片旋转动画：仅在唱机打开且正在播放时运行 rAF
  const isThisRecordPlaying = !!(engine?.playing && activeRecord && (engine.albumId === activeRecord.id || engine.albumId === activeRecord.artistId));
  useEffect(() => {
    if (!playerOpen || !engine?.playing) return;
    let raf = 0;
    let last = performance.now();
    const tick = (now: number) => {
      const dt = Math.min(0.05, (now - last) / 1000);
      last = now;
      setSpinAngle(a => (a + dt * 42) % 360);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, [playerOpen, engine?.playing]);

  const openRecord = useCallback((idx: number, autoPlay = false) => {
    const clamped = clampIndex(idx, records.length);
    setGalleryIndex(clamped);
    setPlayerOpen(true);
    const rec = records[clamped];
    if (rec && autoPlay && engine) {
      engine.open(rec.id, rec.title, 0, true);
    }
  }, [records, engine]);

  // 键盘左右切唱片、空格播放暂停、Esc 返回画廊
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.target as HTMLElement)?.tagName === 'INPUT' || (e.target as HTMLElement)?.tagName === 'SELECT') return;
      if (e.key === 'Escape' && playerOpen) {
        setPlayerOpen(false);
        e.preventDefault();
      } else if (e.key === 'ArrowLeft' && !playerOpen) {
        setGalleryIndex(i => clampIndex(Math.round(i) - 1, records.length));
        e.preventDefault();
      } else if (e.key === 'ArrowRight' && !playerOpen) {
        setGalleryIndex(i => clampIndex(Math.round(i) + 1, records.length));
        e.preventDefault();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [playerOpen, records.length]);

  const progress = engine?.duration && engine.duration > 0 ? Math.min(1, Math.max(0, engine.position / engine.duration)) : 0;
  const tonearmAngle = playerOpen && engine?.playing ? -4 + progress * 20 : -22;

  if (!records.length) {
    return <div className="flex min-h-[420px] items-center justify-center rounded-xl border border-line bg-panel p-8 text-sm text-muted">
      正在加载音乐库唱片画廊…
    </div>;
  }

  return (
    <div
      className="relative isolate min-h-[calc(100vh-9.5rem)] w-full overflow-hidden rounded-xl border border-line select-none"
      style={{
        '--album-accent': activeRecord?.accentColor ?? '#58a6ff',
        '--player-bg': theme.background,
        '--player-disc': theme.disc,
        '--player-disc-core': theme.discCore,
        background: playerOpen ? theme.background : 'radial-gradient(circle at 50% 38%, #1c2230 0%, #0d1117 70%)',
        color: playerOpen && lightMode ? '#141416' : '#e6edf3',
      } as React.CSSProperties}
    >
      <style>{VINYL_STYLES}</style>

      {/* 背景氛围模糊封面 */}
      {activeRecord?.coverUrl ? (
        <img
          src={activeRecord.coverUrl}
          alt=""
          className="pointer-events-none absolute inset-0 h-full w-full scale-110 object-cover opacity-20 blur-3xl transition-opacity duration-500"
        />
      ) : null}

      {/* 顶栏：歌手筛选、主题切换、返回画廊 */}
      <header className="relative z-30 flex flex-wrap items-center justify-between gap-2 px-5 pt-4">
        <div className="flex items-center gap-2">
          {playerOpen ? (
            <button
              type="button"
              className={chip(false)}
              onClick={() => setPlayerOpen(false)}
            >
              ← 返回唱片架
            </button>
          ) : (
            <select
              aria-label="筛选歌手"
              className={select}
              value={artistFilter}
              onChange={e => { setArtistFilter(e.target.value); setGalleryIndex(0); }}
            >
              <option value="">全部常听歌手（{records.length} 张唱片）</option>
              {artists.map(a => (
                <option key={a.id} value={a.id}>
                  {a.main_artist ? '★ ' : ''}{a.title}（{a.album_count ? `${a.album_count} 专辑` : `${a.track_count} 首`}）
                </option>
              ))}
            </select>
          )}
        </div>

        <div className="text-center">
          <strong className="block text-sm font-semibold">
            {playerOpen ? '黑胶唱机 · Now Spinning' : '3D 唱片画廊'}
          </strong>
          <span className="text-xs opacity-70">
            {activeRecord ? `${activeRecord.artist} · ${activeRecord.title}` : ''}
          </span>
        </div>

        <div className="flex items-center gap-1.5">
          {PLAYER_THEMES.map((t, idx) => (
            <button
              key={t.name}
              type="button"
              title={`切换主题：${t.name}`}
              onClick={() => setThemeIndex(idx)}
              className={`h-5 w-5 rounded-full border transition-transform ${
                idx === themeIndex ? 'scale-110 border-white ring-2 ring-primary' : 'border-white/40 opacity-75 hover:opacity-100'
              }`}
              style={{ background: t.swatch }}
            />
          ))}
        </div>
      </header>

      {/* 3D 透视唱片架视图 */}
      {!playerOpen && (
        <>
          <section
            className="relative z-20 flex h-[calc(100vh-19rem)] min-h-[360px] cursor-grab items-center justify-center active:cursor-grabbing"
            onPointerDown={e => {
              dragRef.current = { active: true, moved: false, startX: e.clientX, startIndex: galleryIndex };
              e.currentTarget.setPointerCapture(e.pointerId);
            }}
            onPointerMove={e => {
              if (!dragRef.current.active) return;
              const delta = e.clientX - dragRef.current.startX;
              if (Math.abs(delta) > 6) dragRef.current.moved = true;
              setGalleryIndex(clampIndex(dragRef.current.startIndex - delta / 160, records.length));
            }}
            onPointerUp={e => {
              if (!dragRef.current.active) return;
              dragRef.current.active = false;
              if (e.currentTarget.hasPointerCapture(e.pointerId)) {
                e.currentTarget.releasePointerCapture(e.pointerId);
              }
              setGalleryIndex(i => clampIndex(Math.round(i), records.length));
            }}
            onWheel={e => {
              const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY;
              if (Math.abs(delta) < 4) return;
              setGalleryIndex(i => clampIndex(Math.round(i + Math.sign(delta) * 1), records.length));
            }}
          >
            <div className="oh-vinyl-perspective relative h-full w-full max-w-5xl">
              {records.map((rec, idx) => {
                const offset = idx - galleryIndex;
                if (Math.abs(offset) > 5) return null;
                const isCenter = Math.abs(offset) < 0.45;
                const abs = Math.abs(offset);
                return (
                  <button
                    key={rec.id}
                    type="button"
                    onClick={() => {
                      if (dragRef.current.moved) return;
                      if (isCenter) openRecord(idx, false);
                      else setGalleryIndex(idx);
                    }}
                    className={`oh-vinyl-case absolute left-1/2 top-1/2 w-[clamp(160px,18vw,250px)] border-0 bg-transparent p-0 text-left outline-none ${
                      isCenter ? 'brightness-105' : 'brightness-75 hover:brightness-95'
                    }`}
                    style={{
                      '--album-accent': rec.accentColor,
                      opacity: Math.max(0, 1 - abs * 0.18),
                      transform: `translate(-50%, -50%) ${buildAlbumTransform(offset, false)}`,
                      zIndex: 100 - Math.round(abs * 10),
                    } as React.CSSProperties}
                  >
                    <div className="oh-vinyl-case-box relative aspect-square w-full overflow-hidden rounded-lg bg-[#161b22] shadow-[0_24px_60px_rgba(0,0,0,0.45)]">
                      {rec.coverUrl ? (
                        <img src={rec.coverUrl} alt={rec.title} className="h-full w-full object-cover" loading="lazy" />
                      ) : (
                        <div className="flex h-full w-full items-center justify-center text-5xl text-muted">♪</div>
                      )}
                      <span className="pointer-events-none absolute inset-0 bg-[linear-gradient(125deg,rgba(255,255,255,0.32),transparent_36%,rgba(0,0,0,0.28)_100%)]" />
                    </div>
                    <div className={`mt-3 text-center transition-opacity ${isCenter ? 'opacity-100' : 'opacity-40'}`}>
                      <strong className="block truncate text-sm font-semibold">{rec.title}</strong>
                      <small className="mt-0.5 block truncate text-xs opacity-75">
                        {rec.artist} · {rec.trackCount} 首
                      </small>
                    </div>
                  </button>
                );
              })}
            </div>
          </section>

          {/* 底部当前选中唱片快捷卡片 */}
          {activeRecord && (
            <footer className="relative z-30 mx-auto flex max-w-xl flex-col items-center gap-3 px-4 pb-5">
              <div className="flex items-center gap-1.5">
                {records.slice(Math.max(0, activeIndex - 6), Math.min(records.length, activeIndex + 7)).map(rec => {
                  const idx = records.indexOf(rec);
                  return (
                    <button
                      key={rec.id}
                      type="button"
                      aria-label={rec.title}
                      onClick={() => setGalleryIndex(idx)}
                      className={`h-1.5 rounded-full transition-all ${
                        idx === activeIndex ? 'w-7 bg-primary' : 'w-2 bg-white/30 hover:bg-white/60'
                      }`}
                    />
                  );
                })}
              </div>
              <div className="flex w-full items-center gap-3.5 rounded-xl border border-white/15 bg-black/45 p-2.5 backdrop-blur-xl">
                {activeRecord.coverUrl ? (
                  <img src={activeRecord.coverUrl} alt="" className="h-12 w-12 shrink-0 rounded-md object-cover shadow" />
                ) : (
                  <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-md bg-panel text-xl">♪</div>
                )}
                <div className="min-w-0 flex-1">
                  <strong className="block truncate text-sm font-semibold text-white">{activeRecord.title}</strong>
                  <small className="block truncate text-xs text-white/70">
                    {activeRecord.artist} · {activeRecord.trackCount} 首曲目
                  </small>
                </div>
                <button
                  type="button"
                  className="rounded-lg border border-white/20 bg-white/10 px-3 py-1.5 text-xs font-semibold text-white hover:bg-white/20"
                  onClick={() => openRecord(activeIndex, false)}
                >
                  进入唱机
                </button>
                <button
                  type="button"
                  className="rounded-lg bg-primary px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90"
                  onClick={() => openRecord(activeIndex, true)}
                >
                  ▶ 立即播放
                </button>
              </div>
            </footer>
          )}
        </>
      )}

      {/* 黑胶唱机详情与曲目播放视图 */}
      {playerOpen && activeRecord && (
        <section className="relative z-20 mx-auto grid max-w-6xl grid-cols-1 items-center gap-8 px-6 py-6 lg:grid-cols-[1.08fr_0.92fr]">
          {/* 左侧：黑胶唱片 + 封套 + 金属唱臂 */}
          <div className="relative mx-auto flex h-[380px] w-full max-w-[540px] items-center justify-center sm:h-[440px]">
            {/* 旋转黑胶圆盘 */}
            <div
              className="oh-vinyl-disc absolute right-[6%] top-1/2 aspect-square w-[min(64vw,340px)] rounded-full"
              style={{
                transform: `translateY(-50%) rotate(${spinAngle}deg)`,
              }}
            >
              <span className="oh-vinyl-highlight absolute inset-[6%] rounded-full" />
              <div className="absolute left-1/2 top-1/2 grid aspect-square w-[32%] -translate-x-1/2 -translate-y-1/2 place-items-center overflow-hidden rounded-full border border-black/20 p-2 text-center shadow-inner"
                style={{ background: 'var(--player-disc-core)' }}>
                {activeRecord.coverUrl ? (
                  <img src={activeRecord.coverUrl} alt="" className="h-full w-full rounded-full object-cover opacity-90" />
                ) : (
                  <span className="truncate text-xs font-bold text-neutral-900">{activeRecord.title}</span>
                )}
                <span className="absolute h-3 w-3 rounded-full bg-neutral-950 ring-2 ring-white/50" />
              </div>
            </div>

            {/* 左侧实体封套 */}
            <div className="relative left-[-14%] z-10 aspect-square w-[min(50vw,265px)] overflow-hidden rounded-lg bg-neutral-950 p-2 shadow-[0_32px_70px_rgba(0,0,0,0.45)]">
              {activeRecord.coverUrl ? (
                <img src={activeRecord.coverUrl} alt={activeRecord.title} className="h-full w-full rounded object-cover" />
              ) : (
                <div className="flex h-full w-full items-center justify-center bg-panel text-5xl text-muted">♪</div>
              )}
              <span className="pointer-events-none absolute inset-2 rounded bg-[linear-gradient(130deg,rgba(255,255,255,0.22),transparent_44%,rgba(0,0,0,0.25))]" />
            </div>

            {/* 金属唱臂 */}
            <div
              className="oh-vinyl-tonearm pointer-events-none absolute right-[2%] top-[6%] z-20 h-[250px] w-[120px]"
              style={{ transform: `rotate(${tonearmAngle}deg)` }}
            >
              <span className="absolute left-[51%] top-[18%] h-[74%] w-2.5 origin-top rounded-full bg-[linear-gradient(90deg,#f6efe4,#a48f75_62%,#5b5149)] shadow-lg" />
              <i className="absolute left-[38%] top-[6%] h-12 w-12 rounded-full border border-white/45 bg-[radial-gradient(circle,#f8f1e8_0%,#bca98f_62%,#6c5f53_100%)] shadow-md" />
              <b className="absolute left-[46%] bottom-[6%] h-9 w-6 rounded-b-full bg-neutral-800 shadow-md" />
            </div>
          </div>

          {/* 右侧：唱机控制面板与唱片曲目列表 */}
          <aside
            className={`flex max-h-[calc(100vh-14rem)] flex-col gap-4 rounded-xl border p-5 shadow-2xl backdrop-blur-2xl ${
              lightMode ? 'border-black/10 bg-white/75 text-neutral-950' : 'border-white/10 bg-black/45 text-white'
            }`}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <span className="text-xs font-semibold uppercase tracking-wider opacity-60">
                  {isThisRecordPlaying ? '● 正在播放此唱片' : '唱片详情'}
                </span>
                <h2 className="mt-1 truncate text-2xl font-bold">{activeRecord.title}</h2>
                <p className="truncate text-sm opacity-75">{activeRecord.artist} · 共 {activeRecord.trackCount} 首</p>
              </div>
              <button
                type="button"
                className="shrink-0 rounded-lg bg-primary px-3.5 py-1.5 text-xs font-semibold text-white hover:opacity-90"
                onClick={() => engine?.open(activeRecord.id, activeRecord.title, 0, true)}
              >
                ▶ 播放整张
              </button>
            </div>

            {/* 当前播放进度与控制条 */}
            {engine?.track && (
              <div className={`rounded-lg p-3 ${lightMode ? 'bg-black/5' : 'bg-white/10'}`}>
                <div className="flex items-center justify-between gap-2 text-xs font-medium">
                  <span className="truncate">当前：{engine.track.title}</span>
                  <span className="shrink-0 font-mono opacity-75">
                    {formatTime(engine.position)} / {formatTime(engine.duration)}
                  </span>
                </div>
                <input
                  type="range"
                  aria-label="播放进度"
                  min={0}
                  max={Math.max(1, engine.duration)}
                  value={engine.position}
                  onChange={e => engine.seek(Number(e.target.value))}
                  className="mt-2 h-1.5 w-full cursor-pointer accent-primary"
                />
                <div className="mt-2 flex items-center justify-center gap-4">
                  <button
                    type="button"
                    className="rounded px-2.5 py-1 text-xs font-semibold hover:bg-black/10 dark:hover:bg-white/10"
                    onClick={() => engine.previous()}
                  >
                    ⏮ 上一首
                  </button>
                  <button
                    type="button"
                    className="rounded-full bg-primary px-4 py-1.5 text-xs font-bold text-white shadow"
                    onClick={() => engine.toggle()}
                  >
                    {engine.playing ? '⏸ 暂停' : '▶ 继续'}
                  </button>
                  <button
                    type="button"
                    className="rounded px-2.5 py-1 text-xs font-semibold hover:bg-black/10 dark:hover:bg-white/10"
                    onClick={() => engine.next()}
                  >
                    下一首 ⏭
                  </button>
                </div>
              </div>
            )}

            {/* 曲目列表 */}
            <div className="min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
              {loadingTracks ? (
                <p className="py-6 text-center text-xs opacity-60">正在读取曲目列表…</p>
              ) : tracks.length === 0 ? (
                <p className="py-6 text-center text-xs opacity-60">暂无曲目</p>
              ) : (
                tracks.map((t, idx) => {
                  const playingThis = engine?.track?.id === t.id;
                  return (
                    <button
                      key={t.id}
                      type="button"
                      onClick={() => engine?.open(activeRecord.id, activeRecord.title, t.index ? t.index - 1 : idx, true)}
                      className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
                        playingThis
                          ? 'bg-primary/20 font-semibold text-primary'
                          : lightMode
                            ? 'hover:bg-black/5'
                            : 'hover:bg-white/10'
                      }`}
                    >
                      <span className="w-6 shrink-0 text-right font-mono text-xs opacity-60">
                        {playingThis ? (engine?.playing ? '🔊' : '⏸') : idx + 1}
                      </span>
                      <span className="min-w-0 flex-1 truncate">{t.title}</span>
                    </button>
                  );
                })
              )}
            </div>
          </aside>
        </section>
      )}
    </div>
  );
}
