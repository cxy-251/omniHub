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
const base = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../.test-tmp/web-runtime'), delay = ms => new Promise(r => setTimeout(r, ms));
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
    const pending = new Map(), contexts = new Map(), exceptions = [], remoteRequests = [];
    ws.onmessage = event => {
        const data = JSON.parse(event.data);
        if (data.id) {
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
            void send('Network.enable', {}, data.params.sessionId);
        }
        else if (data.method === 'Target.detachedFromTarget') {
            for (const [frame, context] of contexts) {
                if (context.sessionId === data.params.sessionId)
                    contexts.delete(frame);
            }
        }
        else if (data.method === 'Network.requestWillBeSent') {
            const address = new URL(data.params.request.url);
            if (['http:', 'https:'].includes(address.protocol) && address.origin !== new URL(url).origin)
                remoteRequests.push(address.href);
        }
        else if (data.method === 'Runtime.exceptionThrown')
            exceptions.push(data.params.exceptionDetails.text);
    };
    const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
        const id = ++sequence;
        pending.set(id, { resolve, reject });
        ws.send(JSON.stringify({ id, method, params, sessionId }));
        setTimeout(() => {
            if (pending.delete(id))
                reject(Error('CDP timeout ' + method));
        }, 10000).unref();
    });
    const evaluate = async (expression, context) => {
        const result = await send('Runtime.evaluate', { expression, contextId: context?.id, awaitPromise: true, returnByValue: true, userGesture: true }, context?.sessionId);
        if (result.exceptionDetails)
            throw Error(JSON.stringify(result.exceptionDetails));
        return result.result.value;
    };
    const until = async (expression, label, contextId) => {
        for (let i = 0; i < 100; i++) {
            if (await evaluate(expression, contextId))
                return;
            await delay(100);
        }
        throw Error('timeout ' + label);
    };
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
    await until("document.querySelectorAll('article').length===2", 'runtime cards');
    const results = [];
    for (const id of ['001', '002']) {
        await evaluate(`[...document.querySelectorAll('article')].find(a=>a.querySelector('p').textContent.trim().endsWith(' · ${id}')).querySelector('button').click()`);
        await until("!!document.querySelector('iframe')", 'runtime frame');
        const source = await evaluate("document.querySelector('iframe').src");
        if (!new URL(source).pathname.endsWith(id === '001' ? '/retro.html' : '/flash.html'))
            throw Error('wrong runtime selected ' + source);
        let context;
        for (let i = 0; i < 100; i++) {
            for (const candidate of contexts.values()) {
                if (!candidate.sessionId)
                    continue;
                let tree;
                try {
                    tree = await send('Page.getFrameTree', {}, candidate.sessionId);
                }
                catch (error) {
                    if (error.message.includes('Session with given id not found'))
                        continue;
                    throw error;
                }
                if (tree.frameTree.frame.url === source) {
                    context = candidate;
                    break;
                }
            }
            if (context)
                break;
            await delay(100);
        }
        if (!context)
            throw Error('runtime context missing');
        await until("!!document.querySelector('canvas') || !!document.querySelector('ruffle-player')?.shadowRoot?.querySelector('canvas')", 'canvas', context);
        await delay(3000);
        const detail = await evaluate("({error:document.querySelector('#error')?.hidden===false?document.querySelector('#error').textContent:'',text:document.body.innerText,emulatorStarted:!!window.EJS_emulator?.started,origin:globalThis.origin,ruffle:!!document.querySelector('ruffle-player'),metadata:document.querySelector('ruffle-player')?.ruffle()?.metadata})", context);
        const shot = await send('Page.captureScreenshot', { format: 'png' });
        fs.writeFileSync(path.join(base, id + '.png'), Buffer.from(shot.data, 'base64'));
        if ((id === '001' && !detail.emulatorStarted) || (id === '002' && !detail.ruffle))
            throw Error('runtime identity unproven ' + JSON.stringify(detail));
        if (detail.error)
            throw Error('runtime error ' + JSON.stringify(detail));
        results.push({ id, ...detail });
        await evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent==='退出游戏').click()");
        await until("!document.querySelector('iframe')", 'runtime exit');
        const asset = new URL(source).searchParams.get('url');
        let revoked = false;
        for (let attempt = 0; attempt < 100; attempt++) {
            if ((await fetch(new URL(asset, url))).status === 403) { revoked = true; break; }
            await delay(50);
        }
        if (!revoked) throw Error('runtime exit retained its resource grant');
    }
    fs.writeFileSync(path.join(base, 'result.json'), JSON.stringify({ results, exceptions, remoteRequests }, null, 2));
    if (remoteRequests.length)
        throw Error('unexpected external network ' + JSON.stringify(remoteRequests));
    if (exceptions.length)
        throw Error('runtime exceptions ' + JSON.stringify(exceptions));
    console.log('WEB_RUNTIME_CHECK_OK ' + JSON.stringify({ results, exceptions }));
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
