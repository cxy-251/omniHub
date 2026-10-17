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
    Object.assign(window, settings);
    window.addEventListener('error', event => showError(event.message || '模拟器运行失败'));
    window.addEventListener('unhandledrejection', event => showError(event.reason || '模拟器运行失败'));
    await loadScript('/vendor/players/emulatorjs/data/loader.js');
  } catch (failure) { showError(failure); }
})();
