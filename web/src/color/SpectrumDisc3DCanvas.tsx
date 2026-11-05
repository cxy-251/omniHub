import { useEffect, useMemo, useRef, useState } from 'react';
import {
  buildSpectrumDiscTerrain,
  darkenHex,
  lightenHex,
  type SpectrumColor,
} from './spectrumData';

interface SpectrumDisc3DCanvasProps {
  colors: SpectrumColor[];
  selectedColor: SpectrumColor;
  resetSignal: number;
  onSelect: (id: string) => void;
}

interface ProjectedDisc {
  id: string;
  color: SpectrumColor;
  sx: number;
  sy: number;
  baseSy: number;
  depth: number;
  rx: number;
  ry: number;
  thickness: number;
  selected: boolean;
  hovered: boolean;
  tiltX: number;
  tiltZ: number;
  worldX: number;
  worldY: number;
  worldZ: number;
}

const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v));

export function SpectrumDisc3DCanvas({
  colors,
  selectedColor,
  resetSignal,
  onSelect,
}: SpectrumDisc3DCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const placements = useMemo(() => buildSpectrumDiscTerrain(colors), [colors]);

  const [hoveredColor, setHoveredColor] = useState<SpectrumColor | null>(null);

  // Interactive camera & animation state held in ref for 60fps loop without React re-renders
  const stateRef = useRef({
    yaw: 0.28,
    pitch: 0.62,
    targetYaw: 0.28,
    targetPitch: 0.62,
    zoom: 1.0,
    targetZoom: 1.0,
    pointerX: 0,
    pointerY: 0,
    screenX: -9999,
    screenY: -9999,
    dragging: false,
    dragStartX: 0,
    dragStartY: 0,
    dragMoved: false,
    hoveredId: null as string | null,
    heights: new Map<string, number>(),
    pulseStart: performance.now(),
    pulseOriginX: 0,
    pulseOriginZ: 0,
    pulseHex: selectedColor.hex,
  });

  // Reset camera when resetSignal increments
  useEffect(() => {
    const st = stateRef.current;
    st.targetYaw = 0.28;
    st.targetPitch = 0.62;
    st.targetZoom = 1.0;
  }, [resetSignal]);

  // Trigger droplet + radial wave whenever selectedColor changes
  useEffect(() => {
    const st = stateRef.current;
    const found = placements.find(p => p.color.id === selectedColor.id);
    st.pulseStart = performance.now();
    st.pulseHex = selectedColor.hex;
    if (found) {
      st.pulseOriginX = found.baseX;
      st.pulseOriginZ = found.baseZ;
    } else {
      st.pulseOriginX = 0;
      st.pulseOriginZ = 0;
    }
  }, [selectedColor.id, selectedColor.hex, placements]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    let projectedCache: ProjectedDisc[] = [];

    const resize = () => {
      const rect = canvas.getBoundingClientRect();
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const w = Math.max(300, Math.floor(rect.width * dpr));
      const h = Math.max(300, Math.floor(rect.height * dpr));
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
    };

    resize();
    const ro = new ResizeObserver(resize);
    ro.observe(canvas);

    const render = (now: number) => {
      const st = stateRef.current;
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const width = canvas.width;
      const height = canvas.height;
      const cx = width * 0.5;
      const cy = height * 0.53;

      // Smoothly damp camera angles + parallax
      const parallaxYaw = st.pointerX * 0.22;
      const parallaxPitch = clamp(st.pointerY * 0.14, -0.18, 0.18);
      const goalYaw = st.targetYaw + parallaxYaw;
      const goalPitch = clamp(st.targetPitch + parallaxPitch, 0.34, 1.08);

      st.yaw += (goalYaw - st.yaw) * 0.09;
      st.pitch += (goalPitch - st.pitch) * 0.09;
      st.zoom += (st.targetZoom - st.zoom) * 0.12;

      const cosY = Math.cos(st.yaw);
      const sinY = Math.sin(st.yaw);
      const cosP = Math.cos(st.pitch);
      const sinP = Math.sin(st.pitch);

      const fov = Math.min(width, height) * 0.78 * st.zoom;
      const camDist = 9.2;

      const projectPoint = (wx: number, wy: number, wz: number) => {
        // Rotate around Y (yaw)
        const rx = wx * cosY - wz * sinY;
        const rz1 = wx * sinY + wz * cosY;
        // Rotate around X (pitch)
        const ry = wy * cosP - rz1 * sinP;
        const rz = wy * sinP + rz1 * cosP;
        const zCam = camDist + rz;
        const scale = fov / Math.max(1.5, zCam);
        return {
          sx: cx + rx * scale,
          sy: cy - ry * scale,
          depth: zCam,
          scale,
        };
      };

      ctx.clearRect(0, 0, width, height);

      // Draw subtle concentric orbital guide rings on terrain floor
      ctx.save();
      for (let rIdx = 0; rIdx < 6; rIdx++) {
        const radius = 1.05 + rIdx * 0.88;
        const bowl = Math.pow(rIdx / 5, 1.45) * 0.34 - 0.18;
        ctx.beginPath();
        const segs = 72;
        for (let i = 0; i <= segs; i++) {
          const a = (i / segs) * Math.PI * 2;
          const gx = Math.cos(a) * radius;
          const gz = Math.sin(a) * radius * 0.78;
          const p = projectPoint(gx, bowl - 0.08, gz);
          if (i === 0) ctx.moveTo(p.sx, p.sy);
          else ctx.lineTo(p.sx, p.sy);
        }
        ctx.strokeStyle = 'rgba(20, 16, 12, 0.075)';
        ctx.lineWidth = 1 * dpr;
        ctx.stroke();
      }
      ctx.restore();

      const elapsedSec = (now - st.pulseStart) / 1000;
      const waveActive = elapsedSec >= 0 && elapsedSec < 2.2;

      // Project all discs
      const list: ProjectedDisc[] = [];
      let newHovered: SpectrumColor | null = null;
      let minHoverDist = Infinity;

      for (let i = 0; i < placements.length; i++) {
        const item = placements[i];
        const id = item.color.id;
        const isSelected = id === selectedColor.id;
        const isHovered = id === st.hoveredId;

        // Calculate ripple wave offset from droplet impact
        let rippleY = 0;
        if (waveActive && elapsedSec > 0.22) {
          const dx = item.baseX - st.pulseOriginX;
          const dz = item.baseZ - st.pulseOriginZ;
          const dist = Math.hypot(dx, dz);
          const waveFront = (elapsedSec - 0.22) * 4.2;
          const diff = dist - waveFront;
          if (Math.abs(diff) < 1.15) {
            const env = Math.exp(-dist * 0.38) * Math.max(0, 1 - (elapsedSec - 0.22) / 1.85);
            rippleY = Math.cos(diff * 2.7) * 0.22 * env;
          }
        }

        const idleWave = Math.sin(now * 0.0016 + item.angle * 2 + item.ringIndex * 0.7) * 0.032;
        const targetLift =
          (isSelected ? 0.42 : isHovered ? 0.24 : 0) + rippleY + idleWave;
        const prevLift = st.heights.get(id) ?? 0;
        const currLift = prevLift + (targetLift - prevLift) * 0.14;
        st.heights.set(id, currLift);

        const wy = item.baseY + currLift;
        const proj = projectPoint(item.baseX, wy, item.baseZ);
        const baseProj = projectPoint(item.baseX, item.baseY - 0.06, item.baseZ);

        const baseRadius = 0.31 * item.discScale * (isSelected ? 1.16 : isHovered ? 1.09 : 1.0);
        const rx = Math.max(6 * dpr, baseRadius * proj.scale);
        const aspect = clamp(Math.sin(st.pitch) * 0.82 + (isHovered || isSelected ? 0.08 : 0), 0.36, 0.88);
        const ry = rx * aspect;
        const thickness = Math.max(3.5 * dpr, (isSelected ? 0.14 : 0.095) * proj.scale);

        // Hit test against mouse cursor
        if (!st.dragging && st.screenX >= 0) {
          const ndx = (st.screenX - proj.sx) / (rx * 1.08);
          const ndy = (st.screenY - proj.sy) / (ry * 1.18);
          const d2 = ndx * ndx + ndy * ndy;
          if (d2 <= 1.0 && proj.depth < minHoverDist) {
            minHoverDist = proj.depth;
            newHovered = item.color;
          }
        }

        list.push({
          id,
          color: item.color,
          sx: proj.sx,
          sy: proj.sy,
          baseSy: baseProj.sy,
          depth: proj.depth,
          rx,
          ry,
          thickness,
          selected: isSelected,
          hovered: isHovered,
          tiltX: st.pointerY * 0.15,
          tiltZ: -st.pointerX * 0.15,
          worldX: item.baseX,
          worldY: wy,
          worldZ: item.baseZ,
        });
      }

      if ((newHovered?.id ?? null) !== st.hoveredId) {
        st.hoveredId = newHovered?.id ?? null;
        setHoveredColor(newHovered);
      }

      // Sort back-to-front (Painter's Algorithm)
      list.sort((a, b) => b.depth - a.depth);
      projectedCache = list;

      // Render each 3D disc
      for (let i = 0; i < list.length; i++) {
        const d = list[i];
        const hex = d.color.hex;
        const sideHex = darkenHex(hex, 0.34);
        const rimHex = lightenHex(hex, 0.36);

        ctx.save();

        // 1. Soft ground shadow on the terrain floor
        const shadowAlpha = d.selected ? 0.26 : d.hovered ? 0.2 : 0.13;
        ctx.beginPath();
        ctx.ellipse(
          d.sx,
          d.baseSy + d.thickness * 0.8,
          d.rx * (d.selected ? 1.12 : 0.96),
          d.ry * (d.selected ? 1.08 : 0.92),
          0,
          0,
          Math.PI * 2,
        );
        ctx.fillStyle = `rgba(22, 16, 10, ${shadowAlpha})`;
        ctx.fill();

        // 2. Cylindrical side wall (extrusion between top ellipse and bottom ellipse)
        ctx.beginPath();
        ctx.ellipse(d.sx, d.sy + d.thickness, d.rx, d.ry, 0, 0, Math.PI);
        ctx.lineTo(d.sx - d.rx, d.sy);
        ctx.ellipse(d.sx, d.sy, d.rx, d.ry, 0, Math.PI, 0, true);
        ctx.closePath();
        const sideGrad = ctx.createLinearGradient(d.sx - d.rx, d.sy, d.sx + d.rx, d.sy + d.thickness);
        sideGrad.addColorStop(0, darkenHex(hex, 0.22));
        sideGrad.addColorStop(0.5, sideHex);
        sideGrad.addColorStop(1, darkenHex(hex, 0.46));
        ctx.fillStyle = sideGrad;
        ctx.fill();

        // 3. Top disc face with subtle specular lighting gradient
        ctx.beginPath();
        ctx.ellipse(d.sx, d.sy, d.rx, d.ry, 0, 0, Math.PI * 2);
        const topGrad = ctx.createRadialGradient(
          d.sx - d.rx * 0.28,
          d.sy - d.ry * 0.28,
          d.rx * 0.08,
          d.sx,
          d.sy,
          d.rx * 1.05,
        );
        topGrad.addColorStop(0, lightenHex(hex, d.selected || d.hovered ? 0.24 : 0.14));
        topGrad.addColorStop(0.65, hex);
        topGrad.addColorStop(1, darkenHex(hex, 0.12));
        ctx.fillStyle = topGrad;
        ctx.fill();

        // 4. Jade bevel rim highlight
        ctx.lineWidth = (d.selected ? 2.2 : d.hovered ? 1.6 : 0.9) * dpr;
        ctx.strokeStyle = d.selected
          ? '#FFFFFF'
          : d.hovered
            ? rimHex
            : 'rgba(255, 255, 255, 0.34)';
        ctx.stroke();

        // 5. Inner concentric groove ring on selected/hovered disc
        if (d.selected || d.hovered) {
          ctx.beginPath();
          ctx.ellipse(d.sx, d.sy, d.rx * 0.72, d.ry * 0.72, 0, 0, Math.PI * 2);
          ctx.lineWidth = 1 * dpr;
          ctx.strokeStyle = 'rgba(255, 255, 255, 0.55)';
          ctx.stroke();
        }

        // 6. Droplet fall + dual ripple rings on the selected disc
        if (d.selected && elapsedSec < 1.35) {
          // Falling luminous droplet (0 ~ 0.28s)
          if (elapsedSec < 0.28) {
            const dropProgress = clamp(elapsedSec / 0.28, 0, 1);
            const dropWorldY = d.worldY + (1 - dropProgress) * (1 - dropProgress) * 1.85 + 0.08;
            const dropProj = projectPoint(d.worldX, dropWorldY, d.worldZ);
            const dropRadius = (1 - dropProgress * 0.25) * 6.5 * dpr;

            // Droplet trail
            ctx.beginPath();
            ctx.moveTo(dropProj.sx, dropProj.sy - dropRadius * 2.6);
            ctx.lineTo(dropProj.sx - dropRadius * 0.75, dropProj.sy);
            ctx.arc(dropProj.sx, dropProj.sy, dropRadius * 0.75, Math.PI, 0, true);
            ctx.closePath();
            ctx.fillStyle = lightenHex(hex, 0.42);
            ctx.fill();
          }

          // Dual expanding ripple halos on disc surface (0.20s ~ 1.30s)
          const rippleT = clamp((elapsedSec - 0.2) / 1.05, 0, 1);
          if (rippleT > 0) {
            const rScale1 = 0.25 + rippleT * 1.65;
            const alpha1 = (1 - rippleT) * 0.78;
            ctx.beginPath();
            ctx.ellipse(d.sx, d.sy, d.rx * rScale1, d.ry * rScale1, 0, 0, Math.PI * 2);
            ctx.lineWidth = 2 * dpr;
            ctx.strokeStyle = `rgba(255, 255, 255, ${alpha1.toFixed(3)})`;
            ctx.stroke();

            const rippleT2 = clamp((elapsedSec - 0.34) / 0.95, 0, 1);
            if (rippleT2 > 0) {
              const rScale2 = 0.2 + rippleT2 * 1.35;
              const alpha2 = (1 - rippleT2) * 0.55;
              ctx.beginPath();
              ctx.ellipse(d.sx, d.sy, d.rx * rScale2, d.ry * rScale2, 0, 0, Math.PI * 2);
              ctx.lineWidth = 1.4 * dpr;
              ctx.strokeStyle = lightenHex(hex, 0.45);
              ctx.globalAlpha = alpha2;
              ctx.stroke();
              ctx.globalAlpha = 1;
            }
          }
        }

        ctx.restore();
      }

      rafId = requestAnimationFrame(render);
    };

    rafId = requestAnimationFrame(render);

    const handlePointerMove = (e: PointerEvent) => {
      const rect = canvas.getBoundingClientRect();
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const st = stateRef.current;
      st.screenX = (e.clientX - rect.left) * dpr;
      st.screenY = (e.clientY - rect.top) * dpr;
      st.pointerX = ((e.clientX - rect.left) / Math.max(1, rect.width)) * 2 - 1;
      st.pointerY = ((e.clientY - rect.top) / Math.max(1, rect.height)) * 2 - 1;

      if (st.dragging) {
        const dx = e.clientX - st.dragStartX;
        const dy = e.clientY - st.dragStartY;
        if (Math.hypot(dx, dy) > 4) st.dragMoved = true;
        st.targetYaw += dx * 0.0065;
        st.targetPitch = clamp(st.targetPitch + dy * 0.005, 0.36, 1.05);
        st.dragStartX = e.clientX;
        st.dragStartY = e.clientY;
      }
    };

    const handlePointerDown = (e: PointerEvent) => {
      const st = stateRef.current;
      st.dragging = true;
      st.dragMoved = false;
      st.dragStartX = e.clientX;
      st.dragStartY = e.clientY;
      canvas.setPointerCapture?.(e.pointerId);
    };

    const handlePointerUp = (e: PointerEvent) => {
      const st = stateRef.current;
      const wasClick = !st.dragMoved;
      st.dragging = false;
      canvas.releasePointerCapture?.(e.pointerId);

      if (wasClick) {
        const rect = canvas.getBoundingClientRect();
        const dpr = Math.min(window.devicePixelRatio || 1, 2);
        const clickX = (e.clientX - rect.left) * dpr;
        const clickY = (e.clientY - rect.top) * dpr;
        // Check front-to-back from projectedCache
        for (let i = projectedCache.length - 1; i >= 0; i--) {
          const d = projectedCache[i];
          const ndx = (clickX - d.sx) / (d.rx * 1.12);
          const ndy = (clickY - d.sy) / (d.ry * 1.22);
          if (ndx * ndx + ndy * ndy <= 1.0) {
            onSelect(d.id);
            break;
          }
        }
      }
    };

    const handlePointerLeave = () => {
      const st = stateRef.current;
      st.screenX = -9999;
      st.screenY = -9999;
      st.pointerX = 0;
      st.pointerY = 0;
      st.hoveredId = null;
      setHoveredColor(null);
    };

    const handleWheel = (e: WheelEvent) => {
      e.preventDefault();
      const st = stateRef.current;
      st.targetZoom = clamp(st.targetZoom - e.deltaY * 0.0008, 0.72, 1.48);
    };

    const handleDoubleClick = () => {
      const st = stateRef.current;
      st.targetYaw = 0.28;
      st.targetPitch = 0.62;
      st.targetZoom = 1.0;
    };

    canvas.addEventListener('pointermove', handlePointerMove);
    canvas.addEventListener('pointerdown', handlePointerDown);
    canvas.addEventListener('pointerup', handlePointerUp);
    canvas.addEventListener('pointerleave', handlePointerLeave);
    canvas.addEventListener('wheel', handleWheel, { passive: false });
    canvas.addEventListener('dblclick', handleDoubleClick);

    return () => {
      cancelAnimationFrame(rafId);
      ro.disconnect();
      canvas.removeEventListener('pointermove', handlePointerMove);
      canvas.removeEventListener('pointerdown', handlePointerDown);
      canvas.removeEventListener('pointerup', handlePointerUp);
      canvas.removeEventListener('pointerleave', handlePointerLeave);
      canvas.removeEventListener('wheel', handleWheel);
      canvas.removeEventListener('dblclick', handleDoubleClick);
    };
  }, [placements, selectedColor.id, onSelect]);

  const activeReadout = hoveredColor ?? selectedColor;

  return (
    <div className="spectrum-3d-stage" aria-label="3D Concentric Color Disc Terrain">
      <canvas ref={canvasRef} className="spectrum-3d-canvas" />

      {/* 巨幅宋刻竖排汉字主视觉 */}
      <div className="spectrum-giant-calligraphy writing-vertical" aria-hidden="true">
        {selectedColor.name.split('').map((ch, idx) => (
          <span key={`${selectedColor.id}-${idx}`}>{ch}</span>
        ))}
      </div>

      {/* 左下角古典名色印章与诗词器物考据 */}
      <div className="spectrum-poetic-seal">
        <div className="spectrum-poetic-seal__header">
          <span className="spectrum-poetic-seal__stamp">{activeReadout.familyLabel}</span>
          <span className="spectrum-poetic-seal__pinyin">{activeReadout.pinyin}</span>
          <strong style={{ fontSize: 12, fontFamily: 'monospace' }}>{activeReadout.hex}</strong>
        </div>
        <p className="spectrum-poetic-seal__poem">
          「{activeReadout.name}」—— {activeReadout.poem}
        </p>
      </div>

      {/* 右下角操作状态指示 */}
      <div className="spectrum-3d-hud">
        <span>3D 凝露圆盘地形</span>
        <span>·</span>
        <span>拖拽环绕 / 滚轮缩放 / 点击坠露激波</span>
      </div>
    </div>
  );
}
