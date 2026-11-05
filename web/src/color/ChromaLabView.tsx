import { useEffect, useMemo, useRef, useState } from 'react';
import {
  getSpectrumLuminance,
  hexToRgbTuple,
  rgbToCmykTuple,
  rgbToHexStr,
  rgbToHslTuple,
} from './spectrumData';

interface ChromaLabViewProps {
  onCopy: (text: string, label?: string) => void;
}

type HarmonyMode = 'complementary' | 'analogous' | 'triadic' | 'split' | 'tetradic';

const HARMONY_MODES: { id: HarmonyMode; label: string; angles: number[] }[] = [
  { id: 'complementary', label: '互补色 (180°)', angles: [0, 180] },
  { id: 'analogous', label: '邻近色 (±30°)', angles: [0, -30, 30] },
  { id: 'triadic', label: '三角对等 (120°)', angles: [0, 120, 240] },
  { id: 'split', label: '分裂互补 (150°/210°)', angles: [0, 150, 210] },
  { id: 'tetradic', label: '矩形四色 (90°)', angles: [0, 90, 180, 270] },
];

function hslToRgbTuple(h: number, s: number, l: number): [number, number, number] {
  const hn = ((h % 360) + 360) % 360;
  const sn = Math.max(0, Math.min(100, s)) / 100;
  const ln = Math.max(0, Math.min(100, l)) / 100;
  const c = (1 - Math.abs(2 * ln - 1)) * sn;
  const x = c * (1 - Math.abs(((hn / 60) % 2) - 1));
  const m = ln - c / 2;
  const sector: [number, number, number] =
    hn < 60 ? [c, x, 0] :
    hn < 120 ? [x, c, 0] :
    hn < 180 ? [0, c, x] :
    hn < 240 ? [0, x, c] :
    hn < 300 ? [x, 0, c] :
    [c, 0, x];
  return [
    Math.round((sector[0] + m) * 255),
    Math.round((sector[1] + m) * 255),
    Math.round((sector[2] + m) * 255),
  ];
}

export function ChromaLabView({ onCopy }: ChromaLabViewProps) {
  const [hue, setHue] = useState(198);
  const [sat, setSat] = useState(82);
  const [light, setLight] = useState(56);
  const [harmony, setHarmony] = useState<HarmonyMode>('triadic');
  const [bgHex, setBgHex] = useState('#0D1117');
  const wheelRef = useRef<HTMLCanvasElement | null>(null);

  const primaryRgb = useMemo(() => hslToRgbTuple(hue, sat, light), [hue, sat, light]);
  const primaryHex = useMemo(() => rgbToHexStr(primaryRgb), [primaryRgb]);
  const cmyk = useMemo(() => rgbToCmykTuple(primaryRgb), [primaryRgb]);

  const harmonyConfig = useMemo(
    () => HARMONY_MODES.find(m => m.id === harmony) ?? HARMONY_MODES[0],
    [harmony],
  );

  const harmonySwatches = useMemo(() => {
    return harmonyConfig.angles.map(offset => {
      const h = ((hue + offset) % 360 + 360) % 360;
      const rgb = hslToRgbTuple(h, sat, light);
      const hex = rgbToHexStr(rgb);
      return { h, s: sat, l: light, hex };
    });
  }, [hue, sat, light, harmonyConfig]);

  // 11-step Tailwind-style scale (50 -> 950)
  const tailwindScale = useMemo(() => {
    const steps: Array<[number, number]> = [
      [50, 95],
      [100, 90],
      [200, 82],
      [300, 72],
      [400, 62],
      [500, 52],
      [600, 44],
      [700, 36],
      [800, 27],
      [900, 19],
      [950, 12],
    ];
    return steps.map(([weight, lVal]) => {
      const hex = rgbToHexStr(hslToRgbTuple(hue, sat, lVal));
      return { weight, hex, dark: getSpectrumLuminance(hex) < 0.45 };
    });
  }, [hue, sat]);

  const contrastRatio = useMemo(() => {
    const l1 = getSpectrumLuminance(primaryHex);
    const l2 = getSpectrumLuminance(bgHex);
    return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
  }, [primaryHex, bgHex]);

  // Render interactive 360° color wheel + harmony polygon
  useEffect(() => {
    const canvas = wheelRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const size = 280;
    canvas.width = size;
    canvas.height = size;
    const cx = size / 2;
    const cy = size / 2;
    const outerR = size * 0.44;

    ctx.clearRect(0, 0, size, size);

    // Draw 360 hue wedges + radial saturation gradient
    for (let a = 0; a < 360; a += 2) {
      const startRad = ((a - 1) * Math.PI) / 180;
      const endRad = ((a + 1.5) * Math.PI) / 180;
      ctx.beginPath();
      ctx.moveTo(cx, cy);
      ctx.arc(cx, cy, outerR, startRad, endRad);
      ctx.closePath();
      const grad = ctx.createRadialGradient(cx, cy, 0, cx, cy, outerR);
      grad.addColorStop(0, `hsl(0, 0%, ${light}%)`);
      grad.addColorStop(1, `hsl(${a}, 100%, ${light}%)`);
      ctx.fillStyle = grad;
      ctx.fill();
    }

    // Outer metallic ring
    ctx.beginPath();
    ctx.arc(cx, cy, outerR + 2, 0, Math.PI * 2);
    ctx.strokeStyle = 'rgba(255, 255, 255, 0.24)';
    ctx.lineWidth = 2;
    ctx.stroke();

    // Harmony geometric polygon & nodes
    const nodes = harmonySwatches.map((sw, idx) => {
      const rad = (sw.h * Math.PI) / 180;
      const r = (sw.s / 100) * outerR;
      return {
        x: cx + Math.cos(rad) * r,
        y: cy + Math.sin(rad) * r,
        hex: sw.hex,
        primary: idx === 0,
      };
    });

    if (nodes.length > 1) {
      ctx.beginPath();
      nodes.forEach((n, idx) => {
        if (idx === 0) ctx.moveTo(n.x, n.y);
        else ctx.lineTo(n.x, n.y);
      });
      if (nodes.length > 2) ctx.closePath();
      ctx.strokeStyle = 'rgba(255, 255, 255, 0.78)';
      ctx.lineWidth = 1.8;
      ctx.stroke();
    }

    nodes.forEach(n => {
      ctx.beginPath();
      ctx.arc(n.x, n.y, n.primary ? 9 : 6.5, 0, Math.PI * 2);
      ctx.fillStyle = n.hex;
      ctx.fill();
      ctx.lineWidth = n.primary ? 2.6 : 1.8;
      ctx.strokeStyle = '#FFFFFF';
      ctx.stroke();
    });
  }, [hue, sat, light, harmonySwatches]);

  const handleWheelPick = (clientX: number, clientY: number) => {
    const canvas = wheelRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const dx = clientX - (rect.left + rect.width / 2);
    const dy = clientY - (rect.top + rect.height / 2);
    const outerR = rect.width * 0.44;
    const dist = Math.hypot(dx, dy);
    const angle = ((Math.atan2(dy, dx) * 180) / Math.PI + 360) % 360;
    setHue(Math.round(angle));
    setSat(Math.round(Math.min(100, (dist / Math.max(1, outerR)) * 100)));
  };

  const applyHexInput = (val: string) => {
    const clean = val.trim().replace(/^#/, '');
    if (/^[0-9a-fA-F]{6}$/.test(clean)) {
      const rgb = hexToRgbTuple(clean);
      const [nh, ns, nl] = rgbToHslTuple(rgb);
      setHue(nh);
      setSat(ns);
      setLight(nl);
    }
  };

  return (
    <div className="chroma-lab-grid">
      {/* 左侧：360° 几何配色仪 */}
      <div className="rounded-xl border border-line bg-panel p-4 space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-[11px] font-bold uppercase tracking-widest text-muted">CHROMA HARMONY ENGINE</p>
            <h3 className="text-base font-bold">360° 几何色相环与和谐推演</h3>
          </div>
          <code className="rounded bg-background px-2 py-1 font-mono text-xs font-bold">{primaryHex}</code>
        </div>

        <div className="flex justify-center">
          <canvas
            ref={wheelRef}
            className="cursor-crosshair rounded-full shadow-lg"
            style={{ width: 280, height: 280 }}
            onPointerDown={e => {
              e.currentTarget.setPointerCapture(e.pointerId);
              handleWheelPick(e.clientX, e.clientY);
            }}
            onPointerMove={e => {
              if (e.buttons === 1) handleWheelPick(e.clientX, e.clientY);
            }}
          />
        </div>

        <div className="space-y-2 text-xs">
          <label className="flex items-center justify-between gap-2">
            <span className="text-muted">色相 H ({hue}°)</span>
            <input type="range" min={0} max={359} value={hue} onChange={e => setHue(Number(e.target.value))} className="w-48" />
          </label>
          <label className="flex items-center justify-between gap-2">
            <span className="text-muted">饱和度 S ({sat}%)</span>
            <input type="range" min={0} max={100} value={sat} onChange={e => setSat(Number(e.target.value))} className="w-48" />
          </label>
          <label className="flex items-center justify-between gap-2">
            <span className="text-muted">明度 L ({light}%)</span>
            <input type="range" min={8} max={94} value={light} onChange={e => setLight(Number(e.target.value))} className="w-48" />
          </label>
        </div>

        <div className="flex flex-wrap gap-1.5 pt-1">
          {HARMONY_MODES.map(m => (
            <button
              key={m.id}
              type="button"
              onClick={() => setHarmony(m.id)}
              className={`rounded-full border px-2.5 py-1 text-[11px] font-bold transition ${
                harmony === m.id
                  ? 'border-primary bg-primary/20 text-white'
                  : 'border-line bg-background text-muted hover:text-white'
              }`}
            >
              {m.label}
            </button>
          ))}
        </div>

        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          {harmonySwatches.map((sw, idx) => (
            <button
              key={`${sw.hex}-${idx}`}
              type="button"
              onClick={() => onCopy(sw.hex, '和谐色 HEX')}
              className="flex h-16 flex-col justify-between rounded-lg border border-white/15 p-2 text-left text-xs font-bold shadow transition hover:scale-[1.02]"
              style={{
                backgroundColor: sw.hex,
                color: getSpectrumLuminance(sw.hex) > 0.45 ? '#101318' : '#FFFFFF',
              }}
            >
              <span className="text-[10px] opacity-80">{idx === 0 ? '主色' : `协色 ${idx}`}</span>
              <span className="font-mono">{sw.hex}</span>
            </button>
          ))}
        </div>
      </div>

      {/* 右侧：Tailwind 11 级色阶矩阵 + 全通道换算 + WCAG 2.1 实验室 */}
      <div className="rounded-xl border border-line bg-panel p-4 space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2">
            <input
              type="color"
              value={primaryHex}
              onChange={e => applyHexInput(e.target.value)}
              className="h-9 w-12 cursor-pointer rounded border border-line bg-background p-0.5"
            />
            <input
              value={primaryHex}
              onChange={e => applyHexInput(e.target.value)}
              className="w-28 rounded border border-line bg-background px-2.5 py-1.5 font-mono text-xs"
            />
          </div>
          <button
            type="button"
            className="rounded-lg border border-line bg-background px-3 py-1.5 text-xs font-bold hover:border-primary"
            onClick={() => {
              const obj = Object.fromEntries(tailwindScale.map(s => [s.weight, s.hex]));
              onCopy(JSON.stringify(obj, null, 2), 'Tailwind 50-950 色阶 JSON');
            }}
          >
            复制 50–950 色阶 JSON
          </button>
        </div>

        <div className="grid gap-2 sm:grid-cols-2">
          {[
            ['HEX', primaryHex],
            ['RGB', `rgb(${primaryRgb.join(', ')})`],
            ['HSL', `hsl(${hue}, ${sat}%, ${light}%)`],
            ['CMYK', `cmyk(${cmyk.join('%, ')}%)`],
          ].map(([label, val]) => (
            <button
              key={label}
              type="button"
              onClick={() => onCopy(val, label)}
              className="flex items-center justify-between rounded-lg border border-line bg-background px-3 py-2 text-xs hover:border-primary"
            >
              <span className="font-bold text-muted">{label}</span>
              <code className="font-mono font-semibold">{val}</code>
            </button>
          ))}
        </div>

        <div className="space-y-1.5">
          <p className="text-xs font-bold text-muted">11 级工程色阶矩阵（50 → 950，点击单格复制）</p>
          <div className="grid grid-cols-11 gap-1">
            {tailwindScale.map(item => (
              <button
                key={item.weight}
                type="button"
                onClick={() => onCopy(item.hex, `色阶 ${item.weight}`)}
                className="flex h-16 flex-col justify-between rounded-md border border-white/10 p-1.5 text-left transition hover:-translate-y-0.5"
                style={{
                  backgroundColor: item.hex,
                  color: item.dark ? '#FFFFFF' : '#101318',
                }}
              >
                <span className="text-[10px] font-bold opacity-80">{item.weight}</span>
                <span className="font-mono text-[9px] font-bold">{item.hex}</span>
              </button>
            ))}
          </div>
        </div>

        {/* WCAG 2.1 无障碍对比度实验卡 */}
        <div
          className="rounded-xl border border-white/15 p-4 space-y-2 transition"
          style={{ backgroundColor: bgHex, color: primaryHex }}
        >
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div>
              <span className="text-xs font-bold uppercase tracking-wider opacity-75">WCAG 2.1 CONTRAST AUDIT</span>
              <h4 className="text-lg font-extrabold">
                对比度 {contrastRatio.toFixed(2)} : 1
              </h4>
            </div>
            <label className="flex items-center gap-1.5 rounded-full bg-black/30 px-2.5 py-1 text-xs text-white">
              <span>背景色</span>
              <input
                type="color"
                value={bgHex}
                onChange={e => setBgHex(e.target.value.toUpperCase())}
                className="h-5 w-7 cursor-pointer rounded border-0 bg-transparent"
              />
              <code className="font-mono">{bgHex}</code>
            </label>
          </div>
          <p className="text-sm leading-relaxed opacity-90">
            天地玄黄，宇宙洪荒。The quick brown fox jumps over the lazy dog · 0123456789
          </p>
          <div className="flex flex-wrap gap-2 pt-1 text-xs font-bold">
            <span className="rounded-full bg-black/35 px-2.5 py-1 text-white">
              正文 AA (≥4.5): {contrastRatio >= 4.5 ? 'PASS' : 'FAIL'}
            </span>
            <span className="rounded-full bg-black/35 px-2.5 py-1 text-white">
              正文 AAA (≥7.0): {contrastRatio >= 7.0 ? 'PASS' : 'FAIL'}
            </span>
            <span className="rounded-full bg-black/35 px-2.5 py-1 text-white">
              大字号 AA (≥3.0): {contrastRatio >= 3.0 ? 'PASS' : 'FAIL'}
            </span>
          </div>
        </div>
      </div>
    </div>
  );
}
