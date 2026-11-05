import { useEffect, useMemo, useRef, useState } from 'react';
import { button, chip, notify, primaryButton, select, store, stored } from './ui';

const card = 'space-y-3 rounded-lg border border-line bg-panel p-3';
const subTabs = [
  ['qrcode', '二维码'],
  ['json', 'JSON'],
  ['codec', '编解码与哈希'],
  ['timestamp', '时间戳'],
  ['regex', '正则表达式'],
] as const;
type SubTab = typeof subTabs[number][0];

function copyText(text: string) {
  if (!text) return;
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(text).then(() => notify('已复制'), () => notify('复制失败'));
    return;
  }
  const area = document.createElement('textarea');
  area.value = text;
  document.body.appendChild(area);
  area.select();
  try { document.execCommand('copy'); notify('已复制'); } catch { notify('复制失败，请手动选择'); }
  area.remove();
}

export default function DevTools() {
  const [sub, setSub] = useState<SubTab>(() => {
    const saved = stored('omnihub.tools.dev.sub', 'qrcode');
    return subTabs.some(([id]) => id === saved) ? (saved as SubTab) : 'qrcode';
  });
  useEffect(() => store('omnihub.tools.dev.sub', sub), [sub]);

  return <div className="space-y-3">
    <div className="flex flex-wrap gap-1.5">
      {subTabs.map(([id, label]) => <button key={id} className={chip(sub === id)} onClick={() => setSub(id)}>{label}</button>)}
    </div>
    {sub === 'qrcode' ? <QrCodeTool /> : null}
    {sub === 'json' ? <JsonTool /> : null}
    {sub === 'codec' ? <CodecHashTool /> : null}
    {sub === 'timestamp' ? <TimestampTool /> : null}
    {sub === 'regex' ? <RegexTool /> : null}
  </div>;
}

// ---------------- QR Code Generator (ISO/IEC 18004 Byte Mode, ECC Level L, Version 1..10) ----------------
const QR_VERSIONS: { total: number; ecPerBlock: number; blocks1: number; data1: number; blocks2: number; data2: number; align: number[] }[] = [
  { total: 0, ecPerBlock: 0, blocks1: 0, data1: 0, blocks2: 0, data2: 0, align: [] },
  { total: 19, ecPerBlock: 7, blocks1: 1, data1: 19, blocks2: 0, data2: 0, align: [] },
  { total: 34, ecPerBlock: 10, blocks1: 1, data1: 34, blocks2: 0, data2: 0, align: [6, 18] },
  { total: 55, ecPerBlock: 15, blocks1: 1, data1: 55, blocks2: 0, data2: 0, align: [6, 22] },
  { total: 80, ecPerBlock: 20, blocks1: 1, data1: 80, blocks2: 0, data2: 0, align: [6, 26] },
  { total: 108, ecPerBlock: 26, blocks1: 1, data1: 108, blocks2: 0, data2: 0, align: [6, 30] },
  { total: 136, ecPerBlock: 18, blocks1: 2, data1: 68, blocks2: 0, data2: 0, align: [6, 34] },
  { total: 156, ecPerBlock: 20, blocks1: 2, data1: 78, blocks2: 0, data2: 0, align: [6, 22, 38] },
  { total: 194, ecPerBlock: 24, blocks1: 2, data1: 97, blocks2: 0, data2: 0, align: [6, 24, 42] },
  { total: 232, ecPerBlock: 30, blocks1: 2, data1: 116, blocks2: 0, data2: 0, align: [6, 26, 46] },
  { total: 274, ecPerBlock: 18, blocks1: 2, data1: 68, blocks2: 2, data2: 69, align: [6, 28, 50] },
];

function gfMul(x: number, y: number): number {
  let r = 0;
  for (let i = 7; i >= 0; i--) {
    r = (r << 1) ^ ((r >> 7) * 0x11d) ^ (((y >> i) & 1) * x);
  }
  return r;
}

function rsEncode(data: Uint8Array, ecLen: number): Uint8Array {
  const gen = new Uint8Array(ecLen);
  gen[ecLen - 1] = 1;
  let root = 1;
  for (let i = 0; i < ecLen; i++) {
    for (let j = 0; j < ecLen; j++) {
      gen[j] = gfMul(gen[j], root) ^ (j + 1 < ecLen ? gen[j + 1] : 0);
    }
    root = gfMul(root, 2);
  }
  const rem = new Uint8Array(ecLen);
  for (const b of data) {
    const factor = b ^ rem[0];
    rem.copyWithin(0, 1);
    rem[ecLen - 1] = 0;
    for (let j = 0; j < ecLen; j++) rem[j] ^= gfMul(gen[j], factor);
  }
  return rem;
}

function encodeQrMatrix(text: string): boolean[][] | null {
  const utf8 = new TextEncoder().encode(text);
  let ver = 1;
  while (ver <= 10) {
    const cap = QR_VERSIONS[ver].total - (ver <= 9 ? 2 : 3);
    if (utf8.length <= cap) break;
    ver++;
  }
  if (ver > 10) return null;
  const spec = QR_VERSIONS[ver];
  const bits: number[] = [];
  const pushBits = (val: number, len: number) => {
    for (let i = len - 1; i >= 0; i--) bits.push((val >>> i) & 1);
  };
  pushBits(0b0100, 4);
  pushBits(utf8.length, ver <= 9 ? 8 : 16);
  for (const b of utf8) pushBits(b, 8);
  const maxBits = spec.total * 8;
  pushBits(0, Math.min(4, maxBits - bits.length));
  while (bits.length % 8 !== 0) bits.push(0);
  const dataBytes = new Uint8Array(spec.total);
  for (let i = 0; i < bits.length / 8; i++) {
    let v = 0;
    for (let b = 0; b < 8; b++) v = (v << 1) | bits[i * 8 + b];
    dataBytes[i] = v;
  }
  for (let i = bits.length / 8, p = 0; i < spec.total; i++, p ^= 1) {
    dataBytes[i] = p === 0 ? 0xec : 0x11;
  }

  const blocks: Uint8Array[] = [];
  const ecBlocks: Uint8Array[] = [];
  let offset = 0;
  for (let i = 0; i < spec.blocks1; i++) {
    const blk = dataBytes.subarray(offset, offset + spec.data1);
    blocks.push(blk);
    ecBlocks.push(rsEncode(blk, spec.ecPerBlock));
    offset += spec.data1;
  }
  for (let i = 0; i < spec.blocks2; i++) {
    const blk = dataBytes.subarray(offset, offset + spec.data2);
    blocks.push(blk);
    ecBlocks.push(rsEncode(blk, spec.ecPerBlock));
    offset += spec.data2;
  }
  const codewords: number[] = [];
  const maxDataLen = Math.max(spec.data1, spec.data2);
  for (let i = 0; i < maxDataLen; i++) {
    for (const blk of blocks) if (i < blk.length) codewords.push(blk[i]);
  }
  for (let i = 0; i < spec.ecPerBlock; i++) {
    for (const ec of ecBlocks) codewords.push(ec[i]);
  }

  const size = ver * 4 + 17;
  const modules: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));
  const isFunc: boolean[][] = Array.from({ length: size }, () => Array(size).fill(false));

  const setFunc = (x: number, y: number, dark: boolean) => {
    if (x >= 0 && x < size && y >= 0 && y < size) {
      modules[y][x] = dark;
      isFunc[y][x] = true;
    }
  };
  const drawFinder = (cx: number, cy: number) => {
    for (let dy = -4; dy <= 4; dy++) {
      for (let dx = -4; dx <= 4; dx++) {
        const dist = Math.max(Math.abs(dx), Math.abs(dy));
        setFunc(cx + dx, cy + dy, dist !== 2 && dist !== 4);
      }
    }
  };
  drawFinder(3, 3);
  drawFinder(size - 4, 3);
  drawFinder(3, size - 4);
  for (let i = 0; i < size; i++) {
    if (!isFunc[6][i]) setFunc(i, 6, i % 2 === 0);
    if (!isFunc[i][6]) setFunc(6, i, i % 2 === 0);
  }
  for (const ay of spec.align) {
    for (const ax of spec.align) {
      if ((ax === 6 && ay === 6) || (ax === 6 && ay === size - 7) || (ax === size - 7 && ay === 6)) continue;
      for (let dy = -2; dy <= 2; dy++) {
        for (let dx = -2; dx <= 2; dx++) {
          setFunc(ax + dx, ay + dy, Math.max(Math.abs(dx), Math.abs(dy)) !== 1);
        }
      }
    }
  }
  for (let i = 0; i <= 8; i++) {
    if (i !== 6) {
      setFunc(8, i, false);
      setFunc(i, 8, false);
    }
    if (i < 8) {
      setFunc(size - 1 - i, 8, false);
      setFunc(8, size - 1 - i, false);
    }
  }
  setFunc(8, size - 8, true);

  if (ver >= 7) {
    let rem = ver;
    for (let i = 0; i < 12; i++) rem = (rem << 1) ^ ((rem >> 11) * 0x1f25);
    const vBits = (ver << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const bit = ((vBits >>> i) & 1) !== 0;
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      setFunc(a, b, bit);
      setFunc(b, a, bit);
    }
  }

  let bitIdx = 0;
  const totalBits = codewords.length * 8;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const x = right - j;
        const upward = ((right + 1) & 2) === 0;
        const y = upward ? size - 1 - vert : vert;
        if (!isFunc[y][x] && bitIdx < totalBits) {
          const dark = ((codewords[bitIdx >>> 3] >>> (7 - (bitIdx & 7))) & 1) !== 0;
          modules[y][x] = dark;
          bitIdx++;
        }
      }
    }
  }

  // Mask 0: (x + y) % 2 === 0, ECC Level L (01) -> format bits for (01, 000) = 0x77c4
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      if (!isFunc[y][x] && (x + y) % 2 === 0) modules[y][x] = !modules[y][x];
    }
  }
  const fmtBits = 0x77c4;
  for (let i = 0; i <= 5; i++) setFunc(8, i, ((fmtBits >>> i) & 1) !== 0);
  setFunc(8, 7, ((fmtBits >>> 6) & 1) !== 0);
  setFunc(8, 8, ((fmtBits >>> 7) & 1) !== 0);
  setFunc(7, 8, ((fmtBits >>> 8) & 1) !== 0);
  for (let i = 9; i < 15; i++) setFunc(14 - i, 8, ((fmtBits >>> i) & 1) !== 0);
  for (let i = 0; i < 8; i++) setFunc(size - 1 - i, 8, ((fmtBits >>> i) & 1) !== 0);
  for (let i = 8; i < 15; i++) setFunc(8, size - 15 + i, ((fmtBits >>> i) & 1) !== 0);
  setFunc(8, size - 8, true);

  return modules;
}

function QrCodeTool() {
  const [text, setText] = useState(() => window.location.origin);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const matrix = useMemo(() => (text.trim() ? encodeQrMatrix(text.trim()) : null), [text]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas || !matrix) return;
    const quiet = 4;
    const scale = 6;
    const total = (matrix.length + quiet * 2) * scale;
    canvas.width = total;
    canvas.height = total;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;
    ctx.fillStyle = '#ffffff';
    ctx.fillRect(0, 0, total, total);
    ctx.fillStyle = '#000000';
    for (let y = 0; y < matrix.length; y++) {
      for (let x = 0; x < matrix.length; x++) {
        if (matrix[y][x]) ctx.fillRect((x + quiet) * scale, (y + quiet) * scale, scale, scale);
      }
    }
  }, [matrix]);

  const downloadPng = () => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const a = document.createElement('a');
    a.href = canvas.toDataURL('image/png');
    a.download = 'qrcode.png';
    a.click();
  };

  return <div className={card}>
    <p className="text-xs text-muted">纯浏览器本地生成二维码（支持最多约 270 字节文本或网址），不发请求给后端。</p>
    <div className="flex flex-wrap gap-2">
      <input className={`${select} min-w-0 flex-1`} value={text} onChange={e => setText(e.target.value)} placeholder="输入网址或任意文本" />
      <button className={button} onClick={() => setText(window.location.href)}>填入当前页地址</button>
      <button className={button} disabled={!matrix} onClick={downloadPng}>下载 PNG</button>
    </div>
    {!text.trim() ? <p className="text-sm text-muted">请输入文本以生成二维码</p>
      : !matrix ? <p className="text-sm text-danger">内容过长（超过约 270 字节），请精简内容。</p>
      : <div className="flex flex-col items-start gap-2 pt-1">
          <canvas ref={canvasRef} className="rounded border border-line bg-white" style={{ width: 220, height: 220, imageRendering: 'pixelated' }} />
          <span className="text-xs text-muted">尺寸：{matrix.length}×{matrix.length} 模块</span>
        </div>}
  </div>;
}

// ---------------- JSON Formatter & Validator ----------------
function JsonTool() {
  const [input, setInput] = useState('{\n  "name": "omniHub",\n  "features": ["shortvideo", "gallery", "audio", "manga", "tools"]\n}');
  const [indent, setIndent] = useState(2);
  const [error, setError] = useState('');

  const format = (spaces: number) => {
    try {
      const parsed = JSON.parse(input);
      setInput(JSON.stringify(parsed, null, spaces));
      setError('');
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return <div className={card}>
    <div className="flex flex-wrap items-center gap-2">
      <button className={primaryButton} onClick={() => format(indent)}>格式化</button>
      <button className={button} onClick={() => format(0)}>压缩单行</button>
      <label className="flex items-center gap-1 text-xs text-muted">缩进
        <select className={select} value={indent} onChange={e => setIndent(Number(e.target.value))}>
          <option value={2}>2 空格</option>
          <option value={4}>4 空格</option>
        </select>
      </label>
      <button className={button} onClick={() => copyText(input)}>复制内容</button>
      <button className={button} onClick={() => { setInput(''); setError(''); }}>清空</button>
    </div>
    {error ? <p className="rounded border border-danger/50 bg-danger/10 px-2 py-1 font-mono text-xs text-danger">语法错误：{error}</p>
      : input.trim() ? <p className="text-xs text-[#3fb950]">JSON 格式有效</p> : null}
    <textarea className={`${select} w-full font-mono text-xs`} rows={14} value={input} onChange={e => { setInput(e.target.value); setError(''); }} placeholder="在此粘贴 JSON 文本…" />
  </div>;
}

// ---------------- Pure JS SHA-256 fallback (works over LAN HTTP where crypto.subtle is unavailable) ----------------
const K256 = new Uint32Array([
  0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
  0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
  0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
  0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
  0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
  0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
  0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
  0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
]);

function sha256Fallback(ascii: string): string {
  const bytes = new TextEncoder().encode(ascii);
  const bitLen = bytes.length * 8;
  const padLen = ((bytes.length + 9 + 63) & ~63);
  const buf = new Uint8Array(padLen);
  buf.set(bytes);
  buf[bytes.length] = 0x80;
  const view = new DataView(buf.buffer);
  view.setUint32(padLen - 8, Math.floor(bitLen / 0x100000000), false);
  view.setUint32(padLen - 4, bitLen >>> 0, false);

  let h0 = 0x6a09e667, h1 = 0xbb67ae85, h2 = 0x3c6ef372, h3 = 0xa54ff53a;
  let h4 = 0x510e527f, h5 = 0x9b05688c, h6 = 0x1f83d9ab, h7 = 0x5be0cd19;
  const w = new Uint32Array(64);
  const rotr = (x: number, n: number) => (x >>> n) | (x << (32 - n));

  for (let i = 0; i < padLen; i += 64) {
    for (let j = 0; j < 16; j++) w[j] = view.getUint32(i + j * 4, false);
    for (let j = 16; j < 64; j++) {
      const s0 = rotr(w[j - 15], 7) ^ rotr(w[j - 15], 18) ^ (w[j - 15] >>> 3);
      const s1 = rotr(w[j - 2], 17) ^ rotr(w[j - 2], 19) ^ (w[j - 2] >>> 10);
      w[j] = (w[j - 16] + s0 + w[j - 7] + s1) >>> 0;
    }
    let a = h0, b = h1, c = h2, d = h3, e = h4, f = h5, g = h6, h = h7;
    for (let j = 0; j < 64; j++) {
      const S1 = rotr(e, 6) ^ rotr(e, 11) ^ rotr(e, 25);
      const ch = (e & f) ^ (~e & g);
      const temp1 = (h + S1 + ch + K256[j] + w[j]) >>> 0;
      const S0 = rotr(a, 2) ^ rotr(a, 13) ^ rotr(a, 22);
      const maj = (a & b) ^ (a & c) ^ (b & c);
      const temp2 = (S0 + maj) >>> 0;
      h = g; g = f; f = e; e = (d + temp1) >>> 0;
      d = c; c = b; b = a; a = (temp1 + temp2) >>> 0;
    }
    h0 = (h0 + a) >>> 0; h1 = (h1 + b) >>> 0; h2 = (h2 + c) >>> 0; h3 = (h3 + d) >>> 0;
    h4 = (h4 + e) >>> 0; h5 = (h5 + f) >>> 0; h6 = (h6 + g) >>> 0; h7 = (h7 + h) >>> 0;
  }
  return [h0, h1, h2, h3, h4, h5, h6, h7].map(v => v.toString(16).padStart(8, '0')).join('');
}

function CodecHashTool() {
  const [input, setInput] = useState('');
  const [output, setOutput] = useState('');
  const [hashes, setHashes] = useState<Record<string, string>>({});

  useEffect(() => {
    let cancelled = false;
    if (!input) { setHashes({}); return; }
    const sha256Sync = sha256Fallback(input);
    setHashes({ 'SHA-256': sha256Sync });
    if (window.crypto?.subtle) {
      const data = new TextEncoder().encode(input);
      Promise.all(['SHA-1', 'SHA-256', 'SHA-384', 'SHA-512'].map(async alg => {
        const buf = await window.crypto.subtle.digest(alg, data);
        const hex = Array.from(new Uint8Array(buf)).map(b => b.toString(16).padStart(2, '0')).join('');
        return [alg, hex] as const;
      })).then(entries => {
        if (!cancelled) setHashes(Object.fromEntries(entries));
      }).catch(() => {});
    }
    return () => { cancelled = true; };
  }, [input]);

  const toBase64 = () => {
    try {
      const bytes = new TextEncoder().encode(input);
      let bin = '';
      for (const b of bytes) bin += String.fromCharCode(b);
      setOutput(btoa(bin));
    } catch (e) { setOutput(`错误：${(e as Error).message}`); }
  };
  const fromBase64 = () => {
    try {
      const bin = atob(input.trim());
      const bytes = Uint8Array.from(bin, c => c.charCodeAt(0));
      setOutput(new TextDecoder().decode(bytes));
    } catch (e) { setOutput(`Base64 解码失败：${(e as Error).message}`); }
  };

  return <div className={card}>
    <textarea className={`${select} w-full font-mono text-xs`} rows={4} value={input} onChange={e => setInput(e.target.value)} placeholder="输入原文或待解码的字符串…" />
    <div className="flex flex-wrap gap-2">
      <button className={button} onClick={toBase64}>Base64 编码</button>
      <button className={button} onClick={fromBase64}>Base64 解码</button>
      <button className={button} onClick={() => setOutput(encodeURIComponent(input))}>URL 编码</button>
      <button className={button} onClick={() => { try { setOutput(decodeURIComponent(input)); } catch (e) { setOutput(`URL 解码失败：${(e as Error).message}`); } }}>URL 解码</button>
      <button className={button} onClick={() => copyText(output)} disabled={!output}>复制结果</button>
    </div>
    {output ? <textarea className={`${select} w-full font-mono text-xs`} rows={4} readOnly value={output} /> : null}
    {Object.keys(hashes).length > 0 ? <div className="space-y-1.5 border-t border-line pt-2">
      <p className="text-xs text-muted">实时哈希值{window.crypto?.subtle ? '' : '（局域网 HTTP 模式提供内置 SHA-256）'}</p>
      {Object.entries(hashes).map(([alg, val]) => <div key={alg} className="flex items-center gap-2 rounded border border-line bg-background px-2 py-1 text-xs">
        <span className="w-16 shrink-0 font-semibold text-muted">{alg}</span>
        <code className="min-w-0 flex-1 break-all font-mono">{val}</code>
        <button className="shrink-0 text-primary hover:underline" onClick={() => copyText(val)}>复制</button>
      </div>)}
    </div> : null}
  </div>;
}

// ---------------- Timestamp Converter ----------------
function TimestampTool() {
  const [now, setNow] = useState(() => Date.now());
  const [tsInput, setTsInput] = useState(() => String(Math.floor(Date.now() / 1000)));
  const [dateInput, setDateInput] = useState(() => new Date().toISOString().slice(0, 19).replace('T', ' '));

  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(t);
  }, []);

  const parsedFromTs = useMemo(() => {
    const raw = tsInput.trim();
    if (!/^-?\d+$/.test(raw)) return null;
    const num = Number(raw);
    const ms = raw.length <= 10 ? num * 1000 : num;
    const d = new Date(ms);
    return Number.isNaN(d.getTime()) ? null : d;
  }, [tsInput]);

  const parsedFromDate = useMemo(() => {
    const d = new Date(dateInput.trim());
    return Number.isNaN(d.getTime()) ? null : d;
  }, [dateInput]);

  return <div className={card}>
    <div className="flex flex-wrap items-center justify-between gap-2 rounded border border-line bg-background px-3 py-2 text-sm">
      <span>当前时间：<code className="font-mono">{Math.floor(now / 1000)}</code> 秒 / <code className="font-mono">{now}</code> 毫秒</span>
      <div className="flex gap-2">
        <button className="text-xs text-primary hover:underline" onClick={() => copyText(String(Math.floor(now / 1000)))}>复制秒</button>
        <button className="text-xs text-primary hover:underline" onClick={() => copyText(String(now))}>复制毫秒</button>
      </div>
    </div>
    <div className="grid gap-3 sm:grid-cols-2">
      <div className="space-y-1.5">
        <label className="text-xs text-muted">时间戳（秒或毫秒）→ 日期</label>
        <input className={`${select} w-full font-mono`} value={tsInput} onChange={e => setTsInput(e.target.value)} />
        {parsedFromTs ? <div className="space-y-1 rounded border border-line bg-background p-2 font-mono text-xs">
          <p>本地：{parsedFromTs.toLocaleString()}</p>
          <p>UTC：{parsedFromTs.toISOString()}</p>
        </div> : <p className="text-xs text-danger">无效时间戳</p>}
      </div>
      <div className="space-y-1.5">
        <label className="text-xs text-muted">日期时间 → 时间戳</label>
        <input className={`${select} w-full font-mono`} value={dateInput} onChange={e => setDateInput(e.target.value)} placeholder="YYYY-MM-DD HH:mm:ss" />
        {parsedFromDate ? <div className="space-y-1 rounded border border-line bg-background p-2 font-mono text-xs">
          <p>秒级：{Math.floor(parsedFromDate.getTime() / 1000)} <button className="ml-2 text-primary hover:underline" onClick={() => copyText(String(Math.floor(parsedFromDate.getTime() / 1000)))}>复制</button></p>
          <p>毫秒：{parsedFromDate.getTime()} <button className="ml-2 text-primary hover:underline" onClick={() => copyText(String(parsedFromDate.getTime()))}>复制</button></p>
        </div> : <p className="text-xs text-danger">无法解析日期格式</p>}
      </div>
    </div>
  </div>;
}

// ---------------- Regex Tester ----------------
const REGEX_PRESETS: [string, string, string][] = [
  ['邮箱地址', '[\\w.-]+@[\\w.-]+\\.[a-zA-Z]{2,}', 'g'],
  ['URL 链接', 'https?:\\/\\/[^\\s/$.?#].[^\\s]*', 'gi'],
  ['IPv4 地址', '\\b(?:\\d{1,3}\\.){3}\\d{1,3}\\b', 'g'],
  ['日期 YYYY-MM-DD', '\\b(\\d{4})-(0[1-9]|1[0-2])-(0[1-9]|[12]\\d|3[01])\\b', 'g'],
];

function RegexTool() {
  const [pattern, setPattern] = useState('(\\w+)@([\\w.]+)');
  const [flags, setFlags] = useState('g');
  const [sample, setSample] = useState('联系 admin@omnihub.local 或 user@example.com 获取帮助。');

  const result = useMemo(() => {
    if (!pattern) return { error: '', matches: [] as RegExpExecArray[] };
    try {
      const f = flags.includes('g') ? flags : `${flags}g`;
      const re = new RegExp(pattern, f);
      const matches: RegExpExecArray[] = [];
      let m: RegExpExecArray | null;
      let guard = 0;
      while ((m = re.exec(sample)) !== null && guard++ < 500) {
        matches.push(m);
        if (m[0].length === 0) re.lastIndex++;
      }
      return { error: '', matches };
    } catch (e) {
      return { error: (e as Error).message, matches: [] as RegExpExecArray[] };
    }
  }, [pattern, flags, sample]);

  return <div className={card}>
    <div className="flex flex-wrap gap-1.5">
      {REGEX_PRESETS.map(([label, pat, flg]) => <button key={label} className={button} onClick={() => { setPattern(pat); setFlags(flg); }}>{label}</button>)}
    </div>
    <div className="flex flex-wrap gap-2">
      <input className={`${select} min-w-0 flex-1 font-mono`} value={pattern} onChange={e => setPattern(e.target.value)} placeholder="正则表达式（不含两侧斜杠）" />
      <input className={`${select} w-24 font-mono`} value={flags} onChange={e => setFlags(e.target.value)} placeholder="gims" title="正则修饰符，如 gim" />
    </div>
    {result.error ? <p className="text-xs text-danger">正则错误：{result.error}</p> : null}
    <textarea className={`${select} w-full font-mono text-xs`} rows={4} value={sample} onChange={e => setSample(e.target.value)} placeholder="输入待测试的文本…" />
    <div className="space-y-1">
      <p className="text-xs text-muted">匹配结果（共 {result.matches.length} 处）</p>
      <ul className="max-h-48 space-y-1 overflow-y-auto">
        {result.matches.map((m, idx) => <li key={idx} className="rounded border border-line bg-background px-2 py-1 font-mono text-xs">
          <span className="text-primary">#{idx + 1}</span> 位置 {m.index}：<code>{m[0]}</code>
          {m.length > 1 ? <span className="ml-2 text-muted">分组：[{m.slice(1).map(g => g ?? 'null').join(', ')}]</span> : null}
        </li>)}
      </ul>
    </div>
  </div>;
}
