import { useEffect, useMemo, useRef, useState } from 'react';

type Mode = 'fourier' | 'pendulum';
type PresetShape = 'star' | 'heart' | 'butterfly' | 'trefoil' | 'lissajous' | 'custom';

interface Complex { re: number; im: number }
interface FourierTerm {
  freq: number;
  re: number;
  im: number;
  amp: number;
  phase: number;
}

const SAMPLE_COUNT = 256;

function samplePresetShape(shape: Exclude<PresetShape, 'custom'>, count = SAMPLE_COUNT): Complex[] {
  const pts: Complex[] = [];
  for (let i = 0; i < count; i++) {
    const t = (i / count) * Math.PI * 2;
    if (shape === 'star') {
      const verts: Complex[] = [];
      for (let k = 0; k < 10; k++) {
        const a = -Math.PI / 2 + (k * Math.PI) / 5;
        const r = k % 2 === 0 ? 150 : 62;
        verts.push({ re: Math.cos(a) * r, im: Math.sin(a) * r });
      }
      const segPos = (i / count) * 10;
      const idx = Math.floor(segPos) % 10;
      const frac = segPos - Math.floor(segPos);
      const a = verts[idx], b = verts[(idx + 1) % 10];
      pts.push({ re: a.re + (b.re - a.re) * frac, im: a.im + (b.im - a.im) * frac });
    } else if (shape === 'heart') {
      const x = 16 * Math.pow(Math.sin(t), 3);
      const y = -(13 * Math.cos(t) - 5 * Math.cos(2 * t) - 2 * Math.cos(3 * t) - Math.cos(4 * t));
      pts.push({ re: x * 8.8, im: y * 8.8 });
    } else if (shape === 'butterfly') {
      const r = Math.exp(Math.sin(t)) - 2 * Math.cos(4 * t) + Math.pow(Math.sin((2 * t - Math.PI) / 24), 5);
      pts.push({ re: Math.cos(t) * r * 44, im: -Math.sin(t) * r * 44 });
    } else if (shape === 'trefoil') {
      const r = 95 + 55 * Math.cos(3 * t);
      pts.push({ re: Math.cos(t) * r, im: Math.sin(t) * r });
    } else {
      pts.push({ re: 145 * Math.sin(3 * t + Math.PI / 4), im: 145 * Math.sin(4 * t) });
    }
  }
  return pts;
}

function resampleClosedPath(raw: Complex[], count = SAMPLE_COUNT): Complex[] {
  if (raw.length < 3) return samplePresetShape('star', count);
  const closed = [...raw, raw[0]];
  const cumLen: number[] = [0];
  for (let i = 1; i < closed.length; i++) {
    const d = Math.hypot(closed[i].re - closed[i - 1].re, closed[i].im - closed[i - 1].im);
    cumLen.push(cumLen[i - 1] + d);
  }
  const total = cumLen[cumLen.length - 1];
  if (total < 1e-3) return samplePresetShape('star', count);

  const out: Complex[] = [];
  let seg = 0;
  for (let i = 0; i < count; i++) {
    const target = (i / count) * total;
    while (seg < cumLen.length - 2 && cumLen[seg + 1] < target) seg++;
    const segLen = cumLen[seg + 1] - cumLen[seg] || 1;
    const frac = (target - cumLen[seg]) / segLen;
    out.push({
      re: closed[seg].re + (closed[seg + 1].re - closed[seg].re) * frac,
      im: closed[seg].im + (closed[seg + 1].im - closed[seg].im) * frac,
    });
  }
  return out;
}

function computeDFT(samples: Complex[], sortByAmp: boolean): FourierTerm[] {
  const N = samples.length;
  const terms: FourierTerm[] = [];
  const half = Math.floor(N / 2);
  for (let k = -half; k < N - half; k++) {
    let re = 0;
    let im = 0;
    for (let n = 0; n < N; n++) {
      const phi = (-2 * Math.PI * k * n) / N;
      const c = Math.cos(phi);
      const s = Math.sin(phi);
      re += samples[n].re * c - samples[n].im * s;
      im += samples[n].re * s + samples[n].im * c;
    }
    re /= N;
    im /= N;
    terms.push({
      freq: k,
      re,
      im,
      amp: Math.hypot(re, im),
      phase: Math.atan2(im, re),
    });
  }
  if (sortByAmp) {
    terms.sort((a, b) => b.amp - a.amp);
  } else {
    terms.sort((a, b) => Math.abs(a.freq) - Math.abs(b.freq));
  }
  return terms;
}

interface PendulumState {
  t1: number;
  t2: number;
  w1: number;
  w2: number;
  trail: { x: number; y: number }[];
}

function stepPendulumRK4(
  s: PendulumState,
  dt: number,
  g: number,
  l1: number,
  l2: number,
  m1: number,
  m2: number,
  damping: number
) {
  const deriv = (t1: number, t2: number, w1: number, w2: number) => {
    const delta = t1 - t2;
    const denom1 = l1 * (2 * m1 + m2 - m2 * Math.cos(2 * t1 - 2 * t2));
    const denom2 = l2 * (2 * m1 + m2 - m2 * Math.cos(2 * t1 - 2 * t2));
    const a1 =
      (-g * (2 * m1 + m2) * Math.sin(t1) -
        m2 * g * Math.sin(t1 - 2 * t2) -
        2 * Math.sin(delta) * m2 * (w2 * w2 * l2 + w1 * w1 * l1 * Math.cos(delta))) /
        denom1 -
      damping * w1;
    const a2 =
      (2 *
        Math.sin(delta) *
        (w1 * w1 * l1 * (m1 + m2) + g * (m1 + m2) * Math.cos(t1) + w2 * w2 * l2 * m2 * Math.cos(delta))) /
        denom2 -
      damping * w2;
    return [w1, w2, a1, a2] as const;
  };

  const [k1_t1, k1_t2, k1_w1, k1_w2] = deriv(s.t1, s.t2, s.w1, s.w2);
  const [k2_t1, k2_t2, k2_w1, k2_w2] = deriv(
    s.t1 + 0.5 * dt * k1_t1,
    s.t2 + 0.5 * dt * k1_t2,
    s.w1 + 0.5 * dt * k1_w1,
    s.w2 + 0.5 * dt * k1_w2
  );
  const [k3_t1, k3_t2, k3_w1, k3_w2] = deriv(
    s.t1 + 0.5 * dt * k2_t1,
    s.t2 + 0.5 * dt * k2_t2,
    s.w1 + 0.5 * dt * k2_w1,
    s.w2 + 0.5 * dt * k2_w2
  );
  const [k4_t1, k4_t2, k4_w1, k4_w2] = deriv(
    s.t1 + dt * k3_t1,
    s.t2 + dt * k3_t2,
    s.w1 + dt * k3_w1,
    s.w2 + dt * k3_w2
  );

  s.t1 += (dt / 6) * (k1_t1 + 2 * k2_t1 + 2 * k3_t1 + k4_t1);
  s.t2 += (dt / 6) * (k1_t2 + 2 * k2_t2 + 2 * k3_t2 + k4_t2);
  s.w1 += (dt / 6) * (k1_w1 + 2 * k2_w1 + 2 * k3_w1 + k4_w1);
  s.w2 += (dt / 6) * (k1_w2 + 2 * k2_w2 + 2 * k3_w2 + k4_w2);
}

export default function FourierPendulumLab() {
  const [mode, setMode] = useState<Mode>('fourier');

  // 傅里叶状态
  const [shape, setShape] = useState<PresetShape>('star');
  const [customPts, setCustomPts] = useState<Complex[]>(() => samplePresetShape('star'));
  const [harmonics, setHarmonics] = useState(80);
  const [sortByAmp, setSortByAmp] = useState(true);
  const [showCircles, setShowCircles] = useState(true);
  const [speed, setSpeed] = useState(1.0);

  // 双摆状态
  const [pendulumCount, setPendulumCount] = useState(24);
  const [gravity, setGravity] = useState(9.81);
  const [lengthRatio, setLengthRatio] = useState(1.0);
  const [massRatio, setMassRatio] = useState(1.0);
  const [damping, setDamping] = useState(0.008);
  const [resetTick, setResetTick] = useState(0);

  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const traceRef = useRef<Complex[]>([]);
  const drawingRef = useRef<Complex[] | null>(null);

  const activeSamples = useMemo(() => {
    if (shape === 'custom') return resampleClosedPath(customPts, SAMPLE_COUNT);
    return samplePresetShape(shape, SAMPLE_COUNT);
  }, [shape, customPts]);

  const dftTerms = useMemo(() => {
    traceRef.current = [];
    return computeDFT(activeSamples, sortByAmp);
  }, [activeSamples, sortByAmp]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    let timePhase = 0;
    let lastNow = performance.now();

    // 初始化双摆阵列
    const pendulums: PendulumState[] = Array.from({ length: pendulumCount }, (_, i) => ({
      t1: Math.PI * 0.72 + i * 0.00018,
      t2: Math.PI * 0.65 + i * 0.00018,
      w1: 0,
      w2: 0,
      trail: [],
    }));

    const render = (now: number) => {
      const dt = Math.min(0.03, (now - lastNow) / 1000);
      lastNow = now;

      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const rect = canvas.getBoundingClientRect();
      const w = Math.max(300, Math.floor(rect.width));
      const h = Math.max(260, Math.floor(rect.height));
      if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
        canvas.width = Math.floor(w * dpr);
        canvas.height = Math.floor(h * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

      ctx.fillStyle = '#090d16';
      ctx.fillRect(0, 0, w, h);

      // 坐标网格
      ctx.strokeStyle = 'rgba(148,163,184,0.07)';
      ctx.lineWidth = 1;
      const cx = w * 0.5;
      const cy = h * 0.5;
      for (let x = cx % 40; x < w; x += 40) {
        ctx.beginPath(); ctx.moveTo(x, 0); ctx.lineTo(x, h); ctx.stroke();
      }
      for (let y = cy % 40; y < h; y += 40) {
        ctx.beginPath(); ctx.moveTo(0, y); ctx.lineTo(w, y); ctx.stroke();
      }

      if (mode === 'fourier') {
        // 如果正在手绘，直接画手绘轨迹
        if (drawingRef.current && drawingRef.current.length > 0) {
          ctx.beginPath();
          for (let i = 0; i < drawingRef.current.length; i++) {
            const p = drawingRef.current[i];
            if (i === 0) ctx.moveTo(cx + p.re, cy + p.im);
            else ctx.lineTo(cx + p.re, cy + p.im);
          }
          ctx.strokeStyle = '#38bdf8';
          ctx.lineWidth = 2.4;
          ctx.stroke();
          rafId = window.requestAnimationFrame(render);
          return;
        }

        // 原采样轮廓参考虚线
        ctx.beginPath();
        for (let i = 0; i < activeSamples.length; i++) {
          const p = activeSamples[i];
          if (i === 0) ctx.moveTo(cx + p.re, cy + p.im);
          else ctx.lineTo(cx + p.re, cy + p.im);
        }
        ctx.closePath();
        ctx.strokeStyle = 'rgba(148,163,184,0.18)';
        ctx.lineWidth = 1.2;
        ctx.stroke();

        // 计算本轮叠加
        const maxH = Math.min(harmonics, dftTerms.length);
        let x = cx;
        let y = cy;
        for (let i = 0; i < maxH; i++) {
          const term = dftTerms[i];
          const prevX = x;
          const prevY = y;
          const angle = term.freq * timePhase + term.phase;
          x += term.amp * Math.cos(angle);
          y += term.amp * Math.sin(angle);

          if (showCircles && term.amp > 0.6) {
            ctx.beginPath();
            ctx.arc(prevX, prevY, term.amp, 0, Math.PI * 2);
            ctx.strokeStyle = 'rgba(56,189,248,0.22)';
            ctx.lineWidth = 1;
            ctx.stroke();

            ctx.beginPath();
            ctx.moveTo(prevX, prevY);
            ctx.lineTo(x, y);
            ctx.strokeStyle = 'rgba(226,232,240,0.45)';
            ctx.lineWidth = 1.1;
            ctx.stroke();
          }
        }

        traceRef.current.push({ re: x - cx, im: y - cy });
        if (traceRef.current.length > SAMPLE_COUNT) {
          traceRef.current.shift();
        }

        // 绘制重构出的高亮路径
        if (traceRef.current.length > 1) {
          ctx.beginPath();
          for (let i = 0; i < traceRef.current.length; i++) {
            const p = traceRef.current[i];
            if (i === 0) ctx.moveTo(cx + p.re, cy + p.im);
            else ctx.lineTo(cx + p.re, cy + p.im);
          }
          ctx.strokeStyle = '#facc15';
          ctx.lineWidth = 2.5;
          ctx.stroke();
        }

        // 画笔尖光点
        ctx.beginPath();
        ctx.arc(x, y, 4.5, 0, Math.PI * 2);
        ctx.fillStyle = '#f43f5e';
        ctx.fill();

        timePhase += ((Math.PI * 2) / SAMPLE_COUNT) * speed;
        if (timePhase >= Math.PI * 2) timePhase -= Math.PI * 2;
      } else {
        // 混沌双摆丝带波演化
        const anchorX = w * 0.5;
        const anchorY = h * 0.42;
        const baseL = Math.min(w, h) * 0.22;
        const l1 = baseL;
        const l2 = baseL * lengthRatio;
        const subSteps = 4;
        const stepDt = (dt * 1.4) / subSteps;

        for (let i = 0; i < pendulums.length; i++) {
          const p = pendulums[i];
          for (let s = 0; s < subSteps; s++) {
            stepPendulumRK4(p, stepDt, gravity, 1.0, lengthRatio, 1.0, massRatio, damping);
          }
          const x1 = anchorX + l1 * Math.sin(p.t1);
          const y1 = anchorY + l1 * Math.cos(p.t1);
          const x2 = x1 + l2 * Math.sin(p.t2);
          const y2 = y1 + l2 * Math.cos(p.t2);
          p.trail.push({ x: x2, y: y2 });
          if (p.trail.length > 56) p.trail.shift();

          const hue = Math.round((i / Math.max(1, pendulums.length - 1)) * 280);
          if (p.trail.length > 1) {
            ctx.beginPath();
            for (let t = 0; t < p.trail.length; t++) {
              const pt = p.trail[t];
              if (t === 0) ctx.moveTo(pt.x, pt.y);
              else ctx.lineTo(pt.x, pt.y);
            }
            ctx.strokeStyle = `hsla(${hue}, 90%, 62%, 0.48)`;
            ctx.lineWidth = 1.6;
            ctx.stroke();
          }

          ctx.beginPath();
          ctx.moveTo(anchorX, anchorY);
          ctx.lineTo(x1, y1);
          ctx.lineTo(x2, y2);
          ctx.strokeStyle = `hsla(${hue}, 85%, 65%, 0.35)`;
          ctx.lineWidth = 1.2;
          ctx.stroke();

          ctx.beginPath();
          ctx.arc(x2, y2, 3.2, 0, Math.PI * 2);
          ctx.fillStyle = `hsl(${hue}, 95%, 68%)`;
          ctx.fill();
        }
      }

      rafId = window.requestAnimationFrame(render);
    };

    rafId = window.requestAnimationFrame(render);
    return () => window.cancelAnimationFrame(rafId);
  }, [mode, activeSamples, dftTerms, harmonics, showCircles, speed, pendulumCount, gravity, lengthRatio, massRatio, damping, resetTick]);

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
      <div className="relative flex min-h-[480px] flex-col overflow-hidden rounded-xl border border-line bg-[#090d16] select-none">
        <div className="z-10 flex flex-wrap items-center justify-between gap-2 border-b border-white/10 bg-black/45 px-4 py-2.5 backdrop-blur">
          <div className="flex items-center gap-2">
            <button
              onClick={() => setMode('fourier')}
              className={`rounded-md px-3 py-1 text-xs font-medium transition ${
                mode === 'fourier' ? 'bg-primary text-white' : 'border border-white/15 text-slate-300 hover:border-primary'
              }`}
            >
              傅里叶本轮链 (DFT)
            </button>
            <button
              onClick={() => setMode('pendulum')}
              className={`rounded-md px-3 py-1 text-xs font-medium transition ${
                mode === 'pendulum' ? 'bg-primary text-white' : 'border border-white/15 text-slate-300 hover:border-primary'
              }`}
            >
              混沌双摆丝带波 (RK4)
            </button>
          </div>

          {mode === 'fourier' ? (
            <div className="flex flex-wrap items-center gap-1.5 text-xs">
              {(
                [
                  ['star', '五角星'],
                  ['heart', '心形线'],
                  ['butterfly', '蝴蝶线'],
                  ['trefoil', '三叶结'],
                  ['lissajous', '利萨如'],
                  ['custom', '手绘轮廓'],
                ] as [PresetShape, string][]
              ).map(([id, label]) => (
                <button
                  key={id}
                  onClick={() => setShape(id)}
                  className={`rounded px-2 py-1 ${
                    shape === id ? 'bg-white/15 text-white' : 'text-slate-400 hover:text-white'
                  }`}
                >
                  {label}
                </button>
              ))}
            </div>
          ) : (
            <button
              onClick={() => setResetTick(t => t + 1)}
              className="rounded-md border border-white/15 px-2.5 py-1 text-xs text-slate-200 hover:border-primary"
            >
              重置微扰初态
            </button>
          )}
        </div>

        <canvas
          ref={canvasRef}
          className="h-[460px] w-full flex-1 cursor-crosshair touch-none"
          onPointerDown={e => {
            if (mode !== 'fourier') return;
            e.currentTarget.setPointerCapture(e.pointerId);
            const rect = e.currentTarget.getBoundingClientRect();
            const cx = rect.width * 0.5;
            const cy = rect.height * 0.5;
            drawingRef.current = [{ re: e.clientX - rect.left - cx, im: e.clientY - rect.top - cy }];
          }}
          onPointerMove={e => {
            if (!drawingRef.current) return;
            const rect = e.currentTarget.getBoundingClientRect();
            const cx = rect.width * 0.5;
            const cy = rect.height * 0.5;
            drawingRef.current.push({ re: e.clientX - rect.left - cx, im: e.clientY - rect.top - cy });
          }}
          onPointerUp={() => {
            if (!drawingRef.current) return;
            const pts = drawingRef.current;
            drawingRef.current = null;
            if (pts.length >= 6) {
              setCustomPts(pts);
              setShape('custom');
            }
          }}
        />

        <div className="z-10 border-t border-white/10 bg-black/45 px-4 py-2 text-xs text-slate-300 backdrop-blur">
          {mode === 'fourier'
            ? '提示：直接在画布上按住鼠标拖动即可一笔手绘任意闭合曲线，松开后自动进行离散傅里叶变换并驱动本轮圆环重绘'
            : '提示：初始仅相差 0.00018 弧度的双摆阵列，在几秒内展现由周期同步到指数级混沌分岔的演化过程'}
        </div>
      </div>

      <div className="space-y-4">
        {mode === 'fourier' ? (
          <>
            <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
              <h3 className="text-sm font-semibold">傅里叶级数参数</h3>
              <label className="block space-y-1 text-xs">
                <div className="flex justify-between">
                  <span className="text-muted">参与叠加谐波数 (Harmonics)</span>
                  <span>{harmonics} / {SAMPLE_COUNT}</span>
                </div>
                <input
                  type="range"
                  min="2"
                  max={SAMPLE_COUNT}
                  step="1"
                  value={harmonics}
                  onChange={e => {
                    traceRef.current = [];
                    setHarmonics(Number(e.target.value));
                  }}
                  className="w-full"
                />
              </label>

              <label className="block space-y-1 text-xs">
                <div className="flex justify-between">
                  <span className="text-muted">角频率演化倍速</span>
                  <span>{speed.toFixed(1)}x</span>
                </div>
                <input
                  type="range"
                  min="0.2"
                  max="3.0"
                  step="0.1"
                  value={speed}
                  onChange={e => setSpeed(Number(e.target.value))}
                  className="w-full"
                />
              </label>

              <div className="flex flex-col gap-2 pt-1 text-xs">
                <label className="flex items-center justify-between">
                  <span>按振幅从大到小排序本轮</span>
                  <input type="checkbox" checked={sortByAmp} onChange={e => setSortByAmp(e.target.checked)} />
                </label>
                <label className="flex items-center justify-between">
                  <span>显示本轮圆环与旋转矢量</span>
                  <input type="checkbox" checked={showCircles} onChange={e => setShowCircles(e.target.checked)} />
                </label>
              </div>
            </section>

            <section className="rounded-xl border border-line bg-panel p-4 space-y-2">
              <h3 className="text-sm font-semibold">前 6 阶主导谐波分量</h3>
              <div className="space-y-1.5 text-xs font-mono">
                {dftTerms.slice(0, 6).map((t, idx) => (
                  <div key={idx} className="flex items-center justify-between rounded border border-line/70 px-2.5 py-1">
                    <span>k = {t.freq >= 0 ? `+${t.freq}` : t.freq}</span>
                    <span>A = {t.amp.toFixed(1)}</span>
                    <span className="text-muted">φ = {((t.phase * 180) / Math.PI).toFixed(0)}°</span>
                  </div>
                ))}
              </div>
            </section>
          </>
        ) : (
          <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
            <h3 className="text-sm font-semibold">双摆动力学参数</h3>

            <label className="block space-y-1 text-xs">
              <div className="flex justify-between">
                <span className="text-muted">并行微扰双摆数量</span>
                <span>{pendulumCount}</span>
              </div>
              <input
                type="range"
                min="6"
                max="48"
                step="2"
                value={pendulumCount}
                onChange={e => setPendulumCount(Number(e.target.value))}
                className="w-full"
              />
            </label>

            <label className="block space-y-1 text-xs">
              <div className="flex justify-between">
                <span className="text-muted">重力加速度 g</span>
                <span>{gravity.toFixed(2)} m/s²</span>
              </div>
              <input
                type="range"
                min="2.0"
                max="24.0"
                step="0.5"
                value={gravity}
                onChange={e => setGravity(Number(e.target.value))}
                className="w-full"
              />
            </label>

            <label className="block space-y-1 text-xs">
              <div className="flex justify-between">
                <span className="text-muted">摆杆长度比 (L₂ / L₁)</span>
                <span>{lengthRatio.toFixed(2)}</span>
              </div>
              <input
                type="range"
                min="0.4"
                max="1.6"
                step="0.05"
                value={lengthRatio}
                onChange={e => setLengthRatio(Number(e.target.value))}
                className="w-full"
              />
            </label>

            <label className="block space-y-1 text-xs">
              <div className="flex justify-between">
                <span className="text-muted">摆锤质量比 (m₂ / m₁)</span>
                <span>{massRatio.toFixed(2)}</span>
              </div>
              <input
                type="range"
                min="0.3"
                max="2.5"
                step="0.1"
                value={massRatio}
                onChange={e => setMassRatio(Number(e.target.value))}
                className="w-full"
              />
            </label>

            <label className="block space-y-1 text-xs">
              <div className="flex justify-between">
                <span className="text-muted">耗散阻尼系数</span>
                <span>{damping.toFixed(3)}</span>
              </div>
              <input
                type="range"
                min="0"
                max="0.05"
                step="0.002"
                value={damping}
                onChange={e => setDamping(Number(e.target.value))}
                className="w-full"
              />
            </label>
          </section>
        )}
      </div>
    </div>
  );
}
