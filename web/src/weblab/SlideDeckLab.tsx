import { useEffect, useMemo, useRef, useState } from 'react';

type BgEffect = 'torus' | 'aurora' | 'grid';
type ThemeId = 'cyan' | 'amber' | 'emerald';
type SlideLayout = 'hero' | 'points' | 'compare' | 'quote';

interface SlideItem {
  layout: SlideLayout;
  tag: string;
  title: string;
  subtitle: string;
  bullets: string[];
  caption: string;
}

const THEMES: Record<ThemeId, { name: string; primary: string; secondary: string; bg: string; badge: string }> = {
  cyan: {
    name: '深空青蓝',
    primary: '#38bdf8',
    secondary: '#818cf8',
    bg: 'radial-gradient(circle at 20% 20%, #0f172a 0%, #060913 100%)',
    badge: 'border-sky-400/40 bg-sky-500/15 text-sky-300',
  },
  amber: {
    name: '曜石暖金',
    primary: '#fbbf24',
    secondary: '#f97316',
    bg: 'radial-gradient(circle at 20% 20%, #1c1917 0%, #090706 100%)',
    badge: 'border-amber-400/40 bg-amber-500/15 text-amber-300',
  },
  emerald: {
    name: '极光翡翠',
    primary: '#34d399',
    secondary: '#2dd4bf',
    bg: 'radial-gradient(circle at 20% 20%, #06221e 0%, #040b0a 100%)',
    badge: 'border-emerald-400/40 bg-emerald-500/15 text-emerald-300',
  },
};

const DEFAULT_MARKDOWN = `# [hero] Web 实时交互与数学可视化架构
## 从高维状态空间到 60FPS 零依赖渲染管线
- 统一媒体中枢 · 实时物理仿真 · 结构化分镜演示
> 欢迎来到 omniHub 网页幻灯片演示台，支持使用 Markdown 实时编写结构化分镜。
---
# [points] 核心技术支柱
## 摒弃臃肿运行时，直接驱动底层图形接口
- 三维四元数魔方引擎 :: 基于局部标架射线拾取与画家深度排序，支持 2×2 至 6×6 全阶交互与群论逆序求解
- Navier-Stokes 流体场 :: 半浮点纹理 Ping-Pong 缓冲，实时计算散度、涡度约束与雅可比压力泊松迭代
- 傅里叶本轮与 RK4 混沌 :: 支持任意手绘闭合曲线的离散傅里叶分解，以及四阶龙格库塔微扰双摆丝带波
- 声明式分镜幻灯片 :: 用极简 Markdown 语法直接映射 Hero、要点矩阵、双栏对比与演讲提词字幕
> 本页展示了四大交互模块的核心实现机制，每张卡片由列表项自动生成。
---
# [compare] 传统离线视频链路 vs 实时网页幻灯片
## 为什么轻量化实时分镜更适合技术分享与论文精读
- 传统离线渲染工作链 :: 依赖多阶段脚本转换、TTS 离线合成与 Remotion 逐帧编码，修改一处文案需重新渲染数分钟
- 网页实时分镜工作台 :: 毫秒级即时预览、支持 3D 粒子背景与键盘控场，随时暂停进入代码或仿真实验现场交互
> 双栏对比布局适合展示架构演进前后的差异或正反方案权衡。
---
# [quote] 数学是自然的语言，交互是理解的捷径
## 每一个公式都不应止步于静态排版，而应在指尖实时演化
> 按下键盘左右方向键或点击全屏按钮，即可直接开启沉浸式技术演示。`;

function parseMarkdownToSlides(md: string): SlideItem[] {
  const blocks = md
    .split(/\n---\n/)
    .map(s => s.trim())
    .filter(Boolean);

  if (!blocks.length) {
    return [
      {
        layout: 'hero',
        tag: 'SLIDE 01',
        title: '空白演示文稿',
        subtitle: '在右侧编辑器中输入 Markdown 内容即可实时生成幻灯片',
        bullets: [],
        caption: '',
      },
    ];
  }

  return blocks.map((block, idx) => {
    const lines = block.split('\n').map(l => l.trim()).filter(Boolean);
    let layout: SlideLayout = idx === 0 ? 'hero' : 'points';
    let tag = `SCENE 0${idx + 1}`;
    let title = `幻灯片 ${idx + 1}`;
    let subtitle = '';
    const bullets: string[] = [];
    let caption = '';

    for (const line of lines) {
      if (line.startsWith('# ')) {
        const raw = line.slice(2).trim();
        const match = raw.match(/^\[(hero|points|compare|quote)\]\s*(.*)$/i);
        if (match) {
          layout = match[1].toLowerCase() as SlideLayout;
          title = match[2] || title;
        } else {
          title = raw;
        }
      } else if (line.startsWith('## ')) {
        subtitle = line.slice(3).trim();
      } else if (line.startsWith('- ') || line.startsWith('* ')) {
        bullets.push(line.slice(2).trim());
      } else if (line.startsWith('> ')) {
        caption = line.slice(2).trim();
      } else if (line.startsWith('@')) {
        tag = line.slice(1).trim();
      }
    }

    return { layout, tag, title, subtitle, bullets, caption };
  });
}

export default function SlideDeckLab() {
  const [markdown, setMarkdown] = useState(DEFAULT_MARKDOWN);
  const [currentIdx, setCurrentIdx] = useState(0);
  const [theme, setTheme] = useState<ThemeId>('cyan');
  const [bgEffect, setBgEffect] = useState<BgEffect>('torus');
  const [showCaptions, setShowCaptions] = useState(true);
  const [autoPlay, setAutoPlay] = useState(false);
  const [editorOpen, setEditorOpen] = useState(true);

  const stageRef = useRef<HTMLDivElement | null>(null);
  const bgCanvasRef = useRef<HTMLCanvasElement | null>(null);

  const slides = useMemo(() => parseMarkdownToSlides(markdown), [markdown]);
  const safeIdx = Math.min(currentIdx, slides.length - 1);
  const currentSlide = slides[safeIdx] ?? slides[0];
  const activeTheme = THEMES[theme];

  // 自动轮播
  useEffect(() => {
    if (!autoPlay) return;
    const id = window.setInterval(() => {
      setCurrentIdx(i => (i + 1) % slides.length);
    }, 6500);
    return () => window.clearInterval(id);
  }, [autoPlay, slides.length]);

  // 键盘左右翻页
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const tag = (e.target as HTMLElement | null)?.tagName;
      if (tag === 'TEXTAREA' || tag === 'INPUT') return;
      if (e.key === 'ArrowRight' || e.key === 'PageDown') {
        e.preventDefault();
        setCurrentIdx(i => Math.min(slides.length - 1, i + 1));
      } else if (e.key === 'ArrowLeft' || e.key === 'PageUp') {
        e.preventDefault();
        setCurrentIdx(i => Math.max(0, i - 1));
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [slides.length]);

  // 动态背景 Canvas 渲染
  useEffect(() => {
    const canvas = bgCanvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    let t = 0;

    const renderBg = () => {
      t += 0.012;
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const rect = canvas.getBoundingClientRect();
      const w = Math.max(300, Math.floor(rect.width));
      const h = Math.max(240, Math.floor(rect.height));
      if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
        canvas.width = Math.floor(w * dpr);
        canvas.height = Math.floor(h * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, w, h);

      if (bgEffect === 'torus') {
        const cx = w * 0.72;
        const cy = h * 0.48;
        const R = Math.min(w, h) * 0.25;
        const r = R * 0.42;
        ctx.fillStyle = activeTheme.primary;
        for (let u = 0; u < Math.PI * 2; u += 0.28) {
          for (let v = 0; v < Math.PI * 2; v += 0.35) {
            const x0 = (R + r * Math.cos(v)) * Math.cos(u);
            const y0 = (R + r * Math.cos(v)) * Math.sin(u);
            const z0 = r * Math.sin(v);
            // 绕 X 与 Y 轴旋转
            const y1 = y0 * Math.cos(t) - z0 * Math.sin(t);
            const z1 = y0 * Math.sin(t) + z0 * Math.cos(t);
            const x2 = x0 * Math.cos(t * 0.7) + z1 * Math.sin(t * 0.7);
            const z2 = -x0 * Math.sin(t * 0.7) + z1 * Math.cos(t * 0.7);
            const scale = 320 / (380 + z2);
            const sx = cx + x2 * scale;
            const sy = cy + y1 * scale;
            ctx.globalAlpha = Math.max(0.06, Math.min(0.32, (z2 + R) / (2.5 * R)));
            ctx.beginPath();
            ctx.arc(sx, sy, 2.1 * scale, 0, Math.PI * 2);
            ctx.fill();
          }
        }
      } else if (bgEffect === 'aurora') {
        for (let i = 0; i < 3; i++) {
          ctx.beginPath();
          for (let x = 0; x <= w; x += 24) {
            const y =
              h * (0.35 + i * 0.16) +
              Math.sin(x * 0.006 + t * (1.1 + i * 0.3)) * 38 +
              Math.cos(x * 0.012 - t * 0.8) * 22;
            if (x === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          }
          ctx.strokeStyle = i % 2 === 0 ? activeTheme.primary : activeTheme.secondary;
          ctx.globalAlpha = 0.16;
          ctx.lineWidth = 28;
          ctx.stroke();
        }
      } else {
        const step = 36;
        ctx.fillStyle = activeTheme.primary;
        for (let x = step / 2; x < w; x += step) {
          for (let y = step / 2; y < h; y += step) {
            const wave = Math.sin(x * 0.02 + t) * Math.cos(y * 0.02 - t * 0.8);
            if (wave > 0.25) {
              ctx.globalAlpha = (wave - 0.25) * 0.25;
              ctx.fillRect(x - 4, y - 4, 8, 8);
            }
          }
        }
      }

      ctx.globalAlpha = 1;
      rafId = window.requestAnimationFrame(renderBg);
    };

    rafId = window.requestAnimationFrame(renderBg);
    return () => window.cancelAnimationFrame(rafId);
  }, [bgEffect, activeTheme]);

  const toggleFullscreen = () => {
    const el = stageRef.current;
    if (!el) return;
    if (!document.fullscreenElement) {
      void el.requestFullscreen?.();
    } else {
      void document.exitFullscreen?.();
    }
  };

  return (
    <div className={`grid gap-4 ${editorOpen ? 'lg:grid-cols-[1fr_360px]' : 'grid-cols-1'}`}>
      {/* 幻灯片主舞台 */}
      <div
        ref={stageRef}
        className="relative flex min-h-[500px] flex-col justify-between overflow-hidden rounded-xl border border-line select-none"
        style={{ background: activeTheme.bg }}
      >
        <canvas ref={bgCanvasRef} className="pointer-events-none absolute inset-0 h-full w-full" />

        {/* 顶部控制栏 */}
        <div className="relative z-10 flex flex-wrap items-center justify-between gap-2 border-b border-white/10 bg-black/40 px-4 py-2.5 backdrop-blur">
          <div className="flex items-center gap-2 text-xs">
            <span className={`rounded-full border px-2.5 py-0.5 font-mono ${activeTheme.badge}`}>
              {currentSlide.tag} · {safeIdx + 1}/{slides.length}
            </span>
            {(Object.keys(THEMES) as ThemeId[]).map(id => (
              <button
                key={id}
                onClick={() => setTheme(id)}
                className={`rounded px-2 py-1 ${
                  theme === id ? 'bg-white/15 text-white' : 'text-slate-400 hover:text-white'
                }`}
              >
                {THEMES[id].name}
              </button>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs">
            {(
              [
                ['torus', '3D 环面点云'],
                ['aurora', '极光波束'],
                ['grid', '演化矩阵'],
              ] as [BgEffect, string][]
            ).map(([id, label]) => (
              <button
                key={id}
                onClick={() => setBgEffect(id)}
                className={`rounded border px-2 py-1 ${
                  bgEffect === id ? 'border-primary text-white' : 'border-white/15 text-slate-300'
                }`}
              >
                {label}
              </button>
            ))}
            <button
              onClick={() => setAutoPlay(a => !a)}
              className="rounded border border-white/15 px-2.5 py-1 text-slate-200 hover:border-primary"
            >
              {autoPlay ? '停止轮播' : '自动轮播'}
            </button>
            <button
              onClick={() => setEditorOpen(o => !o)}
              className="rounded border border-white/15 px-2.5 py-1 text-slate-200 hover:border-primary"
            >
              {editorOpen ? '收起编辑器' : '展开编辑器'}
            </button>
            <button
              onClick={toggleFullscreen}
              className="rounded bg-primary px-2.5 py-1 text-white hover:opacity-90"
            >
              全屏放映
            </button>
          </div>
        </div>

        {/* 幻灯片正文中枢 */}
        <div className="relative z-10 flex flex-1 flex-col justify-center px-8 py-10 md:px-14">
          {currentSlide.layout === 'hero' ? (
            <div className="max-w-3xl space-y-5">
              <span className={`inline-block rounded-full border px-3 py-1 text-xs font-semibold tracking-wider ${activeTheme.badge}`}>
                OMNIHUB PRESENTATION STAGE
              </span>
              <h1 className="text-3xl font-bold leading-tight text-white md:text-5xl">
                {currentSlide.title}
              </h1>
              {currentSlide.subtitle ? (
                <p className="text-lg text-slate-300 md:text-xl">{currentSlide.subtitle}</p>
              ) : null}
              {currentSlide.bullets.length > 0 ? (
                <div className="flex flex-wrap gap-2 pt-2">
                  {currentSlide.bullets.map((b, i) => (
                    <span key={i} className="rounded-lg border border-white/15 bg-white/5 px-3.5 py-1.5 text-sm text-slate-200 backdrop-blur">
                      {b}
                    </span>
                  ))}
                </div>
              ) : null}
            </div>
          ) : null}

          {currentSlide.layout === 'points' ? (
            <div className="space-y-6">
              <div>
                <h2 className="text-2xl font-bold text-white md:text-3xl">{currentSlide.title}</h2>
                {currentSlide.subtitle ? <p className="mt-1 text-sm text-slate-300">{currentSlide.subtitle}</p> : null}
              </div>
              <div className="grid gap-3.5 sm:grid-cols-2">
                {currentSlide.bullets.map((b, i) => {
                  const [head, desc] = b.split('::').map(s => s.trim());
                  return (
                    <div
                      key={i}
                      className="rounded-xl border border-white/15 bg-black/35 p-4 backdrop-blur transition hover:border-white/30"
                    >
                      <div className="flex items-center gap-2">
                        <span
                          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-bold text-slate-950"
                          style={{ background: activeTheme.primary }}
                        >
                          {i + 1}
                        </span>
                        <h3 className="font-semibold text-white">{head}</h3>
                      </div>
                      {desc ? <p className="mt-2 text-xs leading-relaxed text-slate-300">{desc}</p> : null}
                    </div>
                  );
                })}
              </div>
            </div>
          ) : null}

          {currentSlide.layout === 'compare' ? (
            <div className="space-y-6">
              <div>
                <h2 className="text-2xl font-bold text-white md:text-3xl">{currentSlide.title}</h2>
                {currentSlide.subtitle ? <p className="mt-1 text-sm text-slate-300">{currentSlide.subtitle}</p> : null}
              </div>
              <div className="grid gap-4 md:grid-cols-2">
                {currentSlide.bullets.map((b, i) => {
                  const [head, desc] = b.split('::').map(s => s.trim());
                  const isRight = i % 2 === 1;
                  return (
                    <div
                      key={i}
                      className={`rounded-xl border p-5 backdrop-blur ${
                        isRight ? 'border-sky-400/40 bg-sky-950/25' : 'border-white/15 bg-white/5'
                      }`}
                    >
                      <div className="text-xs font-mono uppercase tracking-wider text-slate-400">
                        {isRight ? 'UPGRADED PARADIGM' : 'LEGACY PIPELINE'}
                      </div>
                      <h3 className="mt-1 text-lg font-bold text-white">{head}</h3>
                      {desc ? <p className="mt-2 text-sm leading-relaxed text-slate-300">{desc}</p> : null}
                    </div>
                  );
                })}
              </div>
            </div>
          ) : null}

          {currentSlide.layout === 'quote' ? (
            <div className="mx-auto max-w-2xl text-center space-y-5">
              <div className="text-5xl font-serif opacity-40" style={{ color: activeTheme.primary }}>
                “
              </div>
              <blockquote className="text-2xl font-bold leading-snug text-white md:text-4xl">
                {currentSlide.title}
              </blockquote>
              {currentSlide.subtitle ? (
                <p className="text-base text-slate-300">{currentSlide.subtitle}</p>
              ) : null}
            </div>
          ) : null}
        </div>

        {/* 底部演讲者提词字幕与翻页控制栏 */}
        <div className="relative z-10 space-y-2 border-t border-white/10 bg-black/50 px-5 py-3 backdrop-blur">
          {showCaptions && currentSlide.caption ? (
            <div className="rounded-lg bg-black/60 px-4 py-2 text-center text-sm text-amber-200">
              {currentSlide.caption}
            </div>
          ) : null}

          <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-slate-300">
            <div className="flex items-center gap-1.5">
              {slides.map((_, idx) => (
                <button
                  key={idx}
                  onClick={() => setCurrentIdx(idx)}
                  aria-label={`跳转到第 ${idx + 1} 页`}
                  className={`h-2 rounded-full transition-all ${
                    idx === safeIdx ? 'w-7 bg-primary' : 'w-2 bg-white/25 hover:bg-white/50'
                  }`}
                />
              ))}
            </div>

            <div className="flex items-center gap-2">
              <label className="flex items-center gap-1.5 cursor-pointer">
                <input
                  type="checkbox"
                  checked={showCaptions}
                  onChange={e => setShowCaptions(e.target.checked)}
                />
                <span>显示提词字幕</span>
              </label>
              <button
                disabled={safeIdx === 0}
                onClick={() => setCurrentIdx(i => Math.max(0, i - 1))}
                className="rounded border border-white/15 px-3 py-1 hover:border-primary disabled:opacity-40"
              >
                上一页 (←)
              </button>
              <button
                disabled={safeIdx >= slides.length - 1}
                onClick={() => setCurrentIdx(i => Math.min(slides.length - 1, i + 1))}
                className="rounded border border-white/15 px-3 py-1 hover:border-primary disabled:opacity-40"
              >
                下一页 (→)
              </button>
            </div>
          </div>
        </div>
      </div>

      {/* 右侧实时 Markdown 分镜编辑器 */}
      {editorOpen ? (
        <section className="flex flex-col rounded-xl border border-line bg-panel p-4 space-y-3">
          <div className="flex items-center justify-between">
            <h3 className="text-sm font-semibold">实时 Markdown 分镜脚本</h3>
            <button
              onClick={() => {
                setMarkdown(DEFAULT_MARKDOWN);
                setCurrentIdx(0);
              }}
              className="rounded border border-line px-2 py-1 text-xs hover:border-primary"
            >
              恢复示例
            </button>
          </div>
          <p className="text-xs text-muted">
            用 <code>---</code> 分隔每页；<code># [hero|points|compare|quote] 标题</code> 指定版式；<code>- 标题 :: 说明</code> 生成卡片；<code>&gt; 字幕</code> 生成底部提词。
          </p>
          <textarea
            value={markdown}
            onChange={e => setMarkdown(e.target.value)}
            className="min-h-[380px] w-full flex-1 rounded-lg border border-line bg-background p-3 font-mono text-xs leading-relaxed"
          />
        </section>
      ) : null}
    </div>
  );
}
