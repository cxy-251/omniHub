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
      EJS_disableLocalStorage: true, EJS_DEBUG_XX: true,
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
    Object.assign(window, settings, persistence(source), touchControls());
    window.addEventListener('error', event => showError(event.message || '模拟器运行失败'));
    window.addEventListener('unhandledrejection', event => showError(event.reason || '模拟器运行失败'));
    await loadScript('/vendor/players/emulatorjs/data/loader.js');
  } catch (failure) { showError(failure); }
})();

// 存档：沙箱页里浏览器存储（IndexedDB）不可用，EmulatorJS 自己存不住。游戏内存档（.srm）和即时存档（一个槽）
// 都存到服务端、按账号与游戏分开（同一授权下的 __savefile 接口；访客不保存）。二进制按 base64 文本存。
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
    const response = await fetch(fileUrl(name), { method: 'POST', body: await toBase64(data) });
    if (!response.ok) throw new Error(response.status === 413 ? '存档太大，服务端放不下' : response.status === 403 ? '未登录，存档只留在本页' : `存档失败（${response.status}）`);
  };
  const emulator = () => (window as unknown as { EJS_emulator: Emulator }).EJS_emulator;
  // 服务端的存档读回来之前不往回写，免得新开的空存档盖掉旧进度；读失败就整局都不写。
  let uploaded = '', loaded = false;
  return {
    // 游戏内存档的自动写出间隔（秒）；切走或关标签页时也写一次。
    EJS_defaultOptions: { 'save-save-interval': '30' },
    EJS_onGameStart: async () => {
      const ejs = emulator();
      ejs.on('saveSaveFiles', (data: Uint8Array | null) => {
        if (!loaded || !data?.length) return;
        void toBase64(data).then(text => {
          if (text === uploaded) return;
          uploaded = text;
          return store('retro.srm', data);
        }).catch((failure: Error) => { uploaded = ''; ejs.displayMessage(failure.message); });
      });
      document.addEventListener('visibilitychange', () => { if (document.hidden && ejs.started) ejs.gameManager.saveSaveFiles(); });
      let saved: Uint8Array | null;
      try { saved = await load('retro.srm'); } catch (failure) { ejs.displayMessage(`${(failure as Error).message}，本局不会自动存档`); return; }
      loaded = true;
      if (!saved) return;
      const fs = ejs.gameManager.FS, path = ejs.gameManager.getSaveFilePath();
      let dir = '';
      for (const part of path.split('/').slice(1, -1)) { dir += '/' + part; if (!fs.analyzePath(dir).exists) fs.mkdir(dir); }
      fs.writeFile(path, saved);
      ejs.gameManager.loadSaveFiles();
      uploaded = await toBase64(saved);
    },
    EJS_onSaveState: ({ state }: { state: Uint8Array }) => {
      store('retro.state', state).then(() => emulator().displayMessage('即时存档已保存'), (failure: Error) => emulator().displayMessage(failure.message));
    },
    EJS_onLoadState: () => {
      load('retro.state').then(state => {
        if (!state) { emulator().displayMessage('还没有即时存档'); return; }
        emulator().gameManager.loadState(state);
        emulator().displayMessage('已读取即时存档');
      }, () => emulator().displayMessage('读取即时存档失败'));
    },
  };
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
  toggleVirtualGamepad?: (visible: boolean) => void;
  gameManager: {
    FS: { analyzePath(path: string): { exists: boolean }; mkdir(path: string): void; writeFile(path: string, data: Uint8Array): void };
    getSaveFilePath(): string; loadSaveFiles(): void; saveSaveFiles(): void; loadState(state: Uint8Array): void;
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
