import { useEffect, useRef } from 'react';
import { hexToRgbObj } from './gradientData';

interface AuroraMeshCanvasProps {
  colors: string[];
  active: boolean;
}

export function AuroraMeshCanvas({ colors, active }: AuroraMeshCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const pointerRef = useRef({ x: 0.5, y: 0.5 });

  useEffect(() => {
    if (!active) return;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    const w = 360;
    const h = 220;
    canvas.width = w;
    canvas.height = h;

    const palette = (colors.length >= 2 ? colors : ['#111827', '#38BDF8']).map(hexToRgbObj);

    const onPointerMove = (e: PointerEvent) => {
      const x = e.clientX / Math.max(1, window.innerWidth);
      const y = e.clientY / Math.max(1, window.innerHeight);
      pointerRef.current.x += (x - pointerRef.current.x) * 0.25;
      pointerRef.current.y += (y - pointerRef.current.y) * 0.25;
    };

    window.addEventListener('pointermove', onPointerMove, { passive: true });

    const render = (now: number) => {
      const t = now * 0.00075;
      const base = palette[0];
      ctx.fillStyle = `rgb(${base.r}, ${base.g}, ${base.b})`;
      ctx.fillRect(0, 0, w, h);

      ctx.globalCompositeOperation = 'screen';

      for (let i = 0; i < 5; i++) {
        const c = palette[i % palette.length];
        const phase = i * 1.35;
        const ox =
          (0.5 +
            Math.cos(t * (0.9 + i * 0.17) + phase) * 0.34 +
            (pointerRef.current.x - 0.5) * 0.24) *
          w;
        const oy =
          (0.5 +
            Math.sin(t * (1.05 - i * 0.12) + phase) * 0.34 +
            (pointerRef.current.y - 0.5) * 0.24) *
          h;
        const radius = Math.max(w, h) * (0.58 + (i % 3) * 0.14);

        const grad = ctx.createRadialGradient(ox, oy, radius * 0.04, ox, oy, radius);
        grad.addColorStop(0, `rgba(${c.r}, ${c.g}, ${c.b}, 0.86)`);
        grad.addColorStop(0.5, `rgba(${c.r}, ${c.g}, ${c.b}, 0.38)`);
        grad.addColorStop(1, `rgba(${c.r}, ${c.g}, ${c.b}, 0)`);

        ctx.fillStyle = grad;
        ctx.fillRect(0, 0, w, h);
      }

      ctx.globalCompositeOperation = 'source-over';
      rafId = requestAnimationFrame(render);
    };

    rafId = requestAnimationFrame(render);

    return () => {
      cancelAnimationFrame(rafId);
      window.removeEventListener('pointermove', onPointerMove);
    };
  }, [colors, active]);

  if (!active) return null;

  return (
    <canvas
      ref={canvasRef}
      style={{
        position: 'absolute',
        inset: '-4%',
        width: '108%',
        height: '108%',
        filter: 'blur(26px) saturate(132%)',
      }}
    />
  );
}
