import { gameResource, loadScript, showError } from './common';

const cores: Readonly<Record<string, string>> = { nes: 'nes', sfc: 'snes', smc: 'snes', gb: 'gb', gbc: 'gb', gba: 'gba', n64: 'n64', z64: 'n64', v64: 'n64', nds: 'nds', md: 'segaMD', gen: 'segaMD', sms: 'segaMS', gg: 'segaGG', pce: 'pce', zip: 'arcade' };
void (async () => {
  try {
    const source = gameResource();
    const extension = source.pathname.split('.').pop()?.toLowerCase() ?? '';
    if (!cores[extension]) throw new Error('未识别的 ROM 类型');
    Object.assign(window, {
      EJS_player: '#game', EJS_core: cores[extension], EJS_gameUrl: source.href,
      EJS_pathtodata: '/vendor/players/emulatorjs/data/', EJS_startOnLoaded: true,
      EJS_disableDatabases: true, EJS_forceLegacyCores: true, EJS_threads: false,
      EJS_disableLocalStorage: true, EJS_DEBUG_XX: true,
      EJS_language: 'zh-CN', EJS_gameName: decodeURIComponent(source.pathname.split('/').pop() ?? ''),
    });
    window.addEventListener('error', event => showError(event.message || '模拟器运行失败'));
    window.addEventListener('unhandledrejection', event => showError(event.reason || '模拟器运行失败'));
    await loadScript('/vendor/players/emulatorjs/data/loader.js');
  } catch (failure) { showError(failure); }
})();
