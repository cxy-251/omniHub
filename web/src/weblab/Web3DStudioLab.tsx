import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  SCENE_CATEGORIES,
  STUDIO_SCENES,
  type SceneCategory,
  type StudioSceneSpec,
} from './web3dSceneCatalog';
import { type ActiveSceneHandle } from './web3dEnginesA';
import { mountStudioScene } from './web3dEnginesE';

function buildDefaultParams(spec: StudioSceneSpec): Record<string, any> {
  const out: Record<string, any> = {};
  for (const ctrl of spec.controls) {
    out[ctrl.key] = ctrl.defaultValue;
  }
  return out;
}

export default function Web3DStudioLab() {
  const [category, setCategory] = useState<SceneCategory>('all');
  const [searchQuery, setSearchQuery] = useState('');
  const [activeSceneId, setActiveSceneId] = useState<string>(STUDIO_SCENES[0].id);
  const [paramsByScene, setParamsByScene] = useState<Record<string, Record<string, any>>>(() => {
    const init: Record<string, Record<string, any>> = {};
    for (const s of STUDIO_SCENES) {
      init[s.id] = buildDefaultParams(s);
    }
    return init;
  });
  const [hudText, setHudText] = useState<string>('正在初始化 WebGL 渲染管线...');
  const [audioEnabled, setAudioEnabled] = useState<boolean>(false);
  const [isFullscreen, setIsFullscreen] = useState<boolean>(false);

  const stageWrapRef = useRef<HTMLDivElement | null>(null);
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const handleRef = useRef<ActiveSceneHandle | null>(null);
  const paramsRef = useRef<Record<string, any>>(paramsByScene[activeSceneId] ?? {});
  const audioEnabledRef = useRef<boolean>(false);
  const audioCtxRef = useRef<AudioContext | null>(null);

  const activeSpec = useMemo(
    () => STUDIO_SCENES.find((s) => s.id === activeSceneId) ?? STUDIO_SCENES[0],
    [activeSceneId],
  );

  const filteredScenes = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return STUDIO_SCENES.filter((s) => {
      if (category !== 'all' && s.category !== category) return false;
      if (!q) return true;
      return (
        s.title.toLowerCase().includes(q) ||
        s.subtitle.toLowerCase().includes(q) ||
        s.number.includes(q) ||
        s.tags.some((t) => t.toLowerCase().includes(q))
      );
    });
  }, [category, searchQuery]);

  const currentParams = paramsByScene[activeSpec.id] ?? buildDefaultParams(activeSpec);
  paramsRef.current = currentParams;
  audioEnabledRef.current = audioEnabled;

  const ensureAudioContext = useCallback(() => {
    if (!audioCtxRef.current && typeof window !== 'undefined') {
      const Ctor = window.AudioContext || (window as unknown as { webkitAudioContext: typeof AudioContext }).webkitAudioContext;
      if (Ctor) {
        audioCtxRef.current = new Ctor();
      }
    }
    if (audioCtxRef.current && audioCtxRef.current.state === 'suspended') {
      void audioCtxRef.current.resume();
    }
    return audioCtxRef.current;
  }, []);

  const triggerBellNote = useCallback(
    (freq: number, duration: number) => {
      if (!audioEnabledRef.current) return;
      const actx = ensureAudioContext();
      if (!actx) return;

      const now = actx.currentTime;
      const osc1 = actx.createOscillator();
      const osc2 = actx.createOscillator();
      const gain = actx.createGain();

      osc1.type = 'sine';
      osc1.frequency.setValueAtTime(freq, now);
      osc2.type = 'triangle';
      osc2.frequency.setValueAtTime(freq * 2.0, now);

      gain.gain.setValueAtTime(0.0001, now);
      gain.gain.exponentialRampToValueAtTime(0.11, now + 0.012);
      gain.gain.exponentialRampToValueAtTime(0.0001, now + Math.max(0.08, duration));

      osc1.connect(gain);
      osc2.connect(gain);
      gain.connect(actx.destination);

      osc1.start(now);
      osc2.start(now);
      osc1.stop(now + duration + 0.03);
      osc2.stop(now + duration + 0.03);
    },
    [ensureAudioContext],
  );

  const getAudioPulse = useCallback(() => {
    if (!audioEnabledRef.current) {
      return { bass: 0, mid: 0, treble: 0, enabled: false };
    }
    const t = performance.now() * 0.001;
    const bass = Math.pow(0.5 + 0.5 * Math.sin(t * Math.PI * 2 * 1.9), 3.0);
    const mid = 0.5 + 0.5 * Math.sin(t * Math.PI * 2 * 3.8 + 0.9);
    const treble = 0.5 + 0.5 * Math.cos(t * Math.PI * 2 * 7.6);
    return { bass, mid, treble, enabled: true };
  }, []);

  useEffect(() => {
    const container = viewportRef.current;
    if (!container) return;

    handleRef.current?.dispose();
    handleRef.current = null;

    const handle = mountStudioScene(activeSpec.id, {
      container,
      getParams: () => paramsRef.current,
      onHudUpdate: (txt) => setHudText(txt),
      getAudioPulse,
      triggerBellNote,
    });
    handleRef.current = handle;

    return () => {
      handle.dispose();
      if (handleRef.current === handle) {
        handleRef.current = null;
      }
    };
  }, [activeSpec.id, getAudioPulse, triggerBellNote]);

  useEffect(() => {
    const onFsChange = () => {
      setIsFullscreen(Boolean(document.fullscreenElement));
    };
    document.addEventListener('fullscreenchange', onFsChange);
    return () => document.removeEventListener('fullscreenchange', onFsChange);
  }, []);

  const updateParam = (key: string, value: any) => {
    setParamsByScene((prev) => ({
      ...prev,
      [activeSpec.id]: {
        ...(prev[activeSpec.id] ?? buildDefaultParams(activeSpec)),
        [key]: value,
      },
    }));
  };

  const applyPreset = (presetValues: Record<string, any>) => {
    setParamsByScene((prev) => ({
      ...prev,
      [activeSpec.id]: {
        ...buildDefaultParams(activeSpec),
        ...presetValues,
      },
    }));
  };

  const resetCurrentScene = () => {
    setParamsByScene((prev) => ({
      ...prev,
      [activeSpec.id]: buildDefaultParams(activeSpec),
    }));
    handleRef.current?.reset?.();
  };

  const toggleAudio = () => {
    const next = !audioEnabled;
    setAudioEnabled(next);
    if (next) {
      ensureAudioContext();
      triggerBellNote(440, 0.16);
    }
  };

  const toggleFullscreen = () => {
    const el = stageWrapRef.current;
    if (!el) return;
    if (!document.fullscreenElement) {
      void el.requestFullscreen?.();
    } else {
      void document.exitFullscreen?.();
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
      {/* Category bar + Search */}
      <div
        className="card"
        style={{
          padding: '12px 14px',
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 10,
        }}
      >
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
          {SCENE_CATEGORIES.map((cat) => {
            const active = category === cat.id;
            return (
              <button
                key={cat.id}
                type="button"
                className={active ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm'}
                onClick={() => setCategory(cat.id)}
              >
                {cat.label}
              </button>
            );
          })}
        </div>
        <input
          type="search"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="搜索编号 / 算法 / 关键词 (如 GPGPU, RK4, 克拉尼, 吸引子)..."
          style={{
            minWidth: 250,
            padding: '6px 10px',
            borderRadius: 8,
            border: '1px solid var(--border, rgba(148,163,184,0.22))',
            background: 'var(--bg-elevated, rgba(15,23,42,0.55))',
            color: 'inherit',
            fontSize: 13,
          }}
        />
      </div>

      {/* Horizontal compact scene selector strip */}
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: 'repeat(auto-fill, minmax(215px, 1fr))',
          gap: 8,
          maxHeight: 196,
          overflowY: 'auto',
          paddingRight: 2,
        }}
      >
        {filteredScenes.map((scene) => {
          const selected = scene.id === activeSpec.id;
          return (
            <button
              key={scene.id}
              type="button"
              onClick={() => setActiveSceneId(scene.id)}
              style={{
                textAlign: 'left',
                padding: '9px 11px',
                borderRadius: 10,
                cursor: 'pointer',
                border: selected
                  ? '1px solid rgba(56, 189, 248, 0.85)'
                  : '1px solid rgba(148, 163, 184, 0.16)',
                background: selected
                  ? 'linear-gradient(135deg, rgba(14, 116, 144, 0.34), rgba(30, 41, 59, 0.85))'
                  : 'rgba(15, 23, 42, 0.52)',
                color: 'inherit',
                transition: 'border-color 0.15s ease, background 0.15s ease',
              }}
            >
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 6 }}>
                <span
                  style={{
                    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                    fontSize: 11,
                    padding: '1px 6px',
                    borderRadius: 4,
                    background: selected ? 'rgba(56, 189, 248, 0.25)' : 'rgba(148, 163, 184, 0.14)',
                    color: selected ? '#38bdf8' : 'var(--text-muted, #94a3b8)',
                  }}
                >
                  #{scene.number}
                </span>
                <span style={{ fontSize: 11, color: 'var(--text-muted, #94a3b8)' }}>
                  {scene.tags[0]}
                </span>
              </div>
              <div
                style={{
                  fontWeight: 600,
                  fontSize: 13.5,
                  marginTop: 5,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {scene.title}
              </div>
              <div
                style={{
                  fontSize: 11.5,
                  color: 'var(--text-muted, #94a3b8)',
                  marginTop: 2,
                  whiteSpace: 'nowrap',
                  overflow: 'hidden',
                  textOverflow: 'ellipsis',
                }}
              >
                {scene.subtitle}
              </div>
            </button>
          );
        })}
      </div>

      {/* Main Studio Workbench: 3D Viewport + Live Parameter Inspector */}
      <div
        ref={stageWrapRef}
        style={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr) 330px',
          gap: 14,
          minHeight: isFullscreen ? '100vh' : 580,
          background: isFullscreen ? '#02040a' : 'transparent',
          padding: isFullscreen ? 14 : 0,
        }}
      >
        {/* Left: Interactive 3D Canvas Stage */}
        <div
          className="card"
          style={{
            position: 'relative',
            overflow: 'hidden',
            display: 'flex',
            flexDirection: 'column',
            minHeight: 560,
            padding: 0,
            background: '#03050c',
            border: '1px solid rgba(56, 189, 248, 0.22)',
          }}
        >
          {/* Top overlay HUD */}
          <div
            style={{
              position: 'absolute',
              top: 12,
              left: 14,
              right: 14,
              zIndex: 4,
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              justifyContent: 'space-between',
              gap: 8,
              pointerEvents: 'none',
            }}
          >
            <div
              style={{
                padding: '6px 11px',
                borderRadius: 8,
                background: 'rgba(6, 11, 24, 0.78)',
                backdropFilter: 'blur(8px)',
                border: '1px solid rgba(148, 163, 184, 0.2)',
                fontSize: 12,
                fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                color: '#e2e8f0',
              }}
            >
              <strong style={{ color: '#38bdf8' }}>#{activeSpec.number}</strong> · {hudText}
            </div>

            <div style={{ display: 'flex', gap: 6, pointerEvents: 'auto' }}>
              <button
                type="button"
                className={audioEnabled ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm'}
                onClick={toggleAudio}
                style={{
                  background: audioEnabled ? undefined : 'rgba(15, 23, 42, 0.78)',
                  backdropFilter: 'blur(8px)',
                }}
              >
                {audioEnabled ? '🔊 合成器律动: 开启' : '🔇 合成器律动: 关闭'}
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={resetCurrentScene}
                style={{ background: 'rgba(15, 23, 42, 0.78)', backdropFilter: 'blur(8px)' }}
              >
                重置场景
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={toggleFullscreen}
                style={{ background: 'rgba(15, 23, 42, 0.78)', backdropFilter: 'blur(8px)' }}
              >
                {isFullscreen ? '退出全屏' : '全屏'}
              </button>
            </div>
          </div>

          {/* WebGL / 2D mount container */}
          <div
            ref={viewportRef}
            style={{
              flex: 1,
              width: '100%',
              height: '100%',
              minHeight: 560,
              cursor: 'grab',
            }}
          />

          {/* Bottom interaction hint bar */}
          <div
            style={{
              position: 'absolute',
              bottom: 10,
              left: 14,
              right: 14,
              zIndex: 4,
              display: 'flex',
              justifyContent: 'space-between',
              alignItems: 'center',
              pointerEvents: 'none',
            }}
          >
            <div
              style={{
                padding: '5px 10px',
                borderRadius: 7,
                background: 'rgba(6, 11, 24, 0.74)',
                border: '1px solid rgba(148, 163, 184, 0.16)',
                fontSize: 12,
                color: '#cbd5e1',
              }}
            >
              💡 {activeSpec.interactionHint}
            </div>
          </div>
        </div>

        {/* Right: Parameter Inspector & Presets */}
        <div
          className="card"
          style={{
            padding: 16,
            display: 'flex',
            flexDirection: 'column',
            gap: 14,
            maxHeight: isFullscreen ? 'calc(100vh - 28px)' : 640,
            overflowY: 'auto',
          }}
        >
          <div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span
                style={{
                  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                  fontSize: 12,
                  padding: '2px 7px',
                  borderRadius: 5,
                  background: 'rgba(56, 189, 248, 0.18)',
                  color: '#38bdf8',
                }}
              >
                EXP #{activeSpec.number}
              </span>
              <h3 style={{ margin: 0, fontSize: 16 }}>{activeSpec.title}</h3>
            </div>
            <p style={{ margin: '8px 0 0', fontSize: 12.5, lineHeight: 1.55, color: 'var(--text-muted, #94a3b8)' }}>
              {activeSpec.description}
            </p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 5, marginTop: 8 }}>
              {activeSpec.tags.map((tag) => (
                <span
                  key={tag}
                  style={{
                    fontSize: 11,
                    padding: '2px 7px',
                    borderRadius: 999,
                    background: 'rgba(148, 163, 184, 0.14)',
                    color: '#cbd5e1',
                  }}
                >
                  {tag}
                </span>
              ))}
            </div>
          </div>

          {activeSpec.presets && Object.keys(activeSpec.presets).length > 0 && (
            <div style={{ borderTop: '1px solid rgba(148,163,184,0.16)', paddingTop: 12 }}>
              <div style={{ fontSize: 12, fontWeight: 600, marginBottom: 7, color: '#cbd5e1' }}>
                预设参数组 (Presets)
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
                {Object.entries(activeSpec.presets).map(([presetName, presetVals]) => (
                  <button
                    key={presetName}
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => applyPreset(presetVals)}
                    style={{ fontSize: 12 }}
                  >
                    {presetName}
                  </button>
                ))}
              </div>
            </div>
          )}

          <div
            style={{
              borderTop: '1px solid rgba(148,163,184,0.16)',
              paddingTop: 12,
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
            }}
          >
            <div style={{ fontSize: 12, fontWeight: 600, color: '#cbd5e1' }}>
              实时物理与着色器参数
            </div>

            {activeSpec.controls.map((ctrl) => {
              const val = currentParams[ctrl.key] ?? ctrl.defaultValue;

              if (ctrl.type === 'range') {
                return (
                  <label key={ctrl.key} style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5 }}>
                      <span>{ctrl.label}</span>
                      <span
                        style={{
                          fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
                          color: '#38bdf8',
                        }}
                      >
                        {typeof val === 'number'
                          ? Number.isInteger(ctrl.step)
                            ? val
                            : val.toFixed(ctrl.step < 0.01 ? 3 : 2)
                          : val}
                      </span>
                    </div>
                    <input
                      type="range"
                      min={ctrl.min}
                      max={ctrl.max}
                      step={ctrl.step}
                      value={Number(val)}
                      onChange={(e) => updateParam(ctrl.key, parseFloat(e.target.value))}
                    />
                  </label>
                );
              }

              if (ctrl.type === 'select') {
                return (
                  <label key={ctrl.key} style={{ display: 'flex', flexDirection: 'column', gap: 5 }}>
                    <span style={{ fontSize: 12.5 }}>{ctrl.label}</span>
                    <select
                      value={String(val)}
                      onChange={(e) => updateParam(ctrl.key, e.target.value)}
                      style={{
                        padding: '6px 9px',
                        borderRadius: 7,
                        border: '1px solid rgba(148,163,184,0.24)',
                        background: 'rgba(15, 23, 42, 0.75)',
                        color: 'inherit',
                        fontSize: 12.5,
                      }}
                    >
                      {ctrl.options.map((opt) => (
                        <option key={opt.value} value={opt.value}>
                          {opt.label}
                        </option>
                      ))}
                    </select>
                  </label>
                );
              }

              if (ctrl.type === 'color') {
                return (
                  <label
                    key={ctrl.key}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      fontSize: 12.5,
                    }}
                  >
                    <span>{ctrl.label}</span>
                    <input
                      type="color"
                      value={String(val)}
                      onChange={(e) => updateParam(ctrl.key, e.target.value)}
                      style={{
                        width: 44,
                        height: 26,
                        padding: 0,
                        border: 'none',
                        borderRadius: 4,
                        background: 'transparent',
                        cursor: 'pointer',
                      }}
                    />
                  </label>
                );
              }

              return null;
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
