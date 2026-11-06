import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { isSolvedState, type SolveResult } from './rubiksSolver';

type Axis = 'x' | 'y' | 'z';
type FaceKey = 'px' | 'nx' | 'py' | 'ny' | 'pz' | 'nz';
type PaletteKey = 'classic' | 'pastel' | 'neon';
type MaterialKey = 'glossy' | 'matte' | 'metal';

interface Vec3 { x: number; y: number; z: number }
interface Quat { x: number; y: number; z: number; w: number }

export interface RubiksMove {
  axis: Axis;
  layers: number[];
  dir: 1 | -1;
  label: string;
}

interface Cubelet {
  id: string;
  home: [number, number, number];
  coords: [number, number, number];
  pos: Vec3;
  rot: Quat;
}

const PALETTES: Record<PaletteKey, { label: string; body: string; faces: Record<FaceKey, string> }> = {
  classic: {
    label: '经典竞速',
    body: '#161920',
    faces: { py: '#f8fafc', ny: '#facc15', px: '#ef4444', nx: '#f97316', pz: '#22c55e', nz: '#3b82f6' },
  },
  pastel: {
    label: '柔和粉彩',
    body: '#222631',
    faces: { py: '#fffbeb', ny: '#fde68a', px: '#fda4af', nx: '#fdba74', pz: '#86efac', nz: '#93c5fd' },
  },
  neon: {
    label: '赛博霓虹',
    body: '#0d1017',
    faces: { py: '#ecfeff', ny: '#fde047', px: '#f43f5e', nx: '#fb923c', pz: '#10b981', nz: '#38bdf8' },
  },
};

const MATERIALS: Record<MaterialKey, { label: string; ambient: number; diffuse: number; specular: number; shininess: number; stickerInset: number }> = {
  glossy: { label: '亮面树脂', ambient: 0.58, diffuse: 0.42, specular: 0.36, shininess: 28, stickerInset: 0.13 },
  matte: { label: '磨砂塑料', ambient: 0.68, diffuse: 0.34, specular: 0.08, shininess: 8, stickerInset: 0.14 },
  metal: { label: '阳极金属', ambient: 0.46, diffuse: 0.50, specular: 0.55, shininess: 42, stickerInset: 0.16 },
};

const LOCAL_FACES: { key: FaceKey; normal: Vec3; uAxis: Vec3; vAxis: Vec3; corners: [Vec3, Vec3, Vec3, Vec3] }[] = [
  { key: 'px', normal: { x: 1, y: 0, z: 0 }, uAxis: { x: 0, y: 0, z: -1 }, vAxis: { x: 0, y: 1, z: 0 }, corners: [{ x: 0.5, y: -0.5, z: 0.5 }, { x: 0.5, y: -0.5, z: -0.5 }, { x: 0.5, y: 0.5, z: -0.5 }, { x: 0.5, y: 0.5, z: 0.5 }] },
  { key: 'nx', normal: { x: -1, y: 0, z: 0 }, uAxis: { x: 0, y: 0, z: 1 }, vAxis: { x: 0, y: 1, z: 0 }, corners: [{ x: -0.5, y: -0.5, z: -0.5 }, { x: -0.5, y: -0.5, z: 0.5 }, { x: -0.5, y: 0.5, z: 0.5 }, { x: -0.5, y: 0.5, z: -0.5 }] },
  { key: 'py', normal: { x: 0, y: 1, z: 0 }, uAxis: { x: 1, y: 0, z: 0 }, vAxis: { x: 0, y: 0, z: -1 }, corners: [{ x: -0.5, y: 0.5, z: 0.5 }, { x: 0.5, y: 0.5, z: 0.5 }, { x: 0.5, y: 0.5, z: -0.5 }, { x: -0.5, y: 0.5, z: -0.5 }] },
  { key: 'ny', normal: { x: 0, y: -1, z: 0 }, uAxis: { x: 1, y: 0, z: 0 }, vAxis: { x: 0, y: 0, z: 1 }, corners: [{ x: -0.5, y: -0.5, z: -0.5 }, { x: 0.5, y: -0.5, z: -0.5 }, { x: 0.5, y: -0.5, z: 0.5 }, { x: -0.5, y: -0.5, z: 0.5 }] },
  { key: 'pz', normal: { x: 0, y: 0, z: 1 }, uAxis: { x: 1, y: 0, z: 0 }, vAxis: { x: 0, y: 1, z: 0 }, corners: [{ x: -0.5, y: -0.5, z: 0.5 }, { x: 0.5, y: -0.5, z: 0.5 }, { x: 0.5, y: 0.5, z: 0.5 }, { x: -0.5, y: 0.5, z: 0.5 }] },
  { key: 'nz', normal: { x: 0, y: 0, z: -1 }, uAxis: { x: -1, y: 0, z: 0 }, vAxis: { x: 0, y: 1, z: 0 }, corners: [{ x: 0.5, y: -0.5, z: -0.5 }, { x: -0.5, y: -0.5, z: -0.5 }, { x: -0.5, y: 0.5, z: -0.5 }, { x: 0.5, y: 0.5, z: -0.5 }] },
];

function vAdd(a: Vec3, b: Vec3): Vec3 { return { x: a.x + b.x, y: a.y + b.y, z: a.z + b.z }; }
function vSub(a: Vec3, b: Vec3): Vec3 { return { x: a.x - b.x, y: a.y - b.y, z: a.z - b.z }; }
function vScale(a: Vec3, s: number): Vec3 { return { x: a.x * s, y: a.y * s, z: a.z * s }; }
function vDot(a: Vec3, b: Vec3): number { return a.x * b.x + a.y * b.y + a.z * b.z; }
function vCross(a: Vec3, b: Vec3): Vec3 {
  return { x: a.y * b.z - a.z * b.y, y: a.z * b.x - a.x * b.z, z: a.x * b.y - a.y * b.x };
}
function vLen(a: Vec3): number { return Math.hypot(a.x, a.y, a.z) || 1; }
function vNorm(a: Vec3): Vec3 { const l = vLen(a); return { x: a.x / l, y: a.y / l, z: a.z / l }; }

function qAxisAngle(axis: Vec3, rad: number): Quat {
  const half = rad * 0.5;
  const s = Math.sin(half);
  return { x: axis.x * s, y: axis.y * s, z: axis.z * s, w: Math.cos(half) };
}
function qMul(a: Quat, b: Quat): Quat {
  return {
    w: a.w * b.w - a.x * b.x - a.y * b.y - a.z * b.z,
    x: a.w * b.x + a.x * b.w + a.y * b.z - a.z * b.y,
    y: a.w * b.y - a.x * b.z + a.y * b.w + a.z * b.x,
    z: a.w * b.z + a.x * b.y - a.y * b.x + a.z * b.w,
  };
}
function qNorm(q: Quat): Quat {
  const l = Math.hypot(q.x, q.y, q.z, q.w) || 1;
  return { x: q.x / l, y: q.y / l, z: q.z / l, w: q.w / l };
}
function qRotVec(q: Quat, v: Vec3): Vec3 {
  const u = { x: q.x, y: q.y, z: q.z };
  const s = q.w;
  const uv = vDot(u, v);
  const uu = vDot(u, u);
  const cross = vCross(u, v);
  return {
    x: 2 * uv * u.x + (s * s - uu) * v.x + 2 * s * cross.x,
    y: 2 * uv * u.y + (s * s - uu) * v.y + 2 * s * cross.y,
    z: 2 * uv * u.z + (s * s - uu) * v.z + 2 * s * cross.z,
  };
}

function axisVec(axis: Axis): Vec3 {
  return axis === 'x' ? { x: 1, y: 0, z: 0 } : axis === 'y' ? { x: 0, y: 1, z: 0 } : { x: 0, y: 0, z: 1 };
}
function coordIdx(axis: Axis): 0 | 1 | 2 {
  return axis === 'x' ? 0 : axis === 'y' ? 1 : 2;
}
function coordOffset(idx: number, size: number, spacing = 1.02): number {
  return (idx - (size - 1) / 2) * spacing;
}

function createCubelets(size: number): Cubelet[] {
  const list: Cubelet[] = [];
  for (let x = 0; x < size; x++) {
    for (let y = 0; y < size; y++) {
      for (let z = 0; z < size; z++) {
        if (x > 0 && x < size - 1 && y > 0 && y < size - 1 && z > 0 && z < size - 1) continue;
        list.push({
          id: `${x}-${y}-${z}`,
          home: [x, y, z],
          coords: [x, y, z],
          pos: { x: coordOffset(x, size), y: coordOffset(y, size), z: coordOffset(z, size) },
          rot: { x: 0, y: 0, z: 0, w: 1 },
        });
      }
    }
  }
  return list;
}

function rotateGridIndex(x: number, y: number, z: number, size: number, axis: Axis, dir: 1 | -1): [number, number, number] {
  const c = (size - 1) / 2;
  const rx = x - c, ry = y - c, rz = z - c;
  let nx = rx, ny = ry, nz = rz;
  if (axis === 'x') {
    ny = dir === 1 ? -rz : rz;
    nz = dir === 1 ? ry : -ry;
  } else if (axis === 'y') {
    nx = dir === 1 ? rz : -rz;
    nz = dir === 1 ? -rx : rx;
  } else {
    nx = dir === 1 ? -ry : ry;
    ny = dir === 1 ? rx : -rx;
  }
  return [Math.round(nx + c), Math.round(ny + c), Math.round(nz + c)];
}

function applyMoveToCubelets(cubelets: Cubelet[], size: number, move: RubiksMove): Cubelet[] {
  const idx = coordIdx(move.axis);
  const stepQ = qAxisAngle(axisVec(move.axis), move.dir * (Math.PI / 2));
  return cubelets.map(cb => {
    if (!move.layers.includes(cb.coords[idx])) return cb;
    const nextCoords = rotateGridIndex(cb.coords[0], cb.coords[1], cb.coords[2], size, move.axis, move.dir);
    return {
      ...cb,
      coords: nextCoords,
      pos: { x: coordOffset(nextCoords[0], size), y: coordOffset(nextCoords[1], size), z: coordOffset(nextCoords[2], size) },
      rot: qNorm(qMul(stepQ, cb.rot)),
    };
  });
}

// 按贴纸颜色判断（解算后中心块可能自转，四元数不一定全等）。
function isCubeSolved(cubelets: Cubelet[], size: number): boolean {
  return isSolvedState(cubelets, size);
}

function invertMove(m: RubiksMove): RubiksMove {
  const invDir: 1 | -1 = m.dir === 1 ? -1 : 1;
  const base = m.label.replace(/'$/, '');
  return { axis: m.axis, layers: [...m.layers], dir: invDir, label: m.label.endsWith("'") ? base : `${base}'` };
}


function formatMoveLabel(axis: Axis, layer: number, size: number, dir: 1 | -1): string {
  if (layer === size - 1) {
    if (axis === 'x') return dir === -1 ? 'R' : "R'";
    if (axis === 'y') return dir === -1 ? 'U' : "U'";
    return dir === -1 ? 'F' : "F'";
  }
  if (layer === 0) {
    if (axis === 'x') return dir === 1 ? 'L' : "L'";
    if (axis === 'y') return dir === 1 ? 'D' : "D'";
    return dir === 1 ? 'B' : "B'";
  }
  return `${axis.toUpperCase()}${layer + 1}${dir === 1 ? '+' : '-'}`;
}

function parseFormulaString(formula: string, size: number): RubiksMove[] {
  const tokens = formula.trim().split(/\s+/).filter(Boolean);
  const out: RubiksMove[] = [];
  const mid = Math.floor(size / 2);
  const allLayers = Array.from({ length: size }, (_, i) => i);

  for (const raw of tokens) {
    const isDouble = raw.endsWith('2') || raw.endsWith("2'");
    const prime = raw.includes("'");
    const base = raw.replace(/[2']/g, '');
    let move: Omit<RubiksMove, 'label'> | null = null;
    if (base === 'R') move = { axis: 'x', layers: [size - 1], dir: prime ? 1 : -1 };
    else if (base === 'L') move = { axis: 'x', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'U') move = { axis: 'y', layers: [size - 1], dir: prime ? 1 : -1 };
    else if (base === 'D') move = { axis: 'y', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'F') move = { axis: 'z', layers: [size - 1], dir: prime ? 1 : -1 };
    else if (base === 'B') move = { axis: 'z', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'M' && size >= 3) move = { axis: 'x', layers: [mid], dir: prime ? -1 : 1 };
    else if (base === 'E' && size >= 3) move = { axis: 'y', layers: [mid], dir: prime ? -1 : 1 };
    else if (base === 'S' && size >= 3) move = { axis: 'z', layers: [mid], dir: prime ? 1 : -1 };
    else if (base === 'x') move = { axis: 'x', layers: allLayers, dir: prime ? 1 : -1 };
    else if (base === 'y') move = { axis: 'y', layers: allLayers, dir: prime ? 1 : -1 };
    else if (base === 'z') move = { axis: 'z', layers: allLayers, dir: prime ? 1 : -1 };
    if (!move) continue;
    const label = `${base}${prime ? "'" : ''}`;
    out.push({ ...move, label });
    if (isDouble) out.push({ ...move, label });
  }
  return out;
}

function shadeHex(hex: string, factor: number, spec: number): string {
  const n = parseInt(hex.slice(1), 16);
  const r = Math.min(255, Math.round(((n >> 16) & 255) * factor + spec * 255));
  const g = Math.min(255, Math.round(((n >> 8) & 255) * factor + spec * 255));
  const b = Math.min(255, Math.round((n & 255) * factor + spec * 255));
  return `rgb(${r},${g},${b})`;
}

function pointInPoly(px: number, py: number, pts: { x: number; y: number }[]): boolean {
  let inside = false;
  for (let i = 0, j = pts.length - 1; i < pts.length; j = i++) {
    const xi = pts[i].x, yi = pts[i].y;
    const xj = pts[j].x, yj = pts[j].y;
    if (((yi > py) !== (yj > py)) && (px < ((xj - xi) * (py - yi)) / (yj - yi + 1e-9) + xi)) {
      inside = !inside;
    }
  }
  return inside;
}

interface HitFace {
  cubelet: Cubelet;
  faceKey: FaceKey;
  worldNormal: Vec3;
  worldU: Vec3;
  worldV: Vec3;
  screenCenter: { x: number; y: number };
}

const PRESET_FORMULAS: { name: string; expr: string; desc: string }[] = [
  { name: '性感公式 (Sexy Move)', expr: "R U R' U'", desc: '最常用的三阶指法触发器，连做 6 次回到原位' },
  { name: 'T-Perm (PLL 换角换棱)', expr: "R U R' U' R' F R2 U' R' U' R U R' F'", desc: 'CFOP 顶层经典 T 字交换公式' },
  { name: 'Sune (小鱼公式)', expr: "R U R' U R U2 R'", desc: 'OLL 顶面翻色高频公式' },
  { name: '棋盘格花样', expr: 'M2 E2 S2', desc: '三阶对称棋盘格图案（再点一次还原）' },
];

export default function RubiksLab() {
  const [size, setSize] = useState<number>(3);
  const [palette, setPalette] = useState<PaletteKey>('classic');
  const [material, setMaterial] = useState<MaterialKey>('glossy');
  const [cubelets, setCubelets] = useState<Cubelet[]>(() => createCubelets(3));
  const [history, setHistory] = useState<RubiksMove[]>([]);
  const [moveCount, setMoveCount] = useState(0);
  const [customFormula, setCustomFormula] = useState("R U R' U'");
  const [clickDir, setClickDir] = useState<1 | -1>(-1);
  const [solveSpeed, setSolveSpeed] = useState(1.2);
  const [solverPaused, setSolverPaused] = useState(false);
  const [solverStatus, setSolverStatus] = useState<{ total: number; current: number; moveLabel: string; mode: string } | null>(null);
  const [timerRunning, setTimerRunning] = useState(false);
  const [elapsedMs, setElapsedMs] = useState(0);

  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const cubeletsRef = useRef<Cubelet[]>(cubelets);
  cubeletsRef.current = cubelets;
  const historyRef = useRef<RubiksMove[]>(history);
  historyRef.current = history;

  // 相机球坐标与缩放
  const camRef = useRef({ yaw: -0.68, pitch: 0.52, distance: 7.5 });
  // 当前正在播放的转层动画与队列
  const activeAnimRef = useRef<{ move: RubiksMove; angle: number; target: number; isSolver: boolean; isUndo: boolean } | null>(null);
  const queueRef = useRef<{ move: RubiksMove; isSolver?: boolean; isUndo?: boolean }[]>([]);
  const solverPausedRef = useRef(solverPaused);
  solverPausedRef.current = solverPaused;
  const solveSpeedRef = useRef(solveSpeed);
  solveSpeedRef.current = solveSpeed;

  // 最近一帧绘制出的可见贴纸面，用于高精度鼠标拾取与拖拽转层
  const hitFacesRef = useRef<HitFace[]>([]);
  const dragStateRef = useRef<{
    type: 'orbit' | 'sticker';
    startX: number;
    startY: number;
    lastX: number;
    lastY: number;
    moved: boolean;
    hit?: HitFace;
  } | null>(null);

  const solved = useMemo(() => isCubeSolved(cubelets, size), [cubelets, size]);

  // 计时器更新
  useEffect(() => {
    if (!timerRunning) return;
    const t0 = performance.now() - elapsedMs;
    const id = window.setInterval(() => {
      setElapsedMs(Math.floor(performance.now() - t0));
    }, 50);
    return () => window.clearInterval(id);
  }, [timerRunning]);

  // 魔方复原后自动停表
  useEffect(() => {
    if (solved && timerRunning && moveCount > 0) {
      setTimerRunning(false);
    }
  }, [solved, timerRunning, moveCount]);

  const resetCube = useCallback((nextSize = size) => {
    queueRef.current = [];
    activeAnimRef.current = null;
    setSolverPaused(false);
    setSolverStatus(null);
    const fresh = createCubelets(nextSize);
    cubeletsRef.current = fresh;
    setCubelets(fresh);
    setHistory([]);
    setMoveCount(0);
    setTimerRunning(false);
    setElapsedMs(0);
    camRef.current.distance = nextSize * 2.35 + 0.8;
  }, [size]);

  const changeSize = (nextSize: number) => {
    setSize(nextSize);
    resetCube(nextSize);
  };

  const enqueueMoves = useCallback((moves: RubiksMove[], opts?: { isSolver?: boolean; isUndo?: boolean; modeLabel?: string }) => {
    if (!moves.length) return;
    if (opts?.isSolver) {
      setSolverStatus({ total: moves.length, current: 0, moveLabel: moves[0].label, mode: opts.modeLabel ?? '代数消解 + 层先优化' });
    }
    for (const move of moves) {
      queueRef.current.push({ move, isSolver: opts?.isSolver, isUndo: opts?.isUndo });
    }
  }, []);

  const scrambleCube = useCallback(() => {
    if (activeAnimRef.current || queueRef.current.length > 0) return;
    const axes: Axis[] = ['x', 'y', 'z'];
    const dirs: (1 | -1)[] = [1, -1];
    const steps = size <= 3 ? 20 : size * 7;
    const moves: RubiksMove[] = [];
    let lastAxis: Axis | null = null;
    for (let i = 0; i < steps; i++) {
      const avail = axes.filter(a => a !== lastAxis);
      const axis = avail[Math.floor(Math.random() * avail.length)];
      const layer = Math.floor(Math.random() * size);
      const dir = dirs[Math.floor(Math.random() * dirs.length)];
      moves.push({ axis, layers: [layer], dir, label: formatMoveLabel(axis, layer, size, dir) });
      lastAxis = axis;
    }
    setSolverStatus(null);
    setElapsedMs(0);
    setTimerRunning(true);
    enqueueMoves(moves);
  }, [size, enqueueMoves]);

  const undoLastMove = useCallback(() => {
    if (activeAnimRef.current || queueRef.current.length > 0) return;
    const last = historyRef.current[historyRef.current.length - 1];
    if (!last) return;
    setHistory(prev => prev.slice(0, -1));
    enqueueMoves([invertMove(last)], { isUndo: true });
  }, [enqueueMoves]);

  const solverWorkerRef = useRef<Worker | null>(null);
  const [solving, setSolving] = useState(false);
  const [solveNote, setSolveNote] = useState('');
  useEffect(() => () => solverWorkerRef.current?.terminate(), []);

  // 按当前贴纸状态求解（与打乱/操作历史无关）：3×3 用 Kociemba 两阶段，2×2 用 BFS 最优解。
  const startSmartSolve = useCallback(() => {
    if (activeAnimRef.current || queueRef.current.length > 0 || solved || solving) return;
    if (size > 3) { setSolveNote('4 阶及以上暂未实现状态解算'); return; }
    if (!solverWorkerRef.current) {
      solverWorkerRef.current = new Worker(new URL('./rubiksWorker.ts', import.meta.url), { type: 'module' });
    }
    const worker = solverWorkerRef.current;
    const id = Date.now();
    const t0 = performance.now();
    setSolving(true);
    setSolveNote('正在根据当前状态求解…（首次需构建查找表）');
    worker.onmessage = (e: MessageEvent<{ id: number; result: SolveResult | null; error?: string }>) => {
      if (e.data.id !== id) return;
      setSolving(false);
      const r = e.data.result;
      if (!r) { setSolveNote(`求解失败${e.data.error ? `：${e.data.error}` : ''}`); return; }
      const ms = Math.round(performance.now() - t0);
      setSolveNote(`${r.method}：${r.moves.length} 步${r.rotation ? `（先整体转体 ${r.rotation}）` : ''}，用时 ${ms} ms · ${r.formula}`);
      const moves = parseFormulaString(r.formula, size);
      if (!moves.length) return;
      setSolverPaused(false);
      enqueueMoves(moves, { isSolver: true, modeLabel: `${r.method}（${r.moves.length} 步）` });
    };
    worker.postMessage({ id, cubelets: cubeletsRef.current, size });
  }, [solved, solving, size, enqueueMoves]);

  const stepSolverOnce = useCallback(() => {
    if (!solverPausedRef.current || activeAnimRef.current || queueRef.current.length === 0) return;
    const next = queueRef.current.shift()!;
    activeAnimRef.current = {
      move: next.move,
      angle: 0,
      target: next.move.dir * (Math.PI / 2),
      isSolver: !!next.isSolver,
      isUndo: !!next.isUndo,
    };
    setSolverStatus(prev => prev ? { ...prev, current: prev.current + 1, moveLabel: next.move.label } : null);
  }, []);

  // 主 3D 渲染与动画循环
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    let rafId = 0;
    let lastTime = performance.now();

    const render = (now: number) => {
      const dt = Math.min(0.05, (now - lastTime) / 1000);
      lastTime = now;

      // 推进队列与转层动画
      if (!activeAnimRef.current && queueRef.current.length > 0) {
        const peek = queueRef.current[0];
        if (!peek.isSolver || !solverPausedRef.current) {
          const next = queueRef.current.shift()!;
          activeAnimRef.current = {
            move: next.move,
            angle: 0,
            target: next.move.dir * (Math.PI / 2),
            isSolver: !!next.isSolver,
            isUndo: !!next.isUndo,
          };
          if (next.isSolver) {
            setSolverStatus(prev => prev ? { ...prev, current: prev.current + 1, moveLabel: next.move.label } : null);
          }
        }
      }

      if (activeAnimRef.current) {
        const anim = activeAnimRef.current;
        const speed = (anim.isSolver ? 7.5 * solveSpeedRef.current : 10.5);
        const step = anim.move.dir * speed * dt;
        if (Math.abs(anim.angle + step) >= Math.abs(anim.target)) {
          const updated = applyMoveToCubelets(cubeletsRef.current, size, anim.move);
          cubeletsRef.current = updated;
          setCubelets(updated);
          if (!anim.isUndo && !anim.isSolver) {
            setHistory(prev => [...prev, anim.move]);
          }
          setMoveCount(c => c + 1);
          activeAnimRef.current = null;
          if (queueRef.current.length === 0 && anim.isSolver) {
            setSolverStatus(null);
            setHistory([]);
          }
        } else {
          anim.angle += step;
        }
      }

      // 视口尺寸同步
      const dpr = Math.min(window.devicePixelRatio || 1, 2);
      const rect = canvas.getBoundingClientRect();
      const w = Math.max(300, Math.floor(rect.width));
      const h = Math.max(260, Math.floor(rect.height));
      if (canvas.width !== Math.floor(w * dpr) || canvas.height !== Math.floor(h * dpr)) {
        canvas.width = Math.floor(w * dpr);
        canvas.height = Math.floor(h * dpr);
      }
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

      // 背景绘制
      const bgGrad = ctx.createRadialGradient(w * 0.5, h * 0.45, 20, w * 0.5, h * 0.5, Math.max(w, h) * 0.7);
      bgGrad.addColorStop(0, '#1c2433');
      bgGrad.addColorStop(1, '#0b0f17');
      ctx.fillStyle = bgGrad;
      ctx.fillRect(0, 0, w, h);

      // 相机矩阵
      const { yaw, pitch, distance } = camRef.current;
      const eye: Vec3 = {
        x: distance * Math.cos(pitch) * Math.sin(yaw),
        y: distance * Math.sin(pitch),
        z: distance * Math.cos(pitch) * Math.cos(yaw),
      };
      const forward = vNorm(vScale(eye, -1));
      const right = vNorm(vCross(forward, { x: 0, y: 1, z: 0 }));
      const up = vCross(right, forward);
      const fovScale = Math.min(w, h) * 0.88;
      const lightDir = vNorm({ x: 0.45, y: 0.78, z: 0.42 });

      const project = (p: Vec3) => {
        const rel = vSub(p, eye);
        const cx = vDot(rel, right);
        const cy = vDot(rel, up);
        const cz = vDot(rel, forward);
        const s = fovScale / Math.max(0.5, cz);
        return { x: w * 0.5 + cx * s, y: h * 0.5 - cy * s, z: cz };
      };

      const pal = PALETTES[palette];
      const mat = MATERIALS[material];
      const anim = activeAnimRef.current;
      const animIdx = anim ? coordIdx(anim.move.axis) : 0;
      const animQ = anim ? qAxisAngle(axisVec(anim.move.axis), anim.angle) : null;

      interface DrawQuad {
        depth: number;
        bodyPts: { x: number; y: number }[];
        stickerPts: { x: number; y: number }[] | null;
        bodyFill: string;
        stickerFill: string;
        hit?: HitFace;
      }

      const quads: DrawQuad[] = [];
      const currentCubelets = cubeletsRef.current;

      for (const cb of currentCubelets) {
        const inAnim = !!(anim && animQ && anim.move.layers.includes(cb.coords[animIdx]));
        const worldPos = inAnim && animQ ? qRotVec(animQ, cb.pos) : cb.pos;
        const worldRot = inAnim && animQ ? qMul(animQ, cb.rot) : cb.rot;

        for (const lf of LOCAL_FACES) {
          const wn = qRotVec(worldRot, lf.normal);
          const faceCenter = vAdd(worldPos, vScale(wn, 0.47));
          const toEye = vNorm(vSub(eye, faceCenter));
          // 背面剔除
          if (vDot(wn, toEye) <= 0.02) continue;

          // 判断该面是否为原初始外表面（有贴纸）
          const [hx, hy, hz] = cb.home;
          const hasSticker =
            (lf.key === 'px' && hx === size - 1) ||
            (lf.key === 'nx' && hx === 0) ||
            (lf.key === 'py' && hy === size - 1) ||
            (lf.key === 'ny' && hy === 0) ||
            (lf.key === 'pz' && hz === size - 1) ||
            (lf.key === 'nz' && hz === 0);

          const ndotl = Math.max(0, vDot(wn, lightDir));
          const halfVec = vNorm(vAdd(lightDir, toEye));
          const spec = Math.pow(Math.max(0, vDot(wn, halfVec)), mat.shininess) * mat.specular;
          const factor = mat.ambient + mat.diffuse * ndotl;

          const bodyScale = 0.94;
          const bodyPts = lf.corners.map(c => {
            const p = vAdd(worldPos, qRotVec(worldRot, vScale(c, bodyScale)));
            return project(p);
          });
          const avgZ = (bodyPts[0].z + bodyPts[1].z + bodyPts[2].z + bodyPts[3].z) * 0.25;

          let stickerPts: { x: number; y: number }[] | null = null;
          let hit: HitFace | undefined;
          if (hasSticker) {
            const stScale = 1 - mat.stickerInset * 2;
            stickerPts = lf.corners.map(c => {
              const localS: Vec3 = {
                x: lf.normal.x !== 0 ? c.x * bodyScale + lf.normal.x * 0.008 : c.x * stScale,
                y: lf.normal.y !== 0 ? c.y * bodyScale + lf.normal.y * 0.008 : c.y * stScale,
                z: lf.normal.z !== 0 ? c.z * bodyScale + lf.normal.z * 0.008 : c.z * stScale,
              };
              return project(vAdd(worldPos, qRotVec(worldRot, localS)));
            });
            const centerProj = project(faceCenter);
            hit = {
              cubelet: cb,
              faceKey: lf.key,
              worldNormal: wn,
              worldU: qRotVec(worldRot, lf.uAxis),
              worldV: qRotVec(worldRot, lf.vAxis),
              screenCenter: { x: centerProj.x, y: centerProj.y },
            };
          }

          quads.push({
            depth: avgZ,
            bodyPts,
            stickerPts,
            bodyFill: shadeHex(pal.body, factor, spec * 0.45),
            stickerFill: hasSticker ? shadeHex(pal.faces[lf.key], factor, spec) : '',
            hit,
          });
        }
      }

      // 画家算法由远及近排序
      quads.sort((a, b) => b.depth - a.depth);
      const nextHits: HitFace[] = [];

      for (const q of quads) {
        ctx.beginPath();
        ctx.moveTo(q.bodyPts[0].x, q.bodyPts[0].y);
        for (let i = 1; i < q.bodyPts.length; i++) ctx.lineTo(q.bodyPts[i].x, q.bodyPts[i].y);
        ctx.closePath();
        ctx.fillStyle = q.bodyFill;
        ctx.fill();
        ctx.strokeStyle = 'rgba(0,0,0,0.55)';
        ctx.lineWidth = 1.1;
        ctx.stroke();

        if (q.stickerPts && q.hit) {
          ctx.beginPath();
          ctx.moveTo(q.stickerPts[0].x, q.stickerPts[0].y);
          for (let i = 1; i < q.stickerPts.length; i++) ctx.lineTo(q.stickerPts[i].x, q.stickerPts[i].y);
          ctx.closePath();
          ctx.fillStyle = q.stickerFill;
          ctx.fill();
          ctx.strokeStyle = 'rgba(15,23,42,0.45)';
          ctx.lineWidth = 0.9;
          ctx.stroke();
          nextHits.push(q.hit);
        }
      }

      // 由近及远保存以便点击拾取最顶层贴纸
      hitFacesRef.current = nextHits.reverse();
      rafId = window.requestAnimationFrame(render);
    };

    rafId = window.requestAnimationFrame(render);
    return () => window.cancelAnimationFrame(rafId);
  }, [size, palette, material]);

  // 鼠标拾取与拖拽交互
  const pickSticker = (clientX: number, clientY: number): HitFace | undefined => {
    const canvas = canvasRef.current;
    if (!canvas) return undefined;
    const rect = canvas.getBoundingClientRect();
    const x = clientX - rect.left;
    const y = clientY - rect.top;
    // 在距离贴纸中心合理半径内且最靠前的面片
    const maxRadius = Math.max(18, Math.min(rect.width, rect.height) / (size * 2.5));
    for (const h of hitFacesRef.current) {
      if (Math.hypot(x - h.screenCenter.x, y - h.screenCenter.y) <= maxRadius) {
        return h;
      }
    }
    return undefined;
  };

  const triggerDragTurn = (hit: HitFace, dx: number, dy: number) => {
    if (activeAnimRef.current || queueRef.current.length > 0) return;
    const { yaw, pitch } = camRef.current;
    const eye: Vec3 = {
      x: Math.cos(pitch) * Math.sin(yaw),
      y: Math.sin(pitch),
      z: Math.cos(pitch) * Math.cos(yaw),
    };
    const forward = vNorm(vScale(eye, -1));
    const right = vNorm(vCross(forward, { x: 0, y: 1, z: 0 }));
    const up = vCross(right, forward);

    // 将鼠标屏幕位移映射到 3D 视平面切向量
    const dragWorld = vNorm(vAdd(vScale(right, dx), vScale(up, -dy)));
    // 旋转轴 = 贴纸法线 × 拖拽方向
    const rotAxisWorld = vCross(hit.worldNormal, dragWorld);
    const ax = Math.abs(rotAxisWorld.x), ay = Math.abs(rotAxisWorld.y), az = Math.abs(rotAxisWorld.z);
    let axis: Axis = 'x';
    let sign = 1;
    if (ax >= ay && ax >= az) {
      axis = 'x';
      sign = rotAxisWorld.x >= 0 ? 1 : -1;
    } else if (ay >= ax && ay >= az) {
      axis = 'y';
      sign = rotAxisWorld.y >= 0 ? 1 : -1;
    } else {
      axis = 'z';
      sign = rotAxisWorld.z >= 0 ? 1 : -1;
    }
    const dir: 1 | -1 = sign >= 0 ? 1 : -1;
    const layer = hit.cubelet.coords[coordIdx(axis)];
    enqueueMoves([{ axis, layers: [layer], dir, label: formatMoveLabel(axis, layer, size, dir) }]);
  };

  const triggerClickFaceTurn = (hit: HitFace, reverse: boolean) => {
    if (activeAnimRef.current || queueRef.current.length > 0) return;
    const n = hit.worldNormal;
    const ax = Math.abs(n.x), ay = Math.abs(n.y), az = Math.abs(n.z);
    let axis: Axis = 'z';
    let posSide = true;
    if (ax >= ay && ax >= az) { axis = 'x'; posSide = n.x > 0; }
    else if (ay >= ax && ay >= az) { axis = 'y'; posSide = n.y > 0; }
    else { axis = 'z'; posSide = n.z > 0; }

    const layer = posSide ? size - 1 : 0;
    const baseDir: 1 | -1 = posSide ? clickDir : (clickDir === 1 ? -1 : 1);
    const dir: 1 | -1 = reverse ? (baseDir === 1 ? -1 : 1) : baseDir;
    enqueueMoves([{ axis, layers: [layer], dir, label: formatMoveLabel(axis, layer, size, dir) }]);
  };

  return (
    <div className="grid gap-4 lg:grid-cols-[1fr_360px]">
      {/* 左侧 3D 视口 */}
      <div className="relative flex min-h-[480px] flex-col overflow-hidden rounded-xl border border-line bg-[#0b0f17] select-none">
        <div className="z-10 flex flex-wrap items-center justify-between gap-2 border-b border-white/10 bg-black/40 px-4 py-2.5 backdrop-blur">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-white">阶数</span>
            {[2, 3, 4, 5, 6].map(n => (
              <button
                key={n}
                onClick={() => changeSize(n)}
                className={`rounded-md px-2.5 py-1 text-xs font-medium transition ${
                  size === n ? 'bg-primary text-white' : 'border border-white/15 text-slate-300 hover:border-primary'
                }`}
              >
                {n}×{n}
              </button>
            ))}
          </div>

          <div className="flex flex-wrap items-center gap-3 text-xs text-slate-300">
            <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1">
              状态：<strong className={solved ? 'text-emerald-400' : 'text-amber-300'}>{solved ? '已复原' : '已打乱'}</strong>
            </span>
            <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1">
              步数：<strong className="text-white">{moveCount}</strong>
            </span>
            <span className="rounded-full border border-white/10 bg-white/5 px-2.5 py-1 font-mono">
              {(elapsedMs / 1000).toFixed(2)}s
            </span>
            <button
              className="rounded-md border border-white/15 px-2 py-1 hover:border-primary"
              onClick={() => setTimerRunning(r => !r)}
            >
              {timerRunning ? '暂停计时' : '启动计时'}
            </button>
          </div>
        </div>

        <canvas
          ref={canvasRef}
          className="h-[460px] w-full flex-1 cursor-grab active:cursor-grabbing"
          onPointerDown={e => {
            e.currentTarget.setPointerCapture(e.pointerId);
            const hit = pickSticker(e.clientX, e.clientY);
            dragStateRef.current = {
              type: hit ? 'sticker' : 'orbit',
              startX: e.clientX,
              startY: e.clientY,
              lastX: e.clientX,
              lastY: e.clientY,
              moved: false,
              hit,
            };
          }}
          onPointerMove={e => {
            const st = dragStateRef.current;
            if (!st) return;
            const totalDx = e.clientX - st.startX;
            const totalDy = e.clientY - st.startY;
            if (st.type === 'sticker' && st.hit) {
              if (!st.moved && Math.hypot(totalDx, totalDy) >= 14) {
                st.moved = true;
                triggerDragTurn(st.hit, totalDx, totalDy);
              }
            } else {
              const dx = e.clientX - st.lastX;
              const dy = e.clientY - st.lastY;
              if (Math.hypot(totalDx, totalDy) > 4) st.moved = true;
              camRef.current.yaw -= dx * 0.0085;
              camRef.current.pitch = Math.max(-1.35, Math.min(1.35, camRef.current.pitch + dy * 0.0085));
              st.lastX = e.clientX;
              st.lastY = e.clientY;
            }
          }}
          onPointerUp={e => {
            const st = dragStateRef.current;
            dragStateRef.current = null;
            if (st && st.type === 'sticker' && !st.moved && st.hit) {
              triggerClickFaceTurn(st.hit, e.shiftKey || e.button === 2);
            }
          }}
          onWheel={e => {
            e.preventDefault();
            camRef.current.distance = Math.max(size * 1.4, Math.min(size * 4.2, camRef.current.distance + e.deltaY * 0.005));
          }}
        />

        {/* 底部操作提示与解算进度浮层 */}
        <div className="z-10 flex flex-wrap items-center justify-between gap-2 border-t border-white/10 bg-black/45 px-4 py-2 text-xs text-slate-300 backdrop-blur">
          <span>操作：在贴纸上拖拽切向转层 · 单击贴纸转外层（Shift 反向） · 空白处拖拽旋转视角 · 滚轮缩放</span>
          {solverStatus ? (
            <span className="text-sky-300">
              {solverStatus.mode}：第 {solverStatus.current}/{solverStatus.total} 步（当前：{solverStatus.moveLabel}）
            </span>
          ) : null}
        </div>
      </div>

      {/* 右侧控制面板 */}
      <div className="space-y-4">
        {/* 快捷操作与智能求解 */}
        <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
          <h3 className="text-sm font-semibold">魔方控制与智能解算</h3>
          <div className="grid grid-cols-2 gap-2">
            <button
              className="rounded-md bg-primary px-3 py-2 text-sm font-medium text-white hover:opacity-90"
              onClick={scrambleCube}
            >
              随机打乱
            </button>
            <button
              className="rounded-md border border-primary px-3 py-2 text-sm font-medium text-primary hover:bg-primary/10 disabled:opacity-40"
              disabled={solved || solving || !!solverStatus}
              onClick={startSmartSolve}
            >
              {solving ? "求解中…" : "智能解算复原"}
            </button>
            <button
              className="rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary disabled:opacity-40"
              disabled={history.length === 0 || !!solverStatus}
              onClick={undoLastMove}
            >
              撤销一步 ({history.length})
            </button>
            <button
              className="rounded-md border border-line px-3 py-1.5 text-sm hover:border-primary"
              onClick={() => resetCube(size)}
            >
              立即重置
            </button>
          </div>

          <div className="flex items-center justify-between gap-2 pt-1 text-xs">
            <span className="text-muted">单击贴纸默认方向</span>
            <button
              className="rounded border border-line px-2.5 py-1 hover:border-primary"
              onClick={() => setClickDir(d => (d === -1 ? 1 : -1))}
            >
              {clickDir === -1 ? '顺时针 (CW)' : '逆时针 (CCW)'}
            </button>
          </div>

          <div className="space-y-1.5 pt-1 text-xs">
            <div className="flex items-center justify-between">
              <span className="text-muted">解算动画倍速</span>
              <span>{solveSpeed.toFixed(1)}x</span>
            </div>
            <input
              type="range"
              min="0.4"
              max="2.5"
              step="0.1"
              value={solveSpeed}
              onChange={e => setSolveSpeed(Number(e.target.value))}
              className="w-full"
            />
            {solverStatus ? (
              <div className="flex gap-2 pt-1">
                <button
                  className="flex-1 rounded border border-line px-2 py-1 hover:border-primary"
                  onClick={() => setSolverPaused(p => !p)}
                >
                  {solverPaused ? '继续解算' : '暂停解算'}
                </button>
                <button
                  className="flex-1 rounded border border-line px-2 py-1 hover:border-primary disabled:opacity-40"
                  disabled={!solverPaused}
                  onClick={stepSolverOnce}
                >
                  单步执行
                </button>
              </div>
            ) : null}
            {solveNote ? <p className="break-all pt-1 text-xs text-muted">{solveNote}</p> : null}
          </div>
        </section>

        {/* 公式控制台与预设 */}
        <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
          <h3 className="text-sm font-semibold">公式执行器（支持 R L U D F B M E S x y z）</h3>
          <form
            className="flex gap-2"
            onSubmit={e => {
              e.preventDefault();
              const parsed = parseFormulaString(customFormula, size);
              enqueueMoves(parsed);
            }}
          >
            <input
              value={customFormula}
              onChange={e => setCustomFormula(e.target.value)}
              placeholder="例如: R U R' U'"
              className="min-w-0 flex-1 rounded-md border border-line bg-background px-3 py-1.5 font-mono text-sm"
            />
            <button className="rounded-md border border-primary px-3 py-1.5 text-sm text-primary hover:bg-primary/10">
              执行
            </button>
          </form>
          <div className="space-y-1.5">
            {PRESET_FORMULAS.map(item => (
              <div key={item.name} className="flex items-center justify-between gap-2 rounded-lg border border-line/70 px-2.5 py-1.5 text-xs">
                <div className="min-w-0">
                  <div className="font-medium truncate">{item.name}</div>
                  <div className="font-mono text-muted truncate">{item.expr}</div>
                </div>
                <button
                  className="shrink-0 rounded border border-line px-2 py-1 hover:border-primary"
                  onClick={() => {
                    setCustomFormula(item.expr);
                    enqueueMoves(parseFormulaString(item.expr, size));
                  }}
                >
                  运行
                </button>
              </div>
            ))}
          </div>
        </section>

        {/* 外观材质与分层微调 */}
        <section className="rounded-xl border border-line bg-panel p-4 space-y-3">
          <h3 className="text-sm font-semibold">材质与配色方案</h3>
          <div className="grid grid-cols-3 gap-1.5 text-xs">
            {(Object.keys(PALETTES) as PaletteKey[]).map(k => (
              <button
                key={k}
                onClick={() => setPalette(k)}
                className={`rounded-md border px-2 py-1.5 ${palette === k ? 'border-primary text-primary' : 'border-line text-muted'}`}
              >
                {PALETTES[k].label}
              </button>
            ))}
          </div>
          <div className="grid grid-cols-3 gap-1.5 text-xs">
            {(Object.keys(MATERIALS) as MaterialKey[]).map(k => (
              <button
                key={k}
                onClick={() => setMaterial(k)}
                className={`rounded-md border px-2 py-1.5 ${material === k ? 'border-primary text-primary' : 'border-line text-muted'}`}
              >
                {MATERIALS[k].label}
              </button>
            ))}
          </div>
          <div className="pt-1 text-xs text-muted">
            近期步序：{history.length ? history.slice(-14).map(m => m.label).join(' ') : '暂无记录'}
          </div>
        </section>
      </div>
    </div>
  );
}
