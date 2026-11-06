import { useEffect, useRef, useState } from 'react';
import { store, stored } from './ui';

interface PerfMemory {
  usedJSHeapSize: number;
  totalJSHeapSize: number;
  jsHeapSizeLimit: number;
}

interface TabPerfSnapshot {
  fps: number;
  avgFrameMs: number;
  maxFrameMs: number;
  jankFrames: number;
  heapUsedMB: number | null;
  heapTotalMB: number | null;
  heapLimitMB: number | null;
  domNodes: number;
  imgCount: number;
  canvasCount: number;
  videoCount: number;
  playingMedia: number;
  resourceCount: number;
  transferMB: number;
  longTasks: number;
}

const OVERLAY_KEY = 'omnihub.perf.overlay';
const OVERLAY_EVENT = 'omnihub-perf-overlay-changed';

export function usePerfOverlayEnabled(): [boolean, (next: boolean) => void] {
  const [enabled, setEnabled] = useState<boolean>(() => stored(OVERLAY_KEY, false));

  useEffect(() => {
    const sync = () => setEnabled(stored(OVERLAY_KEY, false));
    window.addEventListener(OVERLAY_EVENT, sync);
    window.addEventListener('storage', sync);
    return () => {
      window.removeEventListener(OVERLAY_EVENT, sync);
      window.removeEventListener('storage', sync);
    };
  }, []);

  const update = (next: boolean) => {
    store(OVERLAY_KEY, next);
    setEnabled(next);
    window.dispatchEvent(new Event(OVERLAY_EVENT));
  };

  return [enabled, update];
}

function sampleTabMetrics(
  fps: number,
  avgFrameMs: number,
  maxFrameMs: number,
  jankFrames: number,
  longTasks: number,
): TabPerfSnapshot {
  const perf = performance as Performance & { memory?: PerfMemory };
  const mem = perf.memory;
  const heapUsedMB = mem ? mem.usedJSHeapSize / 1048576 : null;
  const heapTotalMB = mem ? mem.totalJSHeapSize / 1048576 : null;
  const heapLimitMB = mem ? mem.jsHeapSizeLimit / 1048576 : null;

  const domNodes = document.getElementsByTagName('*').length;
  const imgCount = document.getElementsByTagName('img').length;
  const canvasCount = document.getElementsByTagName('canvas').length;
  const videos = Array.from(document.getElementsByTagName('video'));
  const audios = Array.from(document.getElementsByTagName('audio'));
  const playingMedia = [...videos, ...audios].filter(m => !m.paused && !m.ended).length;

  const resources = performance.getEntriesByType('resource') as PerformanceResourceTiming[];
  let transferBytes = 0;
  for (let i = 0; i < resources.length; i++) {
    transferBytes += resources[i].transferSize || 0;
  }

  return {
    fps,
    avgFrameMs,
    maxFrameMs,
    jankFrames,
    heapUsedMB,
    heapTotalMB,
    heapLimitMB,
    domNodes,
    imgCount,
    canvasCount,
    videoCount: videos.length,
    playingMedia,
    resourceCount: resources.length,
    transferMB: transferBytes / 1048576,
    longTasks,
  };
}

function useTabPerformance(active: boolean): TabPerfSnapshot {
  const [snap, setSnap] = useState<TabPerfSnapshot>(() =>
    sampleTabMetrics(60, 16.7, 16.7, 0, 0),
  );

  useEffect(() => {
    if (!active) return;
    let raf = 0;
    let frames = 0;
    let windowStart = performance.now();
    let prevFrame = windowStart;
    let maxDelta = 0;
    let jank = 0;
    let longTaskCount = 0;

    let observer: PerformanceObserver | undefined;
    if (typeof PerformanceObserver !== 'undefined') {
      try {
        observer = new PerformanceObserver(list => {
          longTaskCount += list.getEntries().length;
        });
        observer.observe({ entryTypes: ['longtask'] });
      } catch {
        // longtask observer not supported in all browsers
      }
    }

    const tick = (now: number) => {
      frames++;
      const dt = now - prevFrame;
      prevFrame = now;
      if (dt > maxDelta) maxDelta = dt;
      if (dt > 34) jank++;

      const elapsed = now - windowStart;
      if (elapsed >= 1000) {
        const fps = Math.round((frames * 1000) / elapsed);
        const avgMs = elapsed / Math.max(1, frames);
        setSnap(sampleTabMetrics(fps, avgMs, maxDelta, jank, longTaskCount));
        frames = 0;
        maxDelta = 0;
        jank = 0;
        windowStart = now;
      }
      raf = requestAnimationFrame(tick);
    };

    raf = requestAnimationFrame(tick);
    return () => {
      cancelAnimationFrame(raf);
      observer?.disconnect();
    };
  }, [active]);

  return snap;
}

export default function PerfAdmin() {
  const [overlay, setOverlay] = usePerfOverlayEnabled();
  const snap = useTabPerformance(true);

  const heapPct =
    snap.heapUsedMB !== null && snap.heapLimitMB
      ? Math.min(100, Math.round((snap.heapUsedMB / snap.heapLimitMB) * 100))
      : 0;

  return (
    <section className="space-y-4">
      <div className="space-y-3 rounded-lg border border-line bg-panel p-4">
        <label className="flex items-center gap-3">
          <input
            type="checkbox"
            checked={overlay}
            onChange={e => setOverlay(e.target.checked)}
          />
          <span className="font-semibold">显示当前标签页实时性能悬浮窗</span>
        </label>
        <p className="text-xs text-muted">
          仅采集当前浏览器标签页自身的渲染帧率（FPS / 帧耗时）、JS 堆内存、DOM 节点数与活动媒体数；不轮询服务端，关闭后立即停止 `requestAnimationFrame` 采样。
        </p>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <article className="rounded-lg border border-line bg-panel p-4 space-y-1.5">
          <span className="text-xs text-muted">标签页渲染帧率与帧耗时</span>
          <p className="text-2xl font-bold font-mono">
            {snap.fps} FPS · {snap.avgFrameMs.toFixed(1)} ms
          </p>
          <p className="text-xs text-muted">
            峰值帧耗时 {snap.maxFrameMs.toFixed(1)} ms · 掉帧(&gt;34ms) {snap.jankFrames}/s
          </p>
        </article>

        <article className="rounded-lg border border-line bg-panel p-4 space-y-1.5">
          <span className="text-xs text-muted">标签页 JS 堆内存 (V8 Heap)</span>
          {snap.heapUsedMB !== null ? (
            <>
              <p className="text-2xl font-bold font-mono">
                {snap.heapUsedMB.toFixed(1)} MB / {snap.heapTotalMB?.toFixed(0)} MB
              </p>
              <div className="h-1.5 w-full overflow-hidden rounded-full bg-background">
                <div className="h-full bg-primary transition-all" style={{ width: `${heapPct}%` }} />
              </div>
              <p className="text-xs text-muted">堆上限 {snap.heapLimitMB?.toFixed(0)} MB</p>
            </>
          ) : (
            <p className="text-sm text-muted">当前浏览器内核未暴露 performance.memory</p>
          )}
        </article>

        <article className="rounded-lg border border-line bg-panel p-4 space-y-1.5">
          <span className="text-xs text-muted">标签页 DOM 与画布负载</span>
          <p className="text-2xl font-bold font-mono">{snap.domNodes} DOM 节点</p>
          <p className="text-xs text-muted">
            图片 &lt;img&gt; {snap.imgCount} · 画布 &lt;canvas&gt; {snap.canvasCount}
          </p>
        </article>

        <article className="rounded-lg border border-line bg-panel p-4 space-y-1.5">
          <span className="text-xs text-muted">标签页活动音视频解码</span>
          <p className="text-xl font-bold font-mono">
            播放中 {snap.playingMedia} · &lt;video&gt; {snap.videoCount}
          </p>
          <p className="text-xs text-muted">离开页面后解码器与画布自动卸载</p>
        </article>

        <article className="rounded-lg border border-line bg-panel p-4 space-y-1.5">
          <span className="text-xs text-muted">标签页主线程与资源条目</span>
          <p className="text-xl font-bold font-mono">
            长任务(&gt;50ms) {snap.longTasks} 次 · 资源 {snap.resourceCount} 项
          </p>
          <p className="text-xs text-muted">累计传输 {snap.transferMB.toFixed(2)} MB</p>
        </article>
      </div>
    </section>
  );
}

const OVERLAY_POS_KEY = 'omnihub.perfOverlay.pos';

export function PerfOverlay() {
  const [enabled, setEnabled] = usePerfOverlayEnabled();
  const snap = useTabPerformance(enabled);
  const boxRef = useRef<HTMLDivElement | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    originX: number;
    originY: number;
  } | null>(null);

  const [pos, setPos] = useState<{ x: number; y: number }>(() => {
    try {
      const saved = window.localStorage.getItem(OVERLAY_POS_KEY);
      if (saved) {
        const parsed = JSON.parse(saved);
        if (typeof parsed?.x === 'number' && typeof parsed?.y === 'number') {
          return {
            x: Math.max(8, Math.min(window.innerWidth - 220, parsed.x)),
            y: Math.max(8, Math.min(window.innerHeight - 40, parsed.y)),
          };
        }
      }
    } catch {
      // ignore
    }
    return { x: Math.max(8, window.innerWidth - 320), y: 10 };
  });

  useEffect(() => {
    const onResize = () => {
      const el = boxRef.current;
      const w = el?.offsetWidth ?? 260;
      const h = el?.offsetHeight ?? 32;
      setPos((prev) => ({
        x: Math.max(8, Math.min(window.innerWidth - w - 8, prev.x)),
        y: Math.max(8, Math.min(window.innerHeight - h - 8, prev.y)),
      }));
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  if (!enabled) return null;

  const handlePointerDown = (e: React.PointerEvent<HTMLDivElement>) => {
    if ((e.target as HTMLElement).closest('button')) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      originX: pos.x,
      originY: pos.y,
    };
  };

  const handlePointerMove = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    const el = boxRef.current;
    const w = el?.offsetWidth ?? 260;
    const h = el?.offsetHeight ?? 32;
    const nextX = Math.max(8, Math.min(window.innerWidth - w - 8, drag.originX + (e.clientX - drag.startX)));
    const nextY = Math.max(8, Math.min(window.innerHeight - h - 8, drag.originY + (e.clientY - drag.startY)));
    setPos({ x: nextX, y: nextY });
  };

  const handlePointerUp = (e: React.PointerEvent<HTMLDivElement>) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    dragRef.current = null;
    try {
      window.localStorage.setItem(OVERLAY_POS_KEY, JSON.stringify(pos));
    } catch {
      // ignore
    }
  };

  return (
    <div
      ref={boxRef}
      role="status"
      aria-label="标签页实时性能悬浮窗（可拖拽移动）"
      title="按住可拖拽移动位置"
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerUp}
      style={{ left: `${pos.x}px`, top: `${pos.y}px`, touchAction: 'none' }}
      className="fixed z-[90] flex cursor-grab select-none items-center gap-2.5 rounded-full border border-white/15 bg-black/75 px-3 py-1 font-mono text-[11px] text-white shadow-lg backdrop-blur-md active:cursor-grabbing"
    >
      <span className={snap.fps >= 50 ? 'font-bold text-[#3fb950]' : 'font-bold text-[#d29922]'}>
        {snap.fps} FPS ({snap.avgFrameMs.toFixed(1)}ms)
      </span>
      {snap.heapUsedMB !== null ? <span>JS {snap.heapUsedMB.toFixed(1)}MB</span> : null}
      <span>DOM {snap.domNodes}</span>
      {snap.canvasCount > 0 ? <span>Canvas {snap.canvasCount}</span> : null}
      {snap.playingMedia > 0 ? <span className="text-[#58a6ff]">▶ {snap.playingMedia}</span> : null}
      <button
        type="button"
        onClick={() => setEnabled(false)}
        title="关闭标签页性能悬浮窗"
        className="ml-0.5 cursor-pointer text-white/60 hover:text-white"
      >
        ×
      </button>
    </div>
  );
}
