import { gameResource, loadScript, showError } from './common';

// 复古游戏播放页（EmulatorJS）。机种优先用服务端按游戏目录名判断的 core 参数（如 PS1 的 .zip 不能按扩展名当街机），
// 没有时按 ROM 扩展名推断。街机需要 BIOS 时，服务端给出同一授权下的 BIOS 地址（bios 参数）。
const cores: Readonly<Record<string, string>> = { nes: 'nes', sfc: 'snes', smc: 'snes', gb: 'gb', gbc: 'gb', gba: 'gba', n64: 'n64', z64: 'n64', v64: 'n64', nds: 'nds', md: 'segaMD', gen: 'segaMD', sms: 'segaMS', gg: 'segaGG', pce: 'pce', zip: 'arcade', cue: 'psx', chd: 'psx', pbp: 'psx' };
const known = new Set(['nes', 'snes', 'gb', 'gba', 'n64', 'nds', 'segaMD', 'segaMS', 'segaGG', 'pce', 'arcade', 'psx']);
void (async () => {
  try {
    const params = new URLSearchParams(location.search);
    const source = gameResource();
    const extension = source.pathname.split('.').pop()?.toLowerCase() ?? '';
    const requested = params.get('core') ?? '';
    const core = known.has(requested) ? requested : cores[extension];
    if (!core) throw new Error('未识别的 ROM 类型');
    const settings: Record<string, unknown> = {
      EJS_player: '#game', EJS_core: core, EJS_gameUrl: source.href,
      EJS_pathtodata: '/vendor/players/emulatorjs/data/', EJS_startOnLoaded: true,
      EJS_disableDatabases: true, EJS_forceLegacyCores: true, EJS_threads: false,
      EJS_DEBUG_XX: true,
      EJS_language: 'zh-CN', EJS_gameName: decodeURIComponent(source.pathname.split('/').pop() ?? ''),
    };
    // 街机 BIOS（如 pgm.zip）要以原文件名、不解压地放进模拟器的系统目录：EmulatorJS 把相对地址原样当文件名写入，
    // 所以用 <base> 让相对地址 "pgm.zip" 指向授权下的 BIOS。
    const bios = params.get('bios');
    if (bios) {
      const url = new URL(bios, location.href);
      if (url.origin !== location.origin || !/^\/game-content\/[^/]+\/__bios\/[\w.-]+\.zip$/.test(url.pathname)) throw new Error('BIOS 地址无效');
      const base = document.createElement('base');
      base.href = url.pathname.slice(0, url.pathname.lastIndexOf('/') + 1);
      document.head.prepend(base);
      settings.EJS_biosUrl = url.pathname.split('/').pop();
      settings.EJS_dontExtractBIOS = true;
    }
    await settingsStorage(source);
    Object.assign(window, settings, persistence(source), touchControls());
    window.addEventListener('error', event => showError(event.message || '模拟器运行失败'));
    window.addEventListener('unhandledrejection', event => showError(event.reason || '模拟器运行失败'));
    await loadScript('/vendor/players/emulatorjs/data/loader.js');
  } catch (failure) { showError(failure); }
})();

// 存档：沙箱页里浏览器存储（IndexedDB）不可用，EmulatorJS 自己存不住。都存到服务端、按账号与游戏分开
// （同一授权下的 __savefile 接口；访客不保存）。二进制按 base64 文本存。
//   - 游戏内存档（.srm）：每 30 秒、切走标签页时写出。
//   - 即时存档：按设置里的「存档位」1～9 分开（工具栏的保存/读取、快速存读、快捷键都走这里）；1 号位沿用旧文件名 retro.state。
//   - 自动存档：每 5 分钟、切走标签页时存一次；下次打开时提示「继续上次进度」。
function persistence(source: URL): Record<string, unknown> {
  const token = source.pathname.split('/')[2];
  const fileUrl = (name: string) => `/game-content/${token}/__savefile/${name}`;
  const load = async (name: string) => {
    const response = await fetch(fileUrl(name), { cache: 'no-store' });
    if (response.status === 404) return null;
    if (!response.ok) throw new Error(`读取存档失败（${response.status}）`);
    return fromBase64(await response.text());
  };
  const store = async (name: string, data: Uint8Array) => {
    const body = await toBase64(data);
    // 关标签页时只有 keepalive 请求能送出去，但它限 64KB；小存档（NES、GB 等）才用得上。
    const response = await fetch(fileUrl(name), { method: 'POST', body, keepalive: body.length < 60_000 });
    if (!response.ok) throw new Error(response.status === 413 ? '存档太大，服务端放不下' : response.status === 403 ? '未登录，存档只留在本页' : `存档失败（${response.status}）`);
  };
  const setMeta = (key: string, value: string) => fetch(`/game-content/${token}/__save`, { method: 'POST', body: JSON.stringify({ key, value }), keepalive: true });
  const emulator = () => (window as unknown as { EJS_emulator: Emulator }).EJS_emulator;
  const slot = () => String(emulator().getSettingValue('save-state-slot') || '1');
  const slotFile = (n: string) => n === '1' ? 'retro.state' : `retro.state${n}`;
  const saveSlot = (n: string, state: Uint8Array) => store(slotFile(n), state)
    .then(() => emulator().displayMessage(`已保存到存档位 ${n}`), (failure: Error) => emulator().displayMessage(failure.message));
  const loadSlot = (n: string) => load(slotFile(n)).then(state => {
    if (!state) { emulator().displayMessage(`存档位 ${n} 是空的`); return; }
    emulator().gameManager.loadState(state);
    emulator().displayMessage(`已读取存档位 ${n}`);
  }, () => emulator().displayMessage('读取即时存档失败'));
  // 服务端的存档读回来之前不往回写，免得新开的空存档盖掉旧进度；读失败就整局都不写。
  let uploaded = '', loaded = false;
  return {
    // 游戏内存档的自动写出间隔（秒）；切走或关标签页时也写一次。
    EJS_defaultOptions: { 'save-save-interval': '30' },
    EJS_onGameStart: async () => {
      const ejs = emulator();
      const gm = ejs.gameManager;
      // 快速存读（菜单、快捷键）原本只写进模拟器内存里的文件，关页就没了；改成同一套服务端存档位。
      const quickSave = gm.quickSave.bind(gm);
      gm.quickSave = (n?: string) => {
        const ok = quickSave(n);
        if (ok) void saveSlot(String(n || '1'), gm.FS.readFile(`/${n || 1}-quick.state`));
        return ok;
      };
      gm.quickLoad = (n?: string) => { void loadSlot(String(n || '1')); };
      ejs.on('saveSaveFiles', (data: Uint8Array | null) => {
        if (!loaded || !data?.length) return;
        void toBase64(data).then(text => {
          if (text === uploaded) return;
          uploaded = text;
          return store('retro.srm', data);
        }).catch((failure: Error) => { uploaded = ''; ejs.displayMessage(failure.message); });
      });
      void autoSave(ejs);
      let saved: Uint8Array | null;
      try { saved = await load('retro.srm'); } catch (failure) { ejs.displayMessage(`${(failure as Error).message}，本局不会自动存档`); return; }
      loaded = true;
      if (!saved) return;
      const fs = gm.FS, path = gm.getSaveFilePath();
      let dir = '';
      for (const part of path.split('/').slice(1, -1)) { dir += '/' + part; if (!fs.analyzePath(dir).exists) fs.mkdir(dir); }
      fs.writeFile(path, saved);
      gm.loadSaveFiles();
      uploaded = await toBase64(saved);
    },
    EJS_onSaveState: ({ state }: { state: Uint8Array }) => { void saveSlot(slot(), state); },
    EJS_onLoadState: () => { void loadSlot(slot()); },
  };

  // 自动存档与「继续上次进度」。上次的进度在你选择继续或放弃（或 20 秒没选）之前不会被覆盖。
  async function autoSave(ejs: Emulator) {
    let at = '';
    try {
      const response = await fetch(`/game-content/${token}/__save`, { cache: 'no-store' });
      if (response.ok) at = (await response.json() as Record<string, string>)['retro-auto-at'] ?? '';
    } catch { /* 没有就不提示 */ }
    let armed = false, ready = false;
    const arm = () => { if (!armed) { armed = true; window.setTimeout(() => { ready = true; }, 60_000); } };
    if (at) offerResume(at, async () => {
      const state = await load('retro.auto.state').catch(() => null);
      if (state) { ejs.gameManager.loadState(state); ejs.displayMessage('已继续上次进度'); } else ejs.displayMessage('没有找到上次的进度');
    }, arm); else arm();
    let busy = false;
    const save = () => {
      if (!ready || busy || !ejs.started) return;
      busy = true;
      let state: Uint8Array;
      try { state = ejs.gameManager.getState(); } catch { busy = false; return; }
      store('retro.auto.state', state).then(() => setMeta('retro-auto-at', new Date().toISOString())).catch(() => undefined).finally(() => { busy = false; });
    };
    window.setInterval(() => { if (!document.hidden) save(); }, 5 * 60_000);
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) return;
      if (ejs.started) ejs.gameManager.saveSaveFiles();
      save();
    });
  }
}

// offerResume 在画面上方显示「继续上次进度」按钮，20 秒后或选择后消失；done 在消失时调用。
function offerResume(at: string, resume: () => Promise<void>, done: () => void) {
  const box = document.createElement('div');
  box.style.cssText = 'position:fixed;top:12px;left:50%;transform:translateX(-50%);z-index:10000;display:flex;gap:8px;align-items:center;background:#161b22ee;border:1px solid #30363d;border-radius:8px;padding:8px 10px;font:14px system-ui;color:#e6edf3';
  const when = new Date(at);
  const label = Number.isNaN(when.getTime()) ? '' : `（${when.getMonth() + 1}月${when.getDate()}日 ${String(when.getHours()).padStart(2, '0')}:${String(when.getMinutes()).padStart(2, '0')}）`;
  const yes = document.createElement('button');
  yes.textContent = `▶ 继续上次进度${label}`;
  const no = document.createElement('button');
  no.textContent = '×';
  no.title = '从头开始（上次的进度会在 1 分钟后被新的自动存档替换）';
  for (const b of [yes, no]) b.style.cssText = 'background:#21262d;color:#e6edf3;border:1px solid #30363d;border-radius:6px;padding:4px 10px;cursor:pointer;font:inherit';
  box.append(yes, no);
  document.body.append(box);
  let closed = false;
  const close = () => { if (closed) return; closed = true; box.remove(); window.clearTimeout(timer); done(); };
  const timer = window.setTimeout(close, 20_000);
  yes.addEventListener('click', () => { void resume().finally(close); });
  no.addEventListener('click', close);
}

// 模拟器设置（键位、秘籍、音量与菜单里的选项）：EmulatorJS 存在 localStorage 的 "ejs-…" 键里，沙箱页没有 localStorage。
// 换成一个按账号存在服务端的替身（同一授权下的 __save 键值接口，按游戏分开）；只收 "ejs-" 开头的键。访客只留在本页。
async function settingsStorage(source: URL): Promise<void> {
  const endpoint = `/game-content/${source.pathname.split('/')[2]}/__save`;
  const data = new Map<string, string>();
  try {
    const response = await fetch(endpoint, { cache: 'no-store' });
    if (response.ok) for (const [key, value] of Object.entries(await response.json() as Record<string, string>)) if (key.startsWith('ejs-')) data.set(key, value);
  } catch { /* 读不到就用默认设置 */ }
  const pending = new Map<string, number>();
  const persist = (key: string, value: string | null) => {
    if (!key.startsWith('ejs-')) return;
    window.clearTimeout(pending.get(key));
    pending.set(key, window.setTimeout(() => {
      void fetch(endpoint, { method: 'POST', body: JSON.stringify({ key, value }), keepalive: true }).catch(() => undefined);
    }, 500));
  };
  const storage: Storage = {
    get length() { return data.size; },
    key: index => [...data.keys()][index] ?? null,
    getItem: key => data.get(String(key)) ?? null,
    setItem: (key, value) => { data.set(String(key), String(value)); persist(String(key), String(value)); },
    removeItem: key => { data.delete(String(key)); persist(String(key), null); },
    clear: () => { for (const key of [...data.keys()]) storage.removeItem(key); },
  };
  Object.defineProperty(window, 'localStorage', { configurable: true, get: () => storage });
}

// 触屏按键：EmulatorJS 自带按机种布局的虚拟手柄，但只在手机浏览器标识、或手指点了它的开始按钮时才显示；
// 我们自动开始游戏，iPad（报的是 Mac 标识）和 Steam Deck 触屏就看不到。改成：手指一碰屏幕就显示，按键盘或实体手柄时收起。
function touchControls(): Record<string, unknown> {
  if (navigator.maxTouchPoints <= 0) return {};
  const ejs = () => (window as unknown as { EJS_emulator?: Emulator }).EJS_emulator;
  const show = (visible: boolean) => { const e = ejs(); if (e?.started && e.toggleVirtualGamepad) e.toggleVirtualGamepad(visible); };
  window.addEventListener('pointerdown', event => { if (event.pointerType === 'touch') show(true); }, true);
  window.addEventListener('keydown', () => show(false), true);
  window.addEventListener('gamepadconnected', () => show(false));
  return {};
}

type Emulator = {
  started: boolean;
  on(event: string, callback: (data: Uint8Array | null) => void): void;
  displayMessage(message: string): void;
  getSettingValue(name: string): string | undefined;
  toggleVirtualGamepad?: (visible: boolean) => void;
  gameManager: {
    FS: { analyzePath(path: string): { exists: boolean }; mkdir(path: string): void; writeFile(path: string, data: Uint8Array): void; readFile(path: string): Uint8Array };
    getSaveFilePath(): string; loadSaveFiles(): void; saveSaveFiles(): void; loadState(state: Uint8Array): void; getState(): Uint8Array;
    quickSave(slot?: string): boolean; quickLoad(slot?: string): void;
  };
};

function toBase64(data: Uint8Array): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result).slice(String(reader.result).indexOf(',') + 1));
    reader.onerror = () => reject(reader.error ?? new Error('存档编码失败'));
    reader.readAsDataURL(new Blob([data.slice()]));
  });
}

function fromBase64(text: string): Uint8Array {
  const binary = atob(text.trim());
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}
