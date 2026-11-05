import { useEffect, useMemo, useState, type CSSProperties, type FormEvent } from 'react';
import { AuroraMeshCanvas } from './AuroraMeshCanvas';
import {
  CURATED_GRADIENT_TAGS,
  CURATED_GRADIENTS,
  exportGradientWallpaper,
  generateTags,
  getContrastColor,
  getGradientSurfaceVars,
  GRADIENT_DIRECTIONS,
  isGradientDark,
  parseHexList,
  toCssGradient,
  toCssVariables,
  toFallbackCss,
  toJsonSnippet,
  toReactStyle,
  toTailwindConfig,
  type GradientDirection,
  type GradientPreset,
} from './gradientData';

interface GradientAtlasViewProps {
  onCopy: (text: string, label?: string) => void;
}

const FAVORITES_KEY = 'omnihub.gradient-atlas.favorites.v1';
const CUSTOM_KEY = 'omnihub.gradient-atlas.custom.v1';

export function GradientAtlasView({ onCopy }: GradientAtlasViewProps) {
  const [customGradients, setCustomGradients] = useState<GradientPreset[]>(() => {
    try {
      const raw = localStorage.getItem(CUSTOM_KEY);
      return raw ? (JSON.parse(raw) as GradientPreset[]) : [];
    } catch {
      return [];
    }
  });

  const [favorites, setFavorites] = useState<string[]>(() => {
    try {
      const raw = localStorage.getItem(FAVORITES_KEY);
      return raw ? (JSON.parse(raw) as string[]) : ['lunada-7', 'bluelagoo-6', 'quepal-114'];
    } catch {
      return ['lunada-7', 'bluelagoo-6', 'quepal-114'];
    }
  });

  const allGradients = useMemo(
    () => [...customGradients, ...CURATED_GRADIENTS],
    [customGradients],
  );

  const [selectedId, setSelectedId] = useState<string>(() => allGradients[6]?.id ?? allGradients[0].id);
  const [direction, setDirection] = useState<GradientDirection>('135deg');
  const [engine, setEngine] = useState<'optical' | 'aurora'>('optical');
  const [search, setSearch] = useState('');
  const [activeTag, setActiveTag] = useState<string>('all');
  const [recentIds, setRecentIds] = useState<string[]>(() =>
    allGradients.slice(0, 6).map(g => g.id),
  );
  const [customName, setCustomName] = useState('');
  const [customHexInput, setCustomHexInput] = useState('#1488CC, #2B32B2, #89D9BC');

  useEffect(() => {
    try {
      localStorage.setItem(FAVORITES_KEY, JSON.stringify(favorites));
    } catch {
      // ignore
    }
  }, [favorites]);

  useEffect(() => {
    try {
      localStorage.setItem(CUSTOM_KEY, JSON.stringify(customGradients));
    } catch {
      // ignore
    }
  }, [customGradients]);

  const activeGradient = useMemo(
    () => allGradients.find(g => g.id === selectedId) ?? allGradients[0],
    [allGradients, selectedId],
  );

  const filteredGradients = useMemo(() => {
    const q = search.trim().toLowerCase();
    return allGradients.filter(item => {
      if (activeTag === 'featured' && !item.featured) return false;
      if (activeTag === 'favorites' && !favorites.includes(item.id)) return false;
      if (
        activeTag !== 'all' &&
        activeTag !== 'featured' &&
        activeTag !== 'favorites' &&
        !item.tags.includes(activeTag)
      ) {
        return false;
      }
      if (!q) return true;
      return (
        item.name.toLowerCase().includes(q) ||
        item.colors.some(c => c.toLowerCase().includes(q)) ||
        item.tags.some(t => t.toLowerCase().includes(q))
      );
    });
  }, [allGradients, activeTag, favorites, search]);

  const selectGradient = (preset: GradientPreset) => {
    setSelectedId(preset.id);
    setDirection(preset.direction);
    setRecentIds(prev => [preset.id, ...prev.filter(id => id !== preset.id)].slice(0, 7));
  };

  const toggleFavorite = (id: string) => {
    setFavorites(prev => (prev.includes(id) ? prev.filter(x => x !== id) : [id, ...prev]));
  };

  const pickRandom = () => {
    const pool = filteredGradients.length > 0 ? filteredGradients : allGradients;
    const next = pool[Math.floor(Math.random() * pool.length)];
    if (next) selectGradient(next);
  };

  const handleSaveCustom = (e: FormEvent) => {
    e.preventDefault();
    const colors = parseHexList(customHexInput);
    if (colors.length < 2) {
      onCopy('', '至少输入 2 个有效 HEX 色值');
      return;
    }
    const name = customName.trim() || `Custom Aurora ${customGradients.length + 1}`;
    const next: GradientPreset = {
      id: `custom-${Date.now().toString(36)}`,
      name,
      colors,
      direction,
      tags: generateTags(colors),
      featured: true,
      source: 'custom',
    };
    setCustomGradients(prev => [next, ...prev]);
    selectGradient(next);
    setCustomName('');
    onCopy(toCssGradient(next, direction), `已保存自定义渐变 ${name}`);
  };

  const surfaceVars = useMemo<CSSProperties>(
    () => ({
      ...getGradientSurfaceVars(activeGradient.colors),
      '--atlas-current-gradient': toCssGradient(activeGradient, direction),
    } as CSSProperties),
    [activeGradient, direction],
  );

  const darkSurface = isGradientDark(activeGradient.colors);

  return (
    <div
      className="gradient-atlas-stage"
      data-theme={darkSurface ? 'dark' : 'light'}
      style={surfaceVars}
    >
      {/* 全幅动态渐变与流体极光背景层 */}
      <div className="gradient-backdrop" aria-hidden="true">
        <div
          className="gradient-backdrop__paint"
          style={{
            backgroundColor: activeGradient.colors[0],
            backgroundImage: toCssGradient(activeGradient, direction),
          }}
        />
        <AuroraMeshCanvas colors={activeGradient.colors} active={engine === 'aurora'} />
        <div className="gradient-backdrop__glow-a" />
        <div className="gradient-backdrop__glow-b" />
        <div className="gradient-backdrop__vignette" />
        <div className="gradient-backdrop__grain" />
      </div>

      {/* 三栏毛玻璃悬浮工作台 */}
      <div className="gradient-workbench">
        {/* 左栏：120+ 预设检索与标签库 */}
        <aside className="gradient-glass-panel">
          <div style={{ padding: '14px 14px 10px', borderBottom: '1px solid var(--atlas-faint)', display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
            <div>
              <p style={{ margin: 0, fontSize: 10, fontWeight: 900, letterSpacing: '0.16em', color: 'var(--atlas-muted)' }}>
                GRADIENT ATLAS LIBRARY
              </p>
              <h3 style={{ margin: '4px 0 0', fontSize: 16, fontWeight: 900 }}>
                精选光谱库 ({filteredGradients.length})
              </h3>
            </div>
            <span style={{ fontSize: 11, fontWeight: 800, color: 'var(--atlas-muted)' }}>
              收藏 {favorites.length}
            </span>
          </div>

          <div style={{ padding: '10px 12px 8px' }}>
            <input
              value={search}
              onChange={e => setSearch(e.target.value)}
              placeholder="搜索渐变名、HEX 或风格标签…"
              style={{
                width: '100%',
                padding: '9px 11px',
                borderRadius: 8,
                border: '1px solid var(--atlas-border)',
                background: 'rgba(255,255,255,0.08)',
                color: 'var(--atlas-ink)',
                fontSize: 12,
                fontWeight: 700,
                outline: 'none',
              }}
            />
          </div>

          <div
            style={{
              display: 'flex',
              gap: 6,
              padding: '0 12px 10px',
              overflowX: 'auto',
              scrollbarWidth: 'none',
            }}
          >
            {CURATED_GRADIENT_TAGS.map(tag => (
              <button
                key={tag}
                type="button"
                onClick={() => setActiveTag(tag)}
                style={{
                  flex: '0 0 auto',
                  padding: '5px 10px',
                  borderRadius: 999,
                  border: '1px solid var(--atlas-border)',
                  background:
                    activeTag === tag
                      ? 'color-mix(in srgb, var(--atlas-accent), transparent 72%)'
                      : 'rgba(255,255,255,0.07)',
                  color: 'var(--atlas-ink)',
                  fontSize: 11,
                  fontWeight: 800,
                  textTransform: 'capitalize',
                  cursor: 'pointer',
                }}
              >
                {tag}
              </button>
            ))}
          </div>

          <div style={{ flex: 1, overflowY: 'auto', padding: '0 12px 12px', display: 'grid', gap: 8 }}>
            {filteredGradients.map(preset => {
              const isSelected = preset.id === activeGradient.id;
              const isFav = favorites.includes(preset.id);
              return (
                <div
                  key={preset.id}
                  style={{
                    position: 'relative',
                    borderRadius: 8,
                    border: isSelected
                      ? '1px solid color-mix(in srgb, var(--atlas-accent), white 40%)'
                      : '1px solid var(--atlas-border)',
                    background: isSelected
                      ? 'color-mix(in srgb, var(--atlas-accent), transparent 82%)'
                      : 'rgba(255,255,255,0.06)',
                    padding: 9,
                  }}
                >
                  <button
                    type="button"
                    onClick={() => selectGradient(preset)}
                    style={{
                      width: '100%',
                      border: 0,
                      padding: 0,
                      background: 'transparent',
                      color: 'inherit',
                      textAlign: 'left',
                      cursor: 'pointer',
                      display: 'grid',
                      gap: 7,
                    }}
                  >
                    <div
                      style={{
                        height: 40,
                        borderRadius: 6,
                        backgroundImage: toCssGradient(preset),
                        boxShadow: 'inset 0 0 0 1px rgba(255,255,255,0.2)',
                      }}
                    />
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingRight: 28 }}>
                      <strong style={{ fontSize: 13, fontWeight: 850 }}>{preset.name}</strong>
                      <span style={{ fontSize: 10, fontFamily: 'monospace', opacity: 0.78 }}>
                        {preset.colors.join(' → ')}
                      </span>
                    </div>
                  </button>
                  <button
                    type="button"
                    onClick={() => toggleFavorite(preset.id)}
                    title="收藏渐变"
                    style={{
                      position: 'absolute',
                      right: 8,
                      bottom: 8,
                      width: 24,
                      height: 24,
                      borderRadius: 999,
                      border: '1px solid var(--atlas-border)',
                      background: isFav ? 'rgba(255,122,168,0.24)' : 'rgba(255,255,255,0.1)',
                      color: isFav ? '#ff7aa8' : 'var(--atlas-muted)',
                      fontSize: 12,
                      cursor: 'pointer',
                    }}
                  >
                    ♥
                  </button>
                </div>
              );
            })}
          </div>
        </aside>

        {/* 中栏：大字标题 + 引擎切换 + 1600×900 壁纸导出 */}
        <section className="gradient-center-stage">
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ display: 'inline-flex', gap: 6, padding: 4, borderRadius: 999, background: 'var(--atlas-panel)', border: '1px solid var(--atlas-border)', backdropFilter: 'blur(20px)' }}>
              <button
                type="button"
                onClick={() => setEngine('optical')}
                style={{
                  padding: '6px 12px',
                  borderRadius: 999,
                  border: 0,
                  background: engine === 'optical' ? 'rgba(255,255,255,0.22)' : 'transparent',
                  color: 'var(--atlas-ink)',
                  fontSize: 11,
                  fontWeight: 900,
                  cursor: 'pointer',
                }}
              >
                光学线性/径向层
              </button>
              <button
                type="button"
                onClick={() => setEngine('aurora')}
                style={{
                  padding: '6px 12px',
                  borderRadius: 999,
                  border: 0,
                  background: engine === 'aurora' ? 'rgba(255,255,255,0.22)' : 'transparent',
                  color: 'var(--atlas-ink)',
                  fontSize: 11,
                  fontWeight: 900,
                  cursor: 'pointer',
                }}
              >
                流体极光网格 (60fps Mesh)
              </button>
            </div>
          </div>

          <div className="gradient-hero-title">
            <p style={{ margin: '0 0 10px', fontSize: 11, fontWeight: 900, letterSpacing: '0.18em', textTransform: 'uppercase' }}>
              {activeGradient.colors.join('  ·  ')} — {direction}
            </p>
            <h1>{activeGradient.name}</h1>
            <p>
              实时自适应明暗玻璃界面 · 叠加双层屏幕光晕、胶片噪点与流体极光网格。支持一键渲染导出 1600×900 壁纸与全栈工程代码。
            </p>

            {/* 最近浏览渐变色球轨迹 */}
            <div style={{ display: 'flex', gap: 10, marginTop: 22 }}>
              {recentIds.map(id => {
                const item = allGradients.find(g => g.id === id);
                if (!item) return null;
                return (
                  <button
                    key={item.id}
                    type="button"
                    onClick={() => selectGradient(item)}
                    title={item.name}
                    style={{
                      width: 38,
                      height: 38,
                      borderRadius: 999,
                      border:
                        item.id === activeGradient.id
                          ? '2px solid #FFFFFF'
                          : '1px solid var(--atlas-border)',
                      backgroundImage: toCssGradient(item),
                      boxShadow: '0 10px 26px var(--atlas-shadow)',
                      cursor: 'pointer',
                    }}
                  />
                );
              })}
            </div>
          </div>

          {/* 底部悬浮操控台 */}
          <div
            style={{
              borderRadius: 10,
              border: '1px solid var(--atlas-border)',
              background: 'var(--atlas-panel)',
              backdropFilter: 'blur(32px)',
              padding: 12,
              display: 'grid',
              gap: 10,
            }}
          >
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, justifyContent: 'space-between', alignItems: 'center' }}>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {GRADIENT_DIRECTIONS.map(dir => (
                  <button
                    key={dir}
                    type="button"
                    onClick={() => setDirection(dir)}
                    style={{
                      padding: '6px 11px',
                      borderRadius: 999,
                      border: '1px solid var(--atlas-border)',
                      background:
                        direction === dir
                          ? 'color-mix(in srgb, var(--atlas-accent), transparent 72%)'
                          : 'rgba(255,255,255,0.08)',
                      color: 'var(--atlas-ink)',
                      fontSize: 11,
                      fontWeight: 850,
                      cursor: 'pointer',
                    }}
                  >
                    {dir}
                  </button>
                ))}
              </div>

              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                <button
                  type="button"
                  onClick={pickRandom}
                  style={{
                    padding: '7px 12px',
                    borderRadius: 999,
                    border: '1px solid var(--atlas-border)',
                    background: 'rgba(255,255,255,0.1)',
                    color: 'var(--atlas-ink)',
                    fontSize: 11,
                    fontWeight: 850,
                    cursor: 'pointer',
                  }}
                >
                  随机探索
                </button>
                <button
                  type="button"
                  onClick={() => onCopy(toFallbackCss(activeGradient, direction), 'CSS 渐变')}
                  style={{
                    padding: '7px 12px',
                    borderRadius: 999,
                    border: '1px solid var(--atlas-border)',
                    background: 'rgba(255,255,255,0.1)',
                    color: 'var(--atlas-ink)',
                    fontSize: 11,
                    fontWeight: 850,
                    cursor: 'pointer',
                  }}
                >
                  复制 CSS
                </button>
                <button
                  type="button"
                  onClick={() =>
                    exportGradientWallpaper(activeGradient, direction).then(
                      () => onCopy(activeGradient.name, '已导出 1600×900 噪点壁纸 PNG'),
                      () => onCopy('', '壁纸导出失败'),
                    )
                  }
                  style={{
                    padding: '7px 12px',
                    borderRadius: 999,
                    border: '1px solid var(--atlas-border)',
                    background: 'var(--atlas-ink)',
                    color: darkSurface ? '#0B1020' : '#FFFFFF',
                    fontSize: 11,
                    fontWeight: 900,
                    cursor: 'pointer',
                  }}
                >
                  导出 1600×900 壁纸 PNG
                </button>
              </div>
            </div>
          </div>
        </section>

        {/* 右栏：多色标复制 + 5 格式工程代码 + 自定义渐变构建器 */}
        <aside className="gradient-glass-panel" style={{ overflowY: 'auto', padding: 14, gap: 12 }}>
          <div>
            <p style={{ margin: 0, fontSize: 10, fontWeight: 900, letterSpacing: '0.16em', color: 'var(--atlas-muted)' }}>
              STOP INSPECTOR & EXPORT
            </p>
            <h3 style={{ margin: '4px 0 0', fontSize: 16, fontWeight: 900 }}>{activeGradient.name}</h3>
          </div>

          {/* 独立色标卡片 */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(88px, 1fr))', gap: 8 }}>
            {activeGradient.colors.map((color, idx) => (
              <button
                key={`${color}-${idx}`}
                type="button"
                onClick={() => onCopy(color, `STOP 0${idx + 1}`)}
                style={{
                  minHeight: 62,
                  borderRadius: 8,
                  border: 0,
                  padding: 9,
                  backgroundColor: color,
                  color: getContrastColor(color),
                  textAlign: 'left',
                  display: 'flex',
                  flexDirection: 'column',
                  justifyContent: 'space-between',
                  boxShadow: 'inset 0 0 0 1px rgba(255,255,255,0.22)',
                  cursor: 'pointer',
                }}
              >
                <span style={{ fontSize: 10, fontWeight: 900, opacity: 0.78 }}>STOP 0{idx + 1}</span>
                <strong style={{ fontSize: 12, fontFamily: 'monospace' }}>{color}</strong>
              </button>
            ))}
          </div>

          {/* 5 种工程代码片段 */}
          <div style={{ display: 'grid', gap: 8 }}>
            {[
              ['CSS + Fallback', toFallbackCss(activeGradient, direction)],
              ['Tailwind Config', toTailwindConfig(activeGradient, direction)],
              ['CSS Variables', toCssVariables(activeGradient, direction)],
              ['React CSSProperties', toReactStyle(activeGradient, direction)],
              ['JSON Token', toJsonSnippet(activeGradient, direction)],
            ].map(([title, snippet]) => (
              <div
                key={title}
                style={{
                  borderRadius: 8,
                  border: '1px solid var(--atlas-border)',
                  background: 'rgba(0,0,0,0.22)',
                  overflow: 'hidden',
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'space-between',
                    alignItems: 'center',
                    padding: '6px 10px',
                    borderBottom: '1px solid var(--atlas-faint)',
                    fontSize: 11,
                    fontWeight: 900,
                  }}
                >
                  <span>{title}</span>
                  <button
                    type="button"
                    onClick={() => onCopy(snippet, title)}
                    style={{
                      padding: '3px 8px',
                      borderRadius: 999,
                      border: 0,
                      background: 'rgba(255,255,255,0.14)',
                      color: 'var(--atlas-ink)',
                      fontSize: 10,
                      fontWeight: 850,
                      cursor: 'pointer',
                    }}
                  >
                    复制
                  </button>
                </div>
                <pre
                  style={{
                    margin: 0,
                    padding: 9,
                    maxHeight: 92,
                    overflow: 'auto',
                    fontSize: 10,
                    lineHeight: 1.45,
                    fontFamily: 'monospace',
                  }}
                >
                  <code>{snippet}</code>
                </pre>
              </div>
            ))}
          </div>

          {/* 自定义多色标构建器 */}
          <form
            onSubmit={handleSaveCustom}
            style={{
              borderRadius: 8,
              border: '1px solid var(--atlas-border)',
              background: 'rgba(255,255,255,0.06)',
              padding: 12,
              display: 'grid',
              gap: 8,
            }}
          >
            <strong style={{ fontSize: 12, fontWeight: 900 }}>构建并保存自定义渐变</strong>
            <input
              value={customName}
              onChange={e => setCustomName(e.target.value)}
              placeholder="自定义名称（如 Cyber Jade）"
              style={{
                width: '100%',
                padding: '7px 9px',
                borderRadius: 6,
                border: '1px solid var(--atlas-border)',
                background: 'rgba(255,255,255,0.08)',
                color: 'var(--atlas-ink)',
                fontSize: 12,
              }}
            />
            <input
              value={customHexInput}
              onChange={e => setCustomHexInput(e.target.value)}
              placeholder="#FF512F, #DD2476, #7DD3FC"
              style={{
                width: '100%',
                padding: '7px 9px',
                borderRadius: 6,
                border: '1px solid var(--atlas-border)',
                background: 'rgba(255,255,255,0.08)',
                color: 'var(--atlas-ink)',
                fontSize: 12,
                fontFamily: 'monospace',
              }}
            />
            <button
              type="submit"
              style={{
                padding: '8px 10px',
                borderRadius: 6,
                border: 0,
                background: 'var(--atlas-ink)',
                color: darkSurface ? '#0B1020' : '#FFFFFF',
                fontSize: 11,
                fontWeight: 900,
                cursor: 'pointer',
              }}
            >
              保存到本地光谱库
            </button>
          </form>
        </aside>
      </div>
    </div>
  );
}
