// 开发验证：驱动一个已启动的无头 Chrome（--remote-debugging-port=9333），按步骤操作页面后截图。不装任何依赖（Node 自带 WebSocket）。
// 用法：node scripts/dev-cdp.mjs <起始网址> <截图.png> <结束前等待毫秒> [步骤…]
// 步骤：nav:<网址>  wait:<毫秒>  click:<x>:<y>  key:<key>:<code>:<keyCode>  shot:<文件>  其余按 JS 在页面里执行（可返回 Promise）。
// 页面的 console 与未捕获异常会打印出来。
import { writeFileSync } from 'node:fs';

const [url, out, wait, ...steps] = process.argv.slice(2);
const targets = await (await fetch('http://127.0.0.1:9333/json/list')).json();
const page = targets.find(t => t.type === 'page');
const ws = new WebSocket(page.webSocketDebuggerUrl);
await new Promise(r => ws.addEventListener('open', r));
let id = 0;
const pending = new Map();
ws.addEventListener('message', e => {
  const m = JSON.parse(e.data);
  if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); }
  else if (m.method === 'Runtime.consoleAPICalled') console.log('console:', m.params.args.map(a => a.value ?? a.description).join(' '));
  else if (m.method === 'Runtime.exceptionThrown') console.log('exception:', m.params.exceptionDetails.exception?.description ?? m.params.exceptionDetails.text);
});
const send = (method, params = {}) => new Promise(r => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })); });
const sleep = ms => new Promise(r => setTimeout(r, ms));
const shot = async file => { const s = await send('Page.captureScreenshot', { format: 'png' }); writeFileSync(file, Buffer.from(s.result.data, 'base64')); };

await send('Runtime.enable');
await send('Page.enable');
await send('Page.navigate', { url });
await sleep(3000);
for (const step of steps) {
  const [kind, ...rest] = step.split(':');
  if (kind === 'nav') { await send('Page.navigate', { url: step.slice(4) }); await sleep(3000); }
  else if (kind === 'wait') await sleep(Number(rest[0]));
  else if (kind === 'shot') await shot(step.slice(5));
  else if (kind === 'click') {
    const [x, y] = rest.map(Number);
    for (const type of ['mousePressed', 'mouseReleased']) await send('Input.dispatchMouseEvent', { type, x, y, button: 'left', clickCount: 1 });
    await sleep(800);
  } else if (kind === 'key') {
    const [key, code, vk] = rest;
    for (const type of ['keyDown', 'keyUp']) { await send('Input.dispatchKeyEvent', { type, key, code, windowsVirtualKeyCode: Number(vk), nativeVirtualKeyCode: Number(vk) }); await sleep(120); }
    await sleep(1500);
  } else {
    const r = await send('Runtime.evaluate', { expression: step, awaitPromise: true, returnByValue: true });
    console.log('eval:', JSON.stringify(r.result?.result?.value ?? r.result?.exceptionDetails?.exception?.description ?? r.result?.exceptionDetails?.text));
    await sleep(500);
  }
}
await sleep(Number(wait));
await shot(out);
ws.close();
