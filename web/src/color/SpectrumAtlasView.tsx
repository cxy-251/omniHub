import { useMemo, useState, type CSSProperties } from 'react';
import { SpectrumDisc3DCanvas } from './SpectrumDisc3DCanvas';
import {
  darkenHex,
  getReadableInk,
  lightenHex,
  SPECTRUM_COLORS,
  SPECTRUM_FAMILIES,
  type SpectrumColor,
  type SpectrumFamily,
} from './spectrumData';

interface SpectrumAtlasViewProps {
  onCopy: (text: string, label?: string) => void;
}

const CMYK_META: Array<{ label: string; stroke: string }> = [
  { label: 'C', stroke: '#009FE3' },
  { label: 'M', stroke: '#E6007E' },
  { label: 'Y', stroke: '#FFCC00' },
  { label: 'K', stroke: '#231F20' },
];

const RGB_META: Array<{ label: string; fill: string }> = [
  { label: 'R', fill: '#E53935' },
  { label: 'G', fill: '#43A047' },
  { label: 'B', fill: '#1E88E5' },
];

export function SpectrumAtlasView({ onCopy }: SpectrumAtlasViewProps) {
  const [mode, setMode] = useState<'3d' | '2d'>('3d');
  const [family, setFamily] = useState<SpectrumFamily | 'all'>('all');
  const [query, setQuery] = useState('');
  const [selectedId, setSelectedId] = useState<string>(() => {
    const celadon = SPECTRUM_COLORS.find(c => c.name === '天青');
    return celadon?.id ?? SPECTRUM_COLORS[0].id;
  });
  const [resetSignal, setResetSignal] = useState(0);

  const selectedColor = useMemo<SpectrumColor>(
    () => SPECTRUM_COLORS.find(c => c.id === selectedId) ?? SPECTRUM_COLORS[0],
    [selectedId],
  );

  const filteredColors = useMemo(() => {
    const q = query.trim().toLowerCase();
    return SPECTRUM_COLORS.filter(c => {
      if (family !== 'all' && c.family !== family) return false;
      if (!q) return true;
      return (
        c.name.includes(q) ||
        c.pinyin.toLowerCase().includes(q) ||
        c.hex.toLowerCase().includes(q) ||
        c.poem.toLowerCase().includes(q)
      );
    });
  }, [family, query]);

  const stageVars = useMemo<CSSProperties>(
    () =>
      ({
        '--active-color': selectedColor.hex,
        '--active-light': lightenHex(selectedColor.hex, 0.34),
        '--active-soft': lightenHex(selectedColor.hex, 0.72),
        '--active-dark': darkenHex(selectedColor.hex, 0.32),
        '--active-ink': getReadableInk(selectedColor.hex),
      }) as CSSProperties,
    [selectedColor],
  );

  const pickRandomColor = () => {
    const pool = filteredColors.length > 0 ? filteredColors : SPECTRUM_COLORS;
    const next = pool[Math.floor(Math.random() * pool.length)];
    if (next) setSelectedId(next.id);
  };

  const stepColor = (delta: number) => {
    const pool = filteredColors.length > 0 ? filteredColors : SPECTRUM_COLORS;
    const idx = pool.findIndex(c => c.id === selectedColor.id);
    const nextIdx = ((idx + delta) % pool.length + pool.length) % pool.length;
    if (pool[nextIdx]) setSelectedId(pool[nextIdx].id);
  };

  const circumference = 2 * Math.PI * 20;

  return (
    <div className="spectrum-stage" style={stageVars}>
      <div className="spectrum-paper-grain" />

      <div className="spectrum-layout">
        {/* 左主视口：顶部栏 + 3D 圆盘地形 / 2D 宣纸竹简长卷 */}
        <div className="spectrum-main-viewport">
          <div className="spectrum-top-bar">
            <div className="spectrum-pill-group" role="tablist" aria-label="Spectrum mode">
              <button
                type="button"
                className="spectrum-pill-btn"
                data-active={mode === '3d'}
                onClick={() => setMode('3d')}
              >
                3D 凝露地形
              </button>
              <button
                type="button"
                className="spectrum-pill-btn"
                data-active={mode === '2d'}
                onClick={() => setMode('2d')}
              >
                2D 宣纸竹简 ({filteredColors.length})
              </button>
            </div>

            <div className="spectrum-family-strip">
              {SPECTRUM_FAMILIES.map(f => (
                <button
                  key={f.id}
                  type="button"
                  className="spectrum-family-chip"
                  data-active={family === f.id}
                  onClick={() => setFamily(f.id)}
                >
                  {f.cn}
                </button>
              ))}
            </div>
          </div>

          {mode === '3d' ? (
            <SpectrumDisc3DCanvas
              colors={filteredColors.length > 0 ? filteredColors : SPECTRUM_COLORS}
              selectedColor={selectedColor}
              resetSignal={resetSignal}
              onSelect={setSelectedId}
            />
          ) : (
            <div className="spectrum-archive-wrap">
              <aside className="spectrum-archive-sidebar">
                <div>
                  <span style={{ fontSize: 10, fontWeight: 900, letterSpacing: '0.14em', opacity: 0.55 }}>
                    CLASSICAL ARCHIVE
                  </span>
                  <h3 style={{ margin: '4px 0 0', fontSize: 17, fontWeight: 900, color: '#14110D' }}>
                    传统名色检索
                  </h3>
                </div>

                <input
                  value={query}
                  onChange={e => setQuery(e.target.value)}
                  placeholder="搜色名 / 拼音 / 诗句 / HEX…"
                  style={{
                    width: '100%',
                    padding: '9px 11px',
                    borderRadius: 8,
                    border: '1px solid rgba(20,16,12,0.18)',
                    background: 'rgba(255,255,255,0.55)',
                    color: '#14110D',
                    fontSize: 12,
                    fontWeight: 700,
                    outline: 'none',
                  }}
                />

                <div style={{ fontSize: 12, color: 'rgba(20,16,12,0.72)', lineHeight: 1.6 }}>
                  <strong style={{ display: 'block', marginBottom: 4, color: '#14110D' }}>
                    {selectedColor.name} · {selectedColor.pinyin}
                  </strong>
                  <span>{selectedColor.poem}</span>
                </div>

                <div
                  style={{
                    marginTop: 'auto',
                    paddingTop: 12,
                    borderTop: '1px solid rgba(20,16,12,0.12)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    color: '#14110D',
                    fontSize: 11,
                    fontWeight: 800,
                  }}
                >
                  <span>当前筛选</span>
                  <strong>{filteredColors.length} / 120 色</strong>
                </div>
              </aside>

              <div className="spectrum-archive-grid">
                {filteredColors.map(color => (
                  <button
                    key={color.id}
                    type="button"
                    className="spectrum-slip-card"
                    data-selected={color.id === selectedColor.id}
                    style={{ '--slip-color': color.hex } as CSSProperties}
                    onClick={() => setSelectedId(color.id)}
                    title={`${color.name} (${color.hex})`}
                  >
                    <span className="spectrum-slip-card__cap" />
                    <span className="spectrum-slip-card__name writing-vertical">{color.name}</span>
                    <span className="spectrum-slip-card__pinyin writing-vertical">{color.pinyin}</span>
                    <span className="spectrum-slip-card__hex">{color.hex}</span>
                    <span className="spectrum-slip-card__dots" aria-hidden="true">
                      {color.rgb.map((ch, idx) => (
                        <span key={idx} style={{ opacity: 0.28 + ch / 360 }} />
                      ))}
                    </span>
                  </button>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* 右侧：宣纸控制栏 + CMYK 圆环仪表盘 + 全格式 Inspector */}
        <aside className="spectrum-rail">
          <div className="spectrum-panel" style={{ padding: 12, display: 'grid', gap: 10 }}>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: 6 }}>
              <button
                type="button"
                className="spectrum-pill-btn"
                style={{ border: '1px solid rgba(20,16,12,0.16)', background: 'rgba(255,255,255,0.45)' }}
                onClick={() => stepColor(-1)}
                title="上一色"
              >
                ← 上色
              </button>
              <button
                type="button"
                className="spectrum-pill-btn"
                style={{ border: '1px solid rgba(20,16,12,0.16)', background: 'rgba(255,255,255,0.45)' }}
                onClick={() => stepColor(1)}
                title="下一色"
              >
                下色 →
              </button>
              <button
                type="button"
                className="spectrum-pill-btn"
                style={{ border: '1px solid rgba(20,16,12,0.16)', background: 'rgba(255,255,255,0.45)' }}
                onClick={pickRandomColor}
                title="随机寻色"
              >
                随机
              </button>
              <button
                type="button"
                className="spectrum-pill-btn"
                style={{ border: '1px solid rgba(20,16,12,0.16)', background: 'rgba(255,255,255,0.45)' }}
                onClick={() => setResetSignal(s => s + 1)}
                disabled={mode !== '3d'}
                title="复位 3D 镜头"
              >
                正视
              </button>
            </div>
          </div>

          <div className="spectrum-panel" style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
            {/* 顶部实色名牌 */}
            <div
              style={{
                minHeight: 112,
                padding: 16,
                backgroundColor: selectedColor.hex,
                color: getReadableInk(selectedColor.hex),
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'space-between',
                transition: 'background-color 360ms ease, color 200ms ease',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, fontWeight: 900, letterSpacing: '0.12em' }}>
                <span>{selectedColor.familyLabel}</span>
                <span>RING 0{selectedColor.ring + 1}</span>
              </div>
              <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8 }}>
                <strong style={{ fontSize: 28, fontFamily: '"Songti SC", "STSong", serif', letterSpacing: '0.06em' }}>
                  {selectedColor.name}
                </strong>
                <code style={{ fontSize: 16, fontWeight: 900 }}>{selectedColor.hex}</code>
              </div>
            </div>

            {/* 拼音与诗句考据 */}
            <div style={{ padding: '12px 16px 8px', color: '#14110D' }}>
              <div style={{ fontSize: 11, fontWeight: 900, letterSpacing: '0.14em', color: 'rgba(20,16,12,0.52)' }}>
                {selectedColor.pinyin}
              </div>
              <p style={{ margin: '4px 0 0', fontSize: 12, fontWeight: 700, color: 'rgba(20,16,12,0.82)', lineHeight: 1.5 }}>
                {selectedColor.poem}
              </p>
            </div>

            {/* 动态 SVG CMYK 环形仪表盘 */}
            <div className="spectrum-cmyk-rings">
              {CMYK_META.map((item, idx) => {
                const val = selectedColor.cmyk[idx];
                const dashOffset = circumference * (1 - val / 100);
                return (
                  <div key={item.label} className="spectrum-cmyk-gauge">
                    <svg width="52" height="52" viewBox="0 0 52 52">
                      <circle
                        cx="26"
                        cy="26"
                        r="20"
                        fill="none"
                        stroke="rgba(20,16,12,0.1)"
                        strokeWidth="4"
                      />
                      <circle
                        cx="26"
                        cy="26"
                        r="20"
                        fill="none"
                        stroke={item.stroke}
                        strokeWidth="4"
                        strokeLinecap="round"
                        strokeDasharray={circumference}
                        strokeDashoffset={dashOffset}
                        transform="rotate(-90 26 26)"
                        style={{ transition: 'stroke-dashoffset 420ms cubic-bezier(0.22, 1, 0.36, 1)' }}
                      />
                      <text
                        x="26"
                        y="30"
                        textAnchor="middle"
                        fontSize="11"
                        fontWeight="900"
                        fill="#14110D"
                      >
                        {val}
                      </text>
                    </svg>
                    <span>{item.label}</span>
                  </div>
                );
              })}
            </div>

            {/* RGB 能量条 */}
            <div className="spectrum-rgb-bars">
              {RGB_META.map((ch, idx) => {
                const val = selectedColor.rgb[idx];
                return (
                  <div key={ch.label} className="spectrum-rgb-bar" style={{ color: '#14110D' }}>
                    <span>{ch.label}</span>
                    <div className="spectrum-rgb-bar__track">
                      <div
                        className="spectrum-rgb-bar__fill"
                        style={{ width: `${(val / 255) * 100}%`, backgroundColor: ch.fill }}
                      />
                    </div>
                    <span style={{ textAlign: 'right', fontFamily: 'monospace' }}>{val}</span>
                  </div>
                );
              })}
            </div>

            {/* 一键复制工程 Token 列表 */}
            <div style={{ padding: '4px 16px 16px', display: 'grid', gap: 6, marginTop: 'auto' }}>
              {[
                ['HEX 色值', selectedColor.hex],
                ['RGB 通道', `rgb(${selectedColor.rgb.join(', ')})`],
                ['CMYK 印刷', `cmyk(${selectedColor.cmyk.join('%, ')}%)`],
                ['HSL 色彩', `hsl(${selectedColor.hsl[0]} ${selectedColor.hsl[1]}% ${selectedColor.hsl[2]}%)`],
                ['CSS 变量', `--china-${selectedColor.slug}: ${selectedColor.hex};`],
                [
                  'JSON 典藏卡',
                  JSON.stringify(
                    {
                      name: selectedColor.name,
                      pinyin: selectedColor.pinyin,
                      hex: selectedColor.hex,
                      rgb: selectedColor.rgb,
                      cmyk: selectedColor.cmyk,
                    },
                    null,
                    2,
                  ),
                ],
              ].map(([label, code]) => (
                <button
                  key={label}
                  type="button"
                  onClick={() => onCopy(code, `${selectedColor.name} ${label}`)}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '7px 11px',
                    borderRadius: 999,
                    border: '1px solid rgba(20,16,12,0.16)',
                    background: 'rgba(255,255,255,0.45)',
                    color: '#14110D',
                    fontSize: 11,
                    fontWeight: 800,
                    cursor: 'pointer',
                  }}
                >
                  <span>复制 {label}</span>
                  <code style={{ fontSize: 10, opacity: 0.72, maxWidth: 165, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {code.replace(/\s+/g, ' ')}
                  </code>
                </button>
              ))}
            </div>
          </div>
        </aside>
      </div>
    </div>
  );
}
