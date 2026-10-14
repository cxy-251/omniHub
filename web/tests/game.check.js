// Optional Chrome/CDP integration check. Uses a caller-owned local Go fixture
// and an isolated project profile; never opens the user's browser profile.
import { fileURLToPath } from 'node:url';
import fs from 'node:fs';
import path from 'node:path';
import { spawn } from 'node:child_process';
let input = '';
for await (const chunk of process.stdin)
    input += chunk;
const { url, cookie } = JSON.parse(input);
const service = new URL(url);
if (service.protocol !== 'http:' || !['127.0.0.1', '[::1]'].includes(service.hostname) || typeof cookie !== 'string')
    throw Error('The browser check requires an isolated loopback fixture');
const base = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../.test-tmp/web-game'), delay = ms => new Promise(r => setTimeout(r, ms));
fs.mkdirSync(base, { recursive: true });
const chromePath = process.env.OMNIHUB_CHROME;
if (!chromePath)
    throw Error('Set OMNIHUB_CHROME to an existing Chrome executable');
fs.accessSync(chromePath, fs.constants.X_OK);
const profile = path.join(base, 'profile-' + Date.now() + '-' + process.pid);
fs.mkdirSync(profile, { mode: 0o700 });
const log = fs.openSync(path.join(base, 'chrome.log'), 'w');
let chrome, ws;
try {
    chrome = spawn(chromePath, ['--headless', '--mute-audio', '--password-store=basic', '--use-mock-keychain', '--no-sandbox', '--disable-gpu', '--disable-dev-shm-usage', '--disable-background-networking', '--disable-component-update', '--disable-sync', '--no-first-run', '--remote-debugging-port=0', `--user-data-dir=${profile}`, 'about:blank'], { env: { ...process.env, XDG_CACHE_HOME: path.join(profile, 'cache'), XDG_CONFIG_HOME: path.join(profile, 'config') }, stdio: ['ignore', log, log] });
    let port;
    for (let i = 0; i < 100; i++) {
        try {
            port = +fs.readFileSync(path.join(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0];
            if (port)
                break;
        }
        catch (failure) {
            if (failure.code !== 'ENOENT')
                throw failure;
        }
        await delay(100);
    }
    if (!port)
        throw Error('Chrome CDP unavailable');
    const target = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find(t => t.type === 'page');
    ws = new WebSocket(target.webSocketDebuggerUrl);
    await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
    let sequence = 0;
    const pending = new Map(), contexts = new Map(), exceptions = [];
    ws.onmessage = event => { const data = JSON.parse(event.data); if (data.id) {
        const item = pending.get(data.id);
        if (item) {
            pending.delete(data.id);
            data.error ? item.reject(Error(data.error.message)) : item.resolve(data.result);
        }
    }
    else if (data.method === 'Runtime.executionContextCreated') {
        const c = data.params.context;
        if (c.auxData?.isDefault)
            contexts.set(c.auxData.frameId, { id: c.id, sessionId: data.sessionId });
    }
    else if (data.method === 'Target.attachedToTarget') {
        void send('Runtime.enable', {}, data.params.sessionId);
    }
    else if (data.method === 'Runtime.exceptionThrown')
        exceptions.push(data.params.exceptionDetails.text); };
    const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => { const id = ++sequence; pending.set(id, { resolve, reject }); ws.send(JSON.stringify({ id, method, params, sessionId })); setTimeout(() => { if (pending.delete(id))
        reject(Error('CDP timeout ' + method)); }, 10000).unref(); });
    const evaluate = async (expression, context) => { const result = await send('Runtime.evaluate', { expression, contextId: context?.id, awaitPromise: true, returnByValue: true, userGesture: true }, context?.sessionId); if (result.exceptionDetails)
        throw Error(JSON.stringify(result.exceptionDetails)); return result.result.value; };
    const until = async (expression, label, contextId) => { for (let i = 0; i < 100; i++) {
        if (await evaluate(expression, contextId))
            return;
        await delay(100);
    } throw Error('timeout ' + label); };
    await send('Runtime.enable');
    await send('Page.enable');
    await send('Target.setAutoAttach', { autoAttach: true, waitForDebuggerOnStart: false, flatten: true });
    await send('Network.enable');
    await send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false });
    await send('Network.setCookie', { name: 'omni_token', value: cookie, url });
    await send('Page.navigate', { url });
    await until(`[...document.querySelectorAll('nav button')].some(b=>b.textContent==='网页游戏')`, 'games navigation');
    const role = await evaluate(`fetch('/api/auth/status').then(r=>r.json()).then(value=>value.user.role)`);
    if (role !== 'admin')
        throw Error('Browser fixture did not authenticate its real web account');
    await evaluate(`[...document.querySelectorAll('nav button')].find(b=>b.textContent==='网页游戏').click()`);
    await until(`document.querySelectorAll('article').length===1`, 'catalog');
    const catalog = await evaluate(`({cards:document.querySelectorAll('article').length,title:document.querySelector('article h3').textContent})`);
    await evaluate(`document.querySelector('article button').click()`);
    await until(`!!document.querySelector('iframe')`, 'frame');
    const source = await evaluate(`document.querySelector('iframe').getAttribute('src')`);
    let gameContext;
    const frames = [];
    for (let i = 0; i < 100; i++) {
        for (const context of contexts.values()) {
            if (!context.sessionId)
                continue;
            const tree = await send('Page.getFrameTree', {}, context.sessionId);
            if (tree.frameTree.frame.url === new URL(source, url).href) {
                gameContext = context;
                break;
            }
            if (i === 99)
                frames.push(tree);
        }
        if (gameContext)
            break;
        await delay(100);
    }
    if (!gameContext)
        throw Error('game context missing ' + JSON.stringify(frames));
    await until(`document.body.dataset.ready==='yes'`, 'game script and relative XHR', gameContext);
    const probe = await evaluate(`({...window.probe,origin:globalThis.origin,body:document.body.innerText})`, gameContext);
    if (!probe.parentBlocked || !probe.apiBlocked || !['first', 'second'].includes(probe.asset) || probe.origin !== 'null')
        throw Error('isolation failed ' + JSON.stringify(probe));
    const shot = await send('Page.captureScreenshot', { format: 'png' });
    fs.writeFileSync(path.join(base, 'playing.png'), Buffer.from(shot.data, 'base64'));
    await evaluate(`[...document.querySelectorAll('nav button')].find(b=>b.textContent==='门户大厅').click()`);
    await until(`document.querySelectorAll('iframe').length===0`, 'frame disposal');
    let closed = false;
    for (let i = 0; i < 100; i++) {
        if ((await fetch(new URL(source, url))).status === 403) {
            closed = true;
            break;
        }
        await delay(50);
    }
    if (!closed)
        throw Error('leaving module did not revoke grant');
    const result = { catalog, probe, frameDisposed: true, grantRevoked: true, exceptions };
    fs.writeFileSync(path.join(base, 'result.json'), JSON.stringify(result, null, 2));
    if (exceptions.length)
        throw Error('unexpected browser exceptions ' + exceptions);
    console.log('WEB_GAME_CHECK_OK ' + JSON.stringify(result));
}
finally {
    ws?.close();
    if (chrome?.exitCode === null && chrome.signalCode === null)
        chrome.kill('SIGTERM');
    await delay(500);
    if (chrome?.exitCode === null && chrome.signalCode === null)
        chrome.kill('SIGKILL');
    fs.closeSync(log);
    fs.rmSync(profile, { recursive: true, force: true });
}
