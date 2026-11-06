// 魔方状态解算：只看当前贴纸状态，不依赖打乱/操作历史。
// 3×3：Kociemba 两阶段算法（G0 → G1 → 复原），坐标移动表 + 剪枝表，首次调用时构建（约数百毫秒）。
// 2×2：固定 DBL 角块，仅用 U/R/F，在 7!·3^6 = 3,674,160 个状态上建完整 BFS 距离表，得到最优解。
// 输出为标准公式字符串（含开头的整体转体 x/y/z 用于把中心/基准角摆正），由界面按公式动画执行。

type Axis = 'x' | 'y' | 'z';
interface Vec3 { x: number; y: number; z: number }
interface Quat { x: number; y: number; z: number; w: number }
export interface SolverCubelet {
  id: string;
  home: [number, number, number];
  coords: [number, number, number];
  pos: Vec3;
  rot: Quat;
}
interface Move { axis: Axis; layers: number[]; dir: 1 | -1 }

// ---------- 几何（与 RubiksLab 渲染模型一致：x→R，y→U，z→F） ----------
function qAxisAngle(axis: Vec3, rad: number): Quat {
  const s = Math.sin(rad / 2);
  return { x: axis.x * s, y: axis.y * s, z: axis.z * s, w: Math.cos(rad / 2) };
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
  const ux = q.x, uy = q.y, uz = q.z, s = q.w;
  const uv = ux * v.x + uy * v.y + uz * v.z;
  const uu = ux * ux + uy * uy + uz * uz;
  const cx = uy * v.z - uz * v.y, cy = uz * v.x - ux * v.z, cz = ux * v.y - uy * v.x;
  return {
    x: 2 * uv * ux + (s * s - uu) * v.x + 2 * s * cx,
    y: 2 * uv * uy + (s * s - uu) * v.y + 2 * s * cy,
    z: 2 * uv * uz + (s * s - uu) * v.z + 2 * s * cz,
  };
}
const axisVec = (a: Axis): Vec3 => (a === 'x' ? { x: 1, y: 0, z: 0 } : a === 'y' ? { x: 0, y: 1, z: 0 } : { x: 0, y: 0, z: 1 });
const coordIdx = (a: Axis) => (a === 'x' ? 0 : a === 'y' ? 1 : 2);
const offset = (i: number, n: number) => (i - (n - 1) / 2) * 1.02;

function rotateGrid(c: [number, number, number], n: number, axis: Axis, dir: 1 | -1): [number, number, number] {
  const h = (n - 1) / 2;
  const rx = c[0] - h, ry = c[1] - h, rz = c[2] - h;
  let nx = rx, ny = ry, nz = rz;
  if (axis === 'x') { ny = dir === 1 ? -rz : rz; nz = dir === 1 ? ry : -ry; }
  else if (axis === 'y') { nx = dir === 1 ? rz : -rz; nz = dir === 1 ? -rx : rx; }
  else { nx = dir === 1 ? -ry : ry; ny = dir === 1 ? rx : -rx; }
  return [Math.round(nx + h), Math.round(ny + h), Math.round(nz + h)];
}

export function applyMove(cubelets: SolverCubelet[], n: number, m: Move): SolverCubelet[] {
  const idx = coordIdx(m.axis);
  const q = qAxisAngle(axisVec(m.axis), m.dir * Math.PI / 2);
  return cubelets.map(cb => {
    if (!m.layers.includes(cb.coords[idx])) return cb;
    const c = rotateGrid(cb.coords, n, m.axis, m.dir);
    return { ...cb, coords: c, pos: { x: offset(c[0], n), y: offset(c[1], n), z: offset(c[2], n) }, rot: qNorm(qMul(q, cb.rot)) };
  });
}

/** 与 RubiksLab.parseFormulaString 相同的记号约定。 */
export function parseFormula(formula: string, n: number): Move[] {
  const out: Move[] = [];
  const mid = Math.floor(n / 2);
  const all = Array.from({ length: n }, (_, i) => i);
  for (const raw of formula.trim().split(/\s+/).filter(Boolean)) {
    const dbl = raw.includes('2');
    const prime = raw.includes("'");
    const base = raw.replace(/[2']/g, '');
    let m: Move | null = null;
    if (base === 'R') m = { axis: 'x', layers: [n - 1], dir: prime ? 1 : -1 };
    else if (base === 'L') m = { axis: 'x', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'U') m = { axis: 'y', layers: [n - 1], dir: prime ? 1 : -1 };
    else if (base === 'D') m = { axis: 'y', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'F') m = { axis: 'z', layers: [n - 1], dir: prime ? 1 : -1 };
    else if (base === 'B') m = { axis: 'z', layers: [0], dir: prime ? -1 : 1 };
    else if (base === 'M' && n >= 3) m = { axis: 'x', layers: [mid], dir: prime ? -1 : 1 };
    else if (base === 'E' && n >= 3) m = { axis: 'y', layers: [mid], dir: prime ? -1 : 1 };
    else if (base === 'S' && n >= 3) m = { axis: 'z', layers: [mid], dir: prime ? 1 : -1 };
    else if (base === 'x') m = { axis: 'x', layers: all, dir: prime ? 1 : -1 };
    else if (base === 'y') m = { axis: 'y', layers: all, dir: prime ? 1 : -1 };
    else if (base === 'z') m = { axis: 'z', layers: all, dir: prime ? 1 : -1 };
    if (!m) continue;
    out.push(m);
    if (dbl) out.push(m);
  }
  return out;
}

export function createCubelets(n: number): SolverCubelet[] {
  const list: SolverCubelet[] = [];
  for (let x = 0; x < n; x++) for (let y = 0; y < n; y++) for (let z = 0; z < n; z++) {
    if (x > 0 && x < n - 1 && y > 0 && y < n - 1 && z > 0 && z < n - 1) continue;
    list.push({ id: `${x}-${y}-${z}`, home: [x, y, z], coords: [x, y, z], pos: { x: offset(x, n), y: offset(y, n), z: offset(z, n) }, rot: { x: 0, y: 0, z: 0, w: 1 } });
  }
  return list;
}

/** 按贴纸判断：每个外表面的贴纸同色即为复原（中心块自转、整体朝向不影响）。 */
export function isSolvedState(cubelets: SolverCubelet[], n: number): boolean {
  const at = new Map(cubelets.map(c => [c.coords.join(','), c]));
  for (const face of Object.keys(FACE_DIR) as Face[]) {
    const d = FACE_DIR[face];
    const axis = d.x ? 0 : d.y ? 1 : 2;
    const layer = (d.x || d.y || d.z) > 0 ? n - 1 : 0;
    let color: Face | undefined;
    for (const c of cubelets) {
      if (c.coords[axis] !== layer) continue;
      const col = stickerColor(at, c.coords, face);
      if (color === undefined) color = col;
      else if (col !== color) return false;
    }
  }
  return true;
}

// ---------- 贴纸颜色读取 ----------
type Face = 'U' | 'R' | 'F' | 'D' | 'L' | 'B';
const FACE_DIR: Record<Face, Vec3> = {
  U: { x: 0, y: 1, z: 0 }, D: { x: 0, y: -1, z: 0 }, R: { x: 1, y: 0, z: 0 },
  L: { x: -1, y: 0, z: 0 }, F: { x: 0, y: 0, z: 1 }, B: { x: 0, y: 0, z: -1 },
};
function stickerColor(at: Map<string, SolverCubelet>, pos: [number, number, number], face: Face): Face {
  const cb = at.get(pos.join(','));
  if (!cb) throw new Error('missing cubelet');
  const inv = { x: -cb.rot.x, y: -cb.rot.y, z: -cb.rot.z, w: cb.rot.w };
  const l = qRotVec(inv, FACE_DIR[face]);
  if (l.x > 0.5) return 'R';
  if (l.x < -0.5) return 'L';
  if (l.y > 0.5) return 'U';
  if (l.y < -0.5) return 'D';
  return l.z > 0.5 ? 'F' : 'B';
}

// 角块/棱块位置与贴纸顺序（与 Kociemba 的 cornerFacelet / edgeFacelet 顺序一致）。
const cornerDefs = (n: number): { pos: [number, number, number]; faces: [Face, Face, Face] }[] => {
  const k = n - 1;
  return [
    { pos: [k, k, k], faces: ['U', 'R', 'F'] }, { pos: [0, k, k], faces: ['U', 'F', 'L'] },
    { pos: [0, k, 0], faces: ['U', 'L', 'B'] }, { pos: [k, k, 0], faces: ['U', 'B', 'R'] },
    { pos: [k, 0, k], faces: ['D', 'F', 'R'] }, { pos: [0, 0, k], faces: ['D', 'L', 'F'] },
    { pos: [0, 0, 0], faces: ['D', 'B', 'L'] }, { pos: [k, 0, 0], faces: ['D', 'R', 'B'] },
  ];
};
const EDGE_DEFS: { pos: [number, number, number]; faces: [Face, Face] }[] = [
  { pos: [2, 2, 1], faces: ['U', 'R'] }, { pos: [1, 2, 2], faces: ['U', 'F'] }, { pos: [0, 2, 1], faces: ['U', 'L'] }, { pos: [1, 2, 0], faces: ['U', 'B'] },
  { pos: [2, 0, 1], faces: ['D', 'R'] }, { pos: [1, 0, 2], faces: ['D', 'F'] }, { pos: [0, 0, 1], faces: ['D', 'L'] }, { pos: [1, 0, 0], faces: ['D', 'B'] },
  { pos: [2, 1, 2], faces: ['F', 'R'] }, { pos: [0, 1, 2], faces: ['F', 'L'] }, { pos: [0, 1, 0], faces: ['B', 'L'] }, { pos: [2, 1, 0], faces: ['B', 'R'] },
];

// ---------- Cubie 表示 ----------
interface Cubie { cp: number[]; co: number[]; ep: number[]; eo: number[] }
const SOLVED: Cubie = { cp: [0, 1, 2, 3, 4, 5, 6, 7], co: [0, 0, 0, 0, 0, 0, 0, 0], ep: [0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11], eo: Array(12).fill(0) };
function mul(a: Cubie, b: Cubie): Cubie {
  const cp = [], co = [], ep = [], eo = [];
  for (let i = 0; i < 8; i++) { cp.push(a.cp[b.cp[i]]); co.push((a.co[b.cp[i]] + b.co[i]) % 3); }
  for (let i = 0; i < 12; i++) { ep.push(a.ep[b.ep[i]]); eo.push((a.eo[b.ep[i]] + b.eo[i]) % 2); }
  return { cp, co, ep, eo };
}
const z12 = Array(12).fill(0), z8 = Array(8).fill(0);
const BASIC: Cubie[] = [
  { cp: [3, 0, 1, 2, 4, 5, 6, 7], co: z8, ep: [3, 0, 1, 2, 4, 5, 6, 7, 8, 9, 10, 11], eo: z12 }, // U
  { cp: [4, 1, 2, 0, 7, 5, 6, 3], co: [2, 0, 0, 1, 1, 0, 0, 2], ep: [8, 1, 2, 3, 11, 5, 6, 7, 4, 9, 10, 0], eo: z12 }, // R
  { cp: [1, 5, 2, 3, 0, 4, 6, 7], co: [1, 2, 0, 0, 2, 1, 0, 0], ep: [0, 9, 2, 3, 4, 8, 6, 7, 1, 5, 10, 11], eo: [0, 1, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0] }, // F
  { cp: [0, 1, 2, 3, 5, 6, 7, 4], co: z8, ep: [0, 1, 2, 3, 5, 6, 7, 4, 8, 9, 10, 11], eo: z12 }, // D
  { cp: [0, 2, 6, 3, 4, 1, 5, 7], co: [0, 1, 2, 0, 0, 2, 1, 0], ep: [0, 1, 10, 3, 4, 5, 9, 7, 8, 2, 6, 11], eo: z12 }, // L
  { cp: [0, 1, 3, 7, 4, 5, 2, 6], co: [0, 0, 1, 2, 0, 0, 2, 1], ep: [0, 1, 2, 11, 4, 5, 6, 10, 8, 9, 3, 7], eo: [0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1] }, // B
];
export const MOVES: Cubie[] = [];
for (const b of BASIC) { const m2 = mul(b, b); MOVES.push(b, m2, mul(m2, b)); }
const FACE_NAMES = 'URFDLB';
const moveName = (m: number) => FACE_NAMES[Math.floor(m / 3)] + ['', '2', "'"][m % 3];

export function readCubie3(cubelets: SolverCubelet[]): Cubie {
  const at = new Map(cubelets.map(c => [c.coords.join(','), c]));
  const cp: number[] = [], co: number[] = [], ep: number[] = [], eo: number[] = [];
  const corners = cornerDefs(3);
  for (const d of corners) {
    const cols = d.faces.map(f => stickerColor(at, d.pos, f));
    const ori = cols.findIndex(c => c === 'U' || c === 'D');
    const c1 = cols[(ori + 1) % 3], c2 = cols[(ori + 2) % 3];
    const j = corners.findIndex(k => k.faces[1] === c1 && k.faces[2] === c2);
    if (ori < 0 || j < 0) throw new Error('invalid corner');
    cp.push(j); co.push(ori);
  }
  for (const d of EDGE_DEFS) {
    const a = stickerColor(at, d.pos, d.faces[0]), b = stickerColor(at, d.pos, d.faces[1]);
    const j = EDGE_DEFS.findIndex(k => (k.faces[0] === a && k.faces[1] === b) || (k.faces[0] === b && k.faces[1] === a));
    if (j < 0) throw new Error('invalid edge');
    ep.push(j); eo.push(EDGE_DEFS[j].faces[0] === a ? 0 : 1);
  }
  return { cp, co, ep, eo };
}

// ---------- 坐标 ----------
function lehmer(a: number[]): number {
  let r = 0;
  for (let i = 0; i < a.length; i++) {
    let c = 0;
    for (let j = i + 1; j < a.length; j++) if (a[j] < a[i]) c++;
    r = r * (a.length - i) + c;
  }
  return r;
}
const SLICE_INDEX = new Map<number, number>();
for (let m = 0, k = 0; m < 4096; m++) {
  let b = 0;
  for (let t = m; t; t &= t - 1) b++;
  if (b === 4) SLICE_INDEX.set(m, k++);
}
const twistOf = (c: Cubie) => { let t = 0; for (let i = 0; i < 7; i++) t = t * 3 + c.co[i]; return t; };
const flipOf = (c: Cubie) => { let f = 0; for (let i = 0; i < 11; i++) f = f * 2 + c.eo[i]; return f; };
const sliceOf = (c: Cubie) => { let m = 0; for (let i = 0; i < 12; i++) if (c.ep[i] >= 8) m |= 1 << i; return SLICE_INDEX.get(m)!; };
const cpOf = (c: Cubie) => lehmer(c.cp);
const ep8Of = (c: Cubie) => lehmer(c.ep.slice(0, 8));
const spOf = (c: Cubie) => lehmer(c.ep.slice(8).map(v => v - 8));

/** 以代表元 BFS 构建坐标移动表：table[coord*18+move]。 */
function moveTable(size: number, coord: (c: Cubie) => number, moves: number[]): Int32Array {
  const table = new Int32Array(size * 18).fill(-1);
  const rep: (Cubie | undefined)[] = new Array(size);
  const start = coord(SOLVED);
  rep[start] = SOLVED;
  const queue = [start];
  for (let h = 0; h < queue.length; h++) {
    const c = queue[h], cube = rep[c]!;
    for (const m of moves) {
      const next = mul(cube, MOVES[m]);
      const nc = coord(next);
      table[c * 18 + m] = nc;
      if (!rep[nc]) { rep[nc] = next; queue.push(nc); }
    }
  }
  if (queue.length !== size) throw new Error(`move table incomplete ${queue.length}/${size}`);
  return table;
}

function pruneTable(n1: number, n2: number, t1: Int32Array, t2: Int32Array, s1: number, s2: number, moves: number[]): Int8Array {
  const dist = new Int8Array(n1 * n2).fill(-1);
  const queue = new Int32Array(n1 * n2);
  let head = 0, tail = 0;
  dist[s1 * n2 + s2] = 0; queue[tail++] = s1 * n2 + s2;
  while (head < tail) {
    const idx = queue[head++];
    const a = (idx / n2) | 0, b = idx - a * n2, d = dist[idx];
    for (const m of moves) {
      const ni = t1[a * 18 + m] * n2 + t2[b * 18 + m];
      if (dist[ni] < 0) { dist[ni] = d + 1; queue[tail++] = ni; }
    }
  }
  return dist;
}

const ALL18 = Array.from({ length: 18 }, (_, i) => i);
const P2MOVES = [0, 1, 2, 9, 10, 11, 4, 13, 7, 16]; // U U2 U' D D2 D' R2 L2 F2 B2

interface Tables3 {
  twist: Int32Array; flip: Int32Array; slice: Int32Array; cp: Int32Array; ep8: Int32Array; sp: Int32Array;
  pTwist: Int8Array; pFlip: Int8Array; pCp: Int8Array; pEp: Int8Array; slice0: number;
}
let tables3: Tables3 | null = null;
function getTables3(): Tables3 {
  if (tables3) return tables3;
  const twist = moveTable(2187, twistOf, ALL18);
  const flip = moveTable(2048, flipOf, ALL18);
  const slice = moveTable(495, sliceOf, ALL18);
  const cp = moveTable(40320, cpOf, P2MOVES);
  const ep8 = moveTable(40320, ep8Of, P2MOVES);
  const sp = moveTable(24, spOf, P2MOVES);
  const slice0 = sliceOf(SOLVED);
  tables3 = {
    twist, flip, slice, cp, ep8, sp, slice0,
    pTwist: pruneTable(495, 2187, slice, twist, slice0, 0, ALL18),
    pFlip: pruneTable(495, 2048, slice, flip, slice0, 0, ALL18),
    pCp: pruneTable(40320, 24, cp, sp, 0, 0, P2MOVES),
    pEp: pruneTable(40320, 24, ep8, sp, 0, 0, P2MOVES),
  };
  return tables3;
}

/** 同面连续转、对面按固定顺序（先 U/R/F 后 D/L/B 才允许），避免等价分支。 */
const allowed = (last: number, m: number) => {
  if (last < 0) return true;
  const f = Math.floor(m / 3), lf = Math.floor(last / 3);
  return f !== lf && lf - f !== 3;
};

function kociemba(c: Cubie, maxTotal = 30): number[] | null {
  const T = getTables3();
  const path: number[] = [];
  const path2: number[] = [];

  const search2 = (cp: number, ep: number, sp: number, depth: number, last: number): boolean => {
    if (depth === 0) return cp === 0 && ep === 0 && sp === 0;
    if (Math.max(T.pCp[cp * 24 + sp], T.pEp[ep * 24 + sp]) > depth) return false;
    for (const m of P2MOVES) {
      if (!allowed(last, m)) continue;
      path2.push(m);
      if (search2(T.cp[cp * 18 + m], T.ep8[ep * 18 + m], T.sp[sp * 18 + m], depth - 1, m)) return true;
      path2.pop();
    }
    return false;
  };

  const phase2 = (): boolean => {
    let cube = c;
    for (const m of path) cube = mul(cube, MOVES[m]);
    const cp = cpOf(cube), ep = ep8Of(cube), sp = spOf(cube);
    // 首步不限制与阶段一末步同面，结果由 mergeMoves 合并（否则如 R 的解会被迫绕远）。
    for (let d = 0; d <= maxTotal - path.length; d++) {
      path2.length = 0;
      if (search2(cp, ep, sp, d, -1)) return true;
    }
    return false;
  };

  const search1 = (tw: number, fl: number, sl: number, depth: number, last: number): boolean => {
    if (depth === 0) return tw === 0 && fl === 0 && sl === T.slice0 && phase2();
    if (Math.max(T.pTwist[sl * 2187 + tw], T.pFlip[sl * 2048 + fl]) > depth) return false;
    for (const m of ALL18) {
      if (!allowed(last, m)) continue;
      path.push(m);
      if (search1(T.twist[tw * 18 + m], T.flip[fl * 18 + m], T.slice[sl * 18 + m], depth - 1, m)) return true;
      path.pop();
    }
    return false;
  };

  const tw = twistOf(c), fl = flipOf(c), sl = sliceOf(c);
  for (let d = 0; d <= 14; d++) {
    path.length = 0;
    if (search1(tw, fl, sl, d, -1)) return mergeMoves([...path, ...path2]);
  }
  return null;
}

/** 合并相邻同面转动（次数按 4 取模，抵消的直接删去）。 */
function mergeMoves(moves: number[]): number[] {
  const out: number[] = [];
  for (const m of moves) {
    const last = out[out.length - 1];
    if (last !== undefined && Math.floor(last / 3) === Math.floor(m / 3)) {
      out.pop();
      const turns = ((last % 3) + 1 + (m % 3) + 1) % 4;
      if (turns) out.push(Math.floor(m / 3) * 3 + turns - 1);
    } else out.push(m);
  }
  return out;
}

// ---------- 2×2 ----------
const P2X2 = [0, 1, 2, 3, 4, 5, 6, 7, 8]; // U R F 各三种
const KEEP7 = [0, 1, 2, 3, 4, 5, 7];
const perm7Of = (c: Cubie) => lehmer(KEEP7.map(i => (c.cp[i] === 7 ? 6 : c.cp[i])));
const twist6Of = (c: Cubie) => { let t = 0; for (let i = 0; i < 6; i++) t = t * 3 + c.co[i]; return t; };
interface Tables2 { perm: Int32Array; twist: Int32Array; dist: Int8Array }
let tables2: Tables2 | null = null;
function getTables2(): Tables2 {
  if (tables2) return tables2;
  const perm = moveTable(5040, perm7Of, P2X2);
  const twist = moveTable(729, twist6Of, P2X2);
  tables2 = { perm, twist, dist: pruneTable(5040, 729, perm, twist, 0, 0, P2X2) };
  return tables2;
}
function readCorners2(cubelets: SolverCubelet[]): Cubie {
  const at = new Map(cubelets.map(c => [c.coords.join(','), c]));
  const corners = cornerDefs(2);
  const cp: number[] = [], co: number[] = [];
  for (const d of corners) {
    const cols = d.faces.map(f => stickerColor(at, d.pos, f));
    const ori = cols.findIndex(c => c === 'U' || c === 'D');
    const c1 = cols[(ori + 1) % 3], c2 = cols[(ori + 2) % 3];
    const j = corners.findIndex(k => k.faces[1] === c1 && k.faces[2] === c2);
    if (ori < 0 || j < 0) throw new Error('invalid corner');
    cp.push(j); co.push(ori);
  }
  return { cp, co, ep: SOLVED.ep, eo: SOLVED.eo };
}
function solve2(c: Cubie): number[] | null {
  const T = getTables2();
  let p = perm7Of(c), t = twist6Of(c);
  const out: number[] = [];
  for (let guard = 0; guard < 20; guard++) {
    const d = T.dist[p * 729 + t];
    if (d === 0) return out;
    if (d < 0) return null;
    const m = P2X2.find(m => T.dist[T.perm[p * 18 + m] * 729 + T.twist[t * 18 + m]] === d - 1);
    if (m === undefined) return null;
    out.push(m); p = T.perm[p * 18 + m]; t = T.twist[t * 18 + m];
  }
  return null;
}

// ---------- 整体朝向摆正 ----------
const ROTS = ['x', "x'", 'x2', 'y', "y'", 'y2', 'z', "z'", 'z2'];
const ROT_CANDIDATES = ['', ...ROTS, ...ROTS.flatMap(a => ROTS.map(b => `${a} ${b}`))];
function orient(cubelets: SolverCubelet[], n: number, ok: (s: SolverCubelet[]) => boolean): { rot: string; state: SolverCubelet[] } | null {
  for (const r of ROT_CANDIDATES) {
    let s = cubelets;
    for (const m of parseFormula(r, n)) s = applyMove(s, n, m);
    if (ok(s)) return { rot: r, state: s };
  }
  return null;
}

export interface SolveResult { formula: string; rotation: string; moves: string[]; method: string }

/** 支持 2×2、3×3；其他阶数返回 null。 */
export function solveByState(cubelets: SolverCubelet[], n: number): SolveResult | null {
  if (n === 3) {
    const centers: [number, number, number][] = [[1, 0, 1], [1, 2, 1], [0, 1, 1], [2, 1, 1], [1, 1, 0], [1, 1, 2]];
    const o = orient(cubelets, 3, s => centers.every(h => {
      const c = s.find(p => p.home[0] === h[0] && p.home[1] === h[1] && p.home[2] === h[2])!;
      return c.coords[0] === h[0] && c.coords[1] === h[1] && c.coords[2] === h[2];
    }));
    if (!o) return null;
    const sol = kociemba(readCubie3(o.state));
    if (!sol) return null;
    const moves = sol.map(moveName);
    return { formula: [o.rot, ...moves].join(' ').trim(), rotation: o.rot, moves, method: 'Kociemba 两阶段' };
  }
  if (n === 2) {
    const o = orient(cubelets, 2, s => {
      const c = s.find(p => p.home[0] === 0 && p.home[1] === 0 && p.home[2] === 0)!;
      return c.coords[0] === 0 && c.coords[1] === 0 && c.coords[2] === 0 && Math.abs(c.rot.w) > 0.999;
    });
    if (!o) return null;
    const sol = solve2(readCorners2(o.state));
    if (!sol) return null;
    const moves = sol.map(moveName);
    return { formula: [o.rot, ...moves].join(' ').trim(), rotation: o.rot, moves, method: '二阶 BFS 最优解' };
  }
  return null;
}
