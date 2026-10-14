import { gameResource, loadScript, showError } from './common';

type RuffleElement = HTMLElement & { ruffle(): { load(options: { url: string; allowScriptAccess: boolean }): Promise<void> } };
type RuffleAPI = { config: Record<string, unknown>; newest?(): { createPlayer(): RuffleElement } };
void (async () => {
  try {
    const source = gameResource();
    if (!source.pathname.toLowerCase().endsWith('.swf')) throw new Error('Flash 资源入口无效');
    const runtime = window as unknown as { RufflePlayer: RuffleAPI };
    runtime.RufflePlayer = { config: { publicPath: '/vendor/players/ruffle/', autoplay: 'on', unmuteOverlay: 'visible', contextMenu: 'off', openUrlMode: 'deny', allowScriptAccess: false } };
    await loadScript('/vendor/players/ruffle/ruffle.js');
    const version = runtime.RufflePlayer.newest?.();
    if (!version) throw new Error('Flash 运行器初始化失败');
    const player = version.createPlayer();
    const container = document.getElementById('game');
    if (!container) throw new Error('Flash 播放区域缺失');
    container.appendChild(player);
    await player.ruffle().load({ url: source.href, allowScriptAccess: false });
  } catch (failure) { showError(failure); }
})();
