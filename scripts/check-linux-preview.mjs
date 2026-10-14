// Check an actual packaged service/native client with checkout-local state.
// This check opens no browser, uses no plugin and never loads user media.
import fs from 'node:fs';
import path from 'node:path';
import {spawn, execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const bundle = path.resolve(process.argv[2] || '');
if (!process.argv[2] || !bundle.startsWith(root + path.sep)) throw Error('Pass the packaged directory inside this checkout.');
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const test = fs.mkdtempSync(path.join(root, '.test-tmp', 'release-check-'));
const config = path.join(test, 'config'); fs.mkdirSync(config);
fs.writeFileSync(path.join(config, 'config.json'), JSON.stringify({server: {host: '127.0.0.1', port: 19133}, libraries: [], category_directories: []}), {mode: 0o600});
const alias = path.join(test, 'preview with spaces'); fs.symlinkSync(bundle, alias);
const log = fs.openSync(path.join(test, 'startup.log'), 'w');
const child = spawn(path.join(alias, 'omniHub'), [], {detached: true, cwd: test, stdio: ['ignore', log, log], env: {...process.env, LD_LIBRARY_PATH: '', QT_PLUGIN_PATH: '', QML_IMPORT_PATH: '', QML2_IMPORT_PATH: '', OMNIHUB_CONFIG_DIR: config, QT_QPA_PLATFORM: 'offscreen', QT_QUICK_BACKEND: 'rhi', QSG_RHI_BACKEND: 'opengl', XDG_CACHE_HOME: path.join(test, 'cache'), MESA_SHADER_CACHE_DIR: path.join(test, 'cache', 'mesa'), DBUS_SESSION_BUS_ADDRESS: `unix:path=${test}/no-session-bus`}});
let passed = false;
try {
 let connection;
 for (let attempt = 0; attempt < 100; attempt++) {
  if (child.exitCode !== null) throw Error(`Packaged service exited ${child.exitCode}`);
  try { connection = JSON.parse(fs.readFileSync(path.join(config, 'local-client.json'), 'utf8')); if ((await fetch(connection.url + '/api/auth/status', {headers: {'X-Omni-Local-Token': connection.token}})).ok) break; connection = undefined; } catch { connection = undefined; }
  await delay(100);
 }
 if (!connection) throw Error('Packaged service did not become ready.');
 if ((fs.statSync(path.join(config, 'local-client.json')).mode & 0o077) !== 0) throw Error('Local credential permissions are not private.');
 const headers = {'X-Omni-Local-Token': connection.token};
 const htmlResponse = await fetch(connection.url + '/', {headers}); const html = await htmlResponse.text();
 if (!htmlResponse.ok || !html.includes('id="root"')) throw Error('Embedded React index missing.');
 for (const match of html.matchAll(/(?:src|href)="(\/assets\/[^\"]+)"/g)) if (!(await fetch(connection.url + match[1], {headers})).ok) throw Error(`Embedded asset unavailable: ${match[1]}`);
 const status = await fetch(connection.url + '/api/auth/status', {headers: {'X-Omni-Local-Token': connection.token}});
 if (!status.ok || (await status.json()).has_admin !== false) throw Error('Fresh account setup unavailable.');
 await delay(1500);
 const descendants = [...new Set(fs.readdirSync(`/proc/${child.pid}/task`).flatMap(thread => {
  try { return fs.readFileSync(`/proc/${child.pid}/task/${thread}/children`, 'utf8').trim().split(/\s+/).filter(Boolean); } catch (error) { if (error.code === 'ENOENT') return []; throw error; }
 }))];
 if (descendants.length !== 1) throw Error(`Expected a live Qt child, found ${descendants.length}`);
 const gui = descendants[0], maps = fs.readFileSync(`/proc/${gui}/maps`, 'utf8');
 if (!maps.includes('libQt6Quick.so') || !maps.includes('libmpv.so')) throw Error('Native Qt/mpv runtime missing.');
 const sdkMaps = maps.split('\n').filter(line => /\/Applications\/qt6\/|\/\.build\/qt-deps\//.test(line));
 if (sdkMaps.length) throw Error(`Runtime escaped bundled dependencies:\n${sdkMaps.join('\n')}`);
 const output = fs.readFileSync(path.join(test, 'startup.log'), 'utf8');
 if (/failed to load component|module .+ is not installed|Cannot load library|symbol lookup error/i.test(output)) throw Error(`Native UI did not load:\n${output}`);
 const setup = await fetch(connection.url + '/api/auth/setup', {method: 'POST', headers: {'Content-Type': 'application/json', 'X-Omni-Local-Token': connection.token}, body: JSON.stringify({username: 'preview-owner', password: 'preview-only-password'})});
 if (!setup.ok) throw Error(`Initial account setup failed: ${setup.status} ${await setup.text()}`);
 const after = await fetch(connection.url + '/api/auth/status', {headers: {'X-Omni-Local-Token': connection.token}});
 if (!(await after.json()).has_admin) throw Error('Initial account did not persist.');
 const libraries = await fetch(connection.url + '/api/library/list', {headers: {'X-Omni-Local-Token': connection.token}});
 if (!libraries.ok || (await libraries.json()).libraries.length !== 0) throw Error('Zero-library state is not usable.');
 const version = execFileSync(path.join(bundle, 'server'), ['--version'], {encoding: 'utf8'}).trim();
 if (!version.startsWith('omniHub ')) throw Error('Build identity unavailable.');
 fs.writeFileSync(path.join(test, 'result.json'), JSON.stringify({version, url: connection.url, native_ui: 'Qt Main.qml alive', sdk_fallback: false, isolated_configuration: true, initialized_account: true, zero_libraries: true}, null, 2));
 passed = true;
} finally {
 // Signal only this live, unreaped child-owned test group. No saved PID is used
 // after exit; the packaged GUI never starts a game in this smoke check.
 if (child.exitCode === null && child.signalCode === null) process.kill(-child.pid, 'SIGTERM');
 if (child.exitCode === null && child.signalCode === null) await new Promise(resolve => child.once('exit', resolve));
 fs.closeSync(log);
 const output = fs.readFileSync(path.join(test, 'startup.log'), 'utf8');
 if (passed && !output.includes('server exited gracefully')) throw Error(`Service did not close cleanly:\n${output}`);
 if (passed) console.log(`LINUX_PREVIEW_OK\nEvidence: ${test}`); else console.error(output);
}
