import { useEffect, useMemo, useState } from 'react';
import { button, chip, notify, select, store, stored } from './ui';

const card = 'space-y-3 rounded-lg border border-line bg-panel p-3';
const subTabs = [
  ['converter', '色值与对比度'],
  ['gradient', '渐变工作台'],
  ['tradition', '中国传统色'],
] as const;
type SubTab = typeof subTabs[number][0];

function copyText(text: string) {
  if (!text) return;
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(() => notify(`已复制 ${text}`), () => notify('复制失败'));
    return;
  }
  const area = document.createElement('textarea');
  area.value = text;
  document.body.appendChild(area);
  area.select();
  try { document.execCommand('copy'); notify(`已复制 ${text}`); } catch { notify('复制失败'); }
  area.remove();
}

export default function ColorTools() {
  const [sub, setSub] = useState<SubTab>(() => {
    const saved = stored('omnihub.tools.color.sub', 'converter');
    return subTabs.some(([id]) => id === saved) ? (saved as SubTab) : 'converter';
  });
  useEffect(() => store('omnihub.tools.color.sub', sub), [sub]);

  return <div className="space-y-3">
    <div className="flex flex-wrap gap-1.5">
      {subTabs.map(([id, label]) => <button key={id} className={chip(sub === id)} onClick={() => setSub(id)}>{label}</button>)}
    </div>
    {sub === 'converter' ? <ColorConverter /> : null}
    {sub === 'gradient' ? <GradientStudio /> : null}
    {sub === 'tradition' ? <ChineseColors /> : null}
  </div>;
}

// ---------------- Color Math Helpers ----------------
function parseHex(hex: string): [number, number, number] | null {
  const clean = hex.trim().replace(/^#/, '');
  if (/^[0-9a-fA-F]{3}$/.test(clean)) {
    return [
      parseInt(clean[0] + clean[0], 16),
      parseInt(clean[1] + clean[1], 16),
      parseInt(clean[2] + clean[2], 16),
    ];
  }
  if (/^[0-9a-fA-F]{6}$/.test(clean)) {
    return [
      parseInt(clean.slice(0, 2), 16),
      parseInt(clean.slice(2, 4), 16),
      parseInt(clean.slice(4, 6), 16),
    ];
  }
  return null;
}

function rgbToHex(r: number, g: number, b: number): string {
  const clamp = (v: number) => Math.max(0, Math.min(255, Math.round(v)));
  return '#' + [clamp(r), clamp(g), clamp(b)].map(x => x.toString(16).padStart(2, '0')).join('').toUpperCase();
}

function rgbToHsl(r: number, g: number, b: number): [number, number, number] {
  const rn = r / 255, gn = g / 255, bn = b / 255;
  const max = Math.max(rn, gn, bn), min = Math.min(rn, gn, bn);
  let h = 0, s = 0;
  const l = (max + min) / 2;
  if (max !== min) {
    const d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case rn: h = (gn - bn) / d + (gn < bn ? 6 : 0); break;
      case gn: h = (bn - rn) / d + 2; break;
      case bn: h = (rn - gn) / d + 4; break;
    }
    h /= 6;
  }
  return [Math.round(h * 360), Math.round(s * 100), Math.round(l * 100)];
}

function hslToRgb(h: number, s: number, l: number): [number, number, number] {
  const hn = ((h % 360) + 360) % 360 / 360;
  const sn = Math.max(0, Math.min(100, s)) / 100;
  const ln = Math.max(0, Math.min(100, l)) / 100;
  if (sn === 0) {
    const v = Math.round(ln * 255);
    return [v, v, v];
  }
  const hue2rgb = (p: number, q: number, t: number) => {
    let tt = t;
    if (tt < 0) tt += 1;
    if (tt > 1) tt -= 1;
    if (tt < 1 / 6) return p + (q - p) * 6 * tt;
    if (tt < 1 / 2) return q;
    if (tt < 2 / 3) return p + (q - p) * (2 / 3 - tt) * 6;
    return p;
  };
  const q = ln < 0.5 ? ln * (1 + sn) : ln + sn - ln * sn;
  const p = 2 * ln - q;
  return [
    Math.round(hue2rgb(p, q, hn + 1 / 3) * 255),
    Math.round(hue2rgb(p, q, hn) * 255),
    Math.round(hue2rgb(p, q, hn - 1 / 3) * 255),
  ];
}

function rgbToCmyk(r: number, g: number, b: number): [number, number, number, number] {
  const rn = r / 255, gn = g / 255, bn = b / 255;
  const k = 1 - Math.max(rn, gn, bn);
  if (k === 1) return [0, 0, 0, 100];
  return [
    Math.round(((1 - rn - k) / (1 - k)) * 100),
    Math.round(((1 - gn - k) / (1 - k)) * 100),
    Math.round(((1 - bn - k) / (1 - k)) * 100),
    Math.round(k * 100),
  ];
}

function luminance(r: number, g: number, b: number): number {
  const chan = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * chan(r) + 0.7152 * chan(g) + 0.0722 * chan(b);
}

function contrastRatio(rgb1: [number, number, number], rgb2: [number, number, number]): number {
  const l1 = luminance(...rgb1);
  const l2 = luminance(...rgb2);
  return (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05);
}

// ---------------- 1. Color Converter + Shades + WCAG Contrast ----------------
function ColorConverter() {
  const [hex, setHex] = useState('#58A6FF');
  const [bgHex, setBgHex] = useState('#101318');
  const rgb = useMemo<[number, number, number]>(() => parseHex(hex) ?? [88, 166, 255], [hex]);
  const bgRgb = useMemo<[number, number, number]>(() => parseHex(bgHex) ?? [16, 19, 24], [bgHex]);
  const [h, s, l] = useMemo(() => rgbToHsl(...rgb), [rgb]);
  const [c, m, y, k] = useMemo(() => rgbToCmyk(...rgb), [rgb]);
  const ratio = useMemo(() => contrastRatio(rgb, bgRgb), [rgb, bgRgb]);

  const shades = useMemo(() => {
    return [10, 20, 30, 40, 50, 60, 70, 80, 90].map(lightness => {
      const [sr, sg, sb] = hslToRgb(h, s, lightness);
      return rgbToHex(sr, sg, sb);
    });
  }, [h, s]);

  const formats: [string, string][] = [
    ['HEX', rgbToHex(...rgb)],
    ['RGB', `rgb(${rgb[0]}, ${rgb[1]}, ${rgb[2]})`],
    ['HSL', `hsl(${h}, ${s}%, ${l}%)`],
    ['CMYK', `cmyk(${c}%, ${m}%, ${y}%, ${k}%)`],
  ];

  return <div className={card}>
    <div className="flex flex-wrap items-center gap-3">
      <input type="color" className="h-10 w-14 cursor-pointer rounded border border-line bg-background p-0.5" value={rgbToHex(...rgb)} onChange={e => setHex(e.target.value.toUpperCase())} />
      <label className="flex items-center gap-1.5 text-sm">前景 HEX
        <input className={`${select} w-28 font-mono`} value={hex} onChange={e => setHex(e.target.value)} />
      </label>
      <input type="color" className="h-10 w-14 cursor-pointer rounded border border-line bg-background p-0.5" value={rgbToHex(...bgRgb)} onChange={e => setBgHex(e.target.value.toUpperCase())} />
      <label className="flex items-center gap-1.5 text-sm">背景 HEX
        <input className={`${select} w-28 font-mono`} value={bgHex} onChange={e => setBgHex(e.target.value)} />
      </label>
    </div>

    <div className="grid gap-2 sm:grid-cols-2">
      {formats.map(([label, val]) => <div key={label} className="flex items-center justify-between rounded border border-line bg-background px-2.5 py-1.5 text-xs">
        <span className="font-semibold text-muted">{label}</span>
        <code className="font-mono">{val}</code>
        <button className="text-primary hover:underline" onClick={() => copyText(val)}>复制</button>
      </div>)}
    </div>

    <div className="space-y-1.5">
      <p className="text-xs text-muted">明暗色阶（点击色块复制 HEX）</p>
      <div className="grid grid-cols-9 gap-1">
        {shades.map(sh => <button key={sh} className="flex h-12 flex-col items-center justify-end rounded border border-line p-1 text-[10px] font-mono shadow-sm"
          style={{ backgroundColor: sh, color: luminance(...(parseHex(sh) ?? [0, 0, 0])) > 0.45 ? '#101318' : '#ffffff' }}
          onClick={() => copyText(sh)} title={`复制 ${sh}`}>{sh}</button>)}
      </div>
    </div>

    <div className="rounded border border-line p-3" style={{ backgroundColor: rgbToHex(...bgRgb), color: rgbToHex(...rgb) }}>
      <p className="text-base font-semibold">WCAG 对比度预览：{ratio.toFixed(2)} : 1</p>
      <p className="text-xs opacity-90">
        普通正文 AA (≥4.5)：{ratio >= 4.5 ? '通过' : '未达标'} · 普通正文 AAA (≥7.0)：{ratio >= 7 ? '通过' : '未达标'} · 大字号 AA (≥3.0)：{ratio >= 3 ? '通过' : '未达标'}
      </p>
    </div>
  </div>;
}

// ---------------- 2. Gradient Studio (landrop + lyricMVPlayer Gradient Atlas) ----------------
const GRADIENT_PRESETS: { name: string; angle: number; stops: [string, string, string?] }[] = [
  { name: '薄荷深海 · omniHub', angle: 135, stops: ['#101318', '#1f4e5f', '#89d9bc'] },
  { name: '极光紫青 · Aurora', angle: 120, stops: ['#4158D0', '#C850C0', '#FFCC70'] },
  { name: '暮色霓虹 · Dusk', angle: 135, stops: ['#0F2027', '#203A43', '#2C5364'] },
  { name: '落日珊瑚 · Sunset', angle: 135, stops: ['#FF512F', '#DD2476'] },
  { name: '赛博森林 · Cyber Mint', angle: 135, stops: ['#11998e', '#38ef7d'] },
  { name: '午夜星云 · Nebula', angle: 145, stops: ['#1A1F35', '#4B3F72', '#89D9BC'] },
  { name: '流金岁月 · Amber', angle: 120, stops: ['#F2994A', '#F2C94C'] },
  { name: '冰川晨雾 · Glacier', angle: 135, stops: ['#83a4d4', '#b6fbff'] },
  { name: '朱砂墨韵 · Cinnabar', angle: 135, stops: ['#232526', '#9B2915'] },
];

function GradientStudio() {
  const [angle, setAngle] = useState(135);
  const [c1, setC1] = useState('#1F4E5F');
  const [c2, setC2] = useState('#89D9BC');
  const [c3, setC3] = useState('');

  const css = useMemo(() => {
    const stops = c3 ? `${c1} 0%, ${c2} 50%, ${c3} 100%` : `${c1} 0%, ${c2} 100%`;
    return `linear-gradient(${angle}deg, ${stops})`;
  }, [angle, c1, c2, c3]);

  return <div className={card}>
    <div className="h-28 w-full rounded-lg border border-line shadow-inner" style={{ background: css }} />
    <div className="flex flex-wrap items-center gap-3">
      <label className="flex items-center gap-1.5 text-xs text-muted">角度 {angle}°
        <input type="range" min={0} max={360} value={angle} onChange={e => setAngle(Number(e.target.value))} />
      </label>
      <label className="flex items-center gap-1 text-xs">色标 1 <input type="color" value={c1} onChange={e => setC1(e.target.value.toUpperCase())} /></label>
      <label className="flex items-center gap-1 text-xs">色标 2 <input type="color" value={c2} onChange={e => setC2(e.target.value.toUpperCase())} /></label>
      <label className="flex items-center gap-1 text-xs">色标 3（可选）
        <input type="color" value={c3 || '#FFFFFF'} onChange={e => setC3(e.target.value.toUpperCase())} />
        {c3 ? <button className="text-xs text-muted hover:underline" onClick={() => setC3('')}>移除</button> : null}
      </label>
      <button className={button} onClick={() => copyText(`background: ${css};`)}>复制 CSS</button>
    </div>
    <code className="block rounded border border-line bg-background px-2.5 py-1.5 font-mono text-xs">{`background: ${css};`}</code>
    <div className="space-y-1.5 pt-1">
      <p className="text-xs text-muted">精选渐变预设（点击应用）</p>
      <div className="grid gap-2 sm:grid-cols-3">
        {GRADIENT_PRESETS.map(p => {
          const bg = `linear-gradient(${p.angle}deg, ${p.stops.filter(Boolean).join(', ')})`;
          return <button key={p.name} className="flex h-16 items-end rounded-md border border-line p-2 text-left text-xs font-medium text-white shadow-sm"
            style={{ background: bg, textShadow: '0 1px 2px rgba(0,0,0,0.75)' }}
            onClick={() => { setAngle(p.angle); setC1(p.stops[0]); setC2(p.stops[1]); setC3(p.stops[2] ?? ''); }}>
            {p.name}
          </button>;
        })}
      </div>
    </div>
  </div>;
}

// ---------------- 3. Chinese Traditional Colors (2D Catalog) ----------------
const TRADITIONAL_COLORS: { name: string; pinyin: string; hex: string; group: string }[] = [
  { name: '朱砂', pinyin: 'zhusha', hex: '#FF461F', group: '红' },
  { name: '胭脂', pinyin: 'yanzhi', hex: '#9D2933', group: '红' },
  { name: '海棠红', pinyin: 'haitanghong', hex: '#DB5A6B', group: '红' },
  { name: '妃色', pinyin: 'feise', hex: '#ED5736', group: '红' },
  { name: '茜色', pinyin: 'qianse', hex: '#CB3A56', group: '红' },
  { name: '鹅黄', pinyin: 'ehuang', hex: '#FFF143', group: '黄' },
  { name: '缃色', pinyin: 'xiangse', hex: '#F0C239', group: '黄' },
  { name: '琥珀', pinyin: 'hupo', hex: '#CA6924', group: '黄' },
  { name: '秋香色', pinyin: 'qiuxiangse', hex: '#D9B611', group: '黄' },
  { name: '茶白', pinyin: 'chabai', hex: '#F3F9F1', group: '白黑' },
  { name: '松花色', pinyin: 'songhuase', hex: '#BCE672', group: '绿' },
  { name: '竹青', pinyin: 'zhuqing', hex: '#789262', group: '绿' },
  { name: '苍翠', pinyin: 'cangcui', hex: '#519A73', group: '绿' },
  { name: '石绿', pinyin: 'shilv', hex: '#16A951', group: '绿' },
  { name: '缥色', pinyin: 'piaose', hex: '#7FECAD', group: '青' },
  { name: '天青', pinyin: 'tianqing', hex: '#2EC4B6', group: '青' },
  { name: '石青', pinyin: 'shiqing', hex: '#1685A9', group: '青' },
  { name: '碧色', pinyin: 'bise', hex: '#1BD1A5', group: '青' },
  { name: '蔚蓝', pinyin: 'weilan', hex: '#70F3FF', group: '蓝' },
  { name: '群青', pinyin: 'qunqing', hex: '#4C8DAE', group: '蓝' },
  { name: '靛青', pinyin: 'dianqing', hex: '#177CB0', group: '蓝' },
  { name: '藏青', pinyin: 'zangqing', hex: '#2E4E7E', group: '蓝' },
  { name: '黛蓝', pinyin: 'dailan', hex: '#425066', group: '蓝' },
  { name: '丁香色', pinyin: 'dingxiangse', hex: '#CCA4E3', group: '紫' },
  { name: '紫檀', pinyin: 'zitan', hex: '#4C221B', group: '紫' },
  { name: '绛紫', pinyin: 'jiangzi', hex: '#8C4356', group: '紫' },
  { name: '月白', pinyin: 'yuebai', hex: '#D6ECF0', group: '白黑' },
  { name: '玄青', pinyin: 'xuanqing', hex: '#3D3B4F', group: '白黑' },
  { name: '墨色', pinyin: 'mose', hex: '#50616D', group: '白黑' },
  { name: '缁色', pinyin: 'zise', hex: '#493131', group: '白黑' },
];
const GROUPS = ['全部', '红', '黄', '绿', '青', '蓝', '紫', '白黑'] as const;

function ChineseColors() {
  const [group, setGroup] = useState<string>('全部');
  const [query, setQuery] = useState('');
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return TRADITIONAL_COLORS.filter(c => (group === '全部' || c.group === group) && (!q || c.name.includes(q) || c.pinyin.includes(q) || c.hex.toLowerCase().includes(q)));
  }, [group, query]);

  return <div className={card}>
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex flex-wrap gap-1">
        {GROUPS.map(g => <button key={g} className={chip(group === g)} onClick={() => setGroup(g)}>{g}</button>)}
      </div>
      <input className={`${select} min-w-40 flex-1`} value={query} onChange={e => setQuery(e.target.value)} placeholder="搜索色名、拼音或 HEX…" />
    </div>
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 md:grid-cols-5">
      {filtered.map(c => {
        const rgb = parseHex(c.hex) ?? [0, 0, 0];
        const darkText = luminance(...rgb) > 0.45;
        return <button key={c.name} className="flex h-20 flex-col justify-between rounded-md border border-line p-2.5 text-left transition hover:scale-[1.02]"
          style={{ backgroundColor: c.hex, color: darkText ? '#101318' : '#ffffff' }}
          onClick={() => copyText(c.hex)} title={`点击复制 ${c.hex}`}>
          <span className="text-sm font-semibold">{c.name}</span>
          <span className="font-mono text-xs opacity-90">{c.hex}</span>
        </button>;
      })}
    </div>
  </div>;
}
