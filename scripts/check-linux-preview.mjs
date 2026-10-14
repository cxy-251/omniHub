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
fs.writeFileSync(path.join(config, 'config.json'), JSON.stringify({server: {host: '127.0.0.1', port: 19133}, libraries: [], category_directories: ["media_library/docs","media_library/manga"]}), {mode: 0o600});
const probe = {id:'delivery.verify',name:'Delivery task',category:'media_library/docs',write:false,executable:process.execPath,args:['-e',"if(process.cwd()!==process.env.OMNIHUB_TASK_DIR||process.env.DELIVERY_PROBE!=='private-probe')process.exit(2);console.log('INFO packaged task reached actual worker')"],parameters:[],env:{DELIVERY_PROBE:'private-probe'}};
fs.writeFileSync(path.join(config,'tasks.json'),JSON.stringify([probe]),{mode:0o600});
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
 const storage=path.join(test,'library');fs.mkdirSync(storage);
 const mounted=await fetch(connection.url+'/api/library/add',{method:'POST',headers:{...headers,'Content-Type':'application/json'},body:JSON.stringify({path:storage,read_only:false})});
 if(!mounted.ok)throw Error('Isolated task library could not be mounted.');
 const snapshot=await (await fetch(connection.url+'/api/library/list',{headers})).json();
 const started=await fetch(connection.url+'/api/tasks/run',{method:'POST',headers:{...headers,'Content-Type':'application/json'},body:JSON.stringify({definition:'delivery.verify',library_id:snapshot.libraries[0].id,directory:'',parameters:{}})});
 if(!started.ok)throw Error('Registered task did not start: '+await started.text());
 const job=await started.json();let finished=false;
 for(let i=0;i<50;i++){
  const response=await fetch(connection.url+'/api/tasks/catalog',{headers});const raw=await response.text();
  if(!response.ok||raw.includes('private-probe')||raw.includes(process.execPath))throw Error('Task catalog failed or exposed private configuration.');
  const task=JSON.parse(raw).tasks.find(task=>task.id===job.id);
  if(task&&!task.running){if(task.exit_code!==0)throw Error('Actual task failed.');finished=true;break;}
  await delay(50);
 }
 if(!finished)throw Error('Actual registered task did not finish.');
 const events=await (await fetch(connection.url+'/api/tasks/logs?id='+encodeURIComponent(job.id),{headers})).text();
 if(!events.includes('packaged task reached actual worker')||!events.includes('event: end'))throw Error('Actual task SSE history/end missing.');
 if(process.env.OMNIHUB_PACK_PREVIEW_FIXTURE){
  const fixture=path.resolve(process.env.OMNIHUB_PACK_PREVIEW_FIXTURE);if(!fixture.startsWith(root+path.sep))throw Error('Pack fixture must be inside checkout.');
  const bookDir=path.join(storage,'media_library/manga/Pack fixture');fs.mkdirSync(bookDir,{recursive:true});
  const jpeg=fs.readFileSync(fixture);fs.writeFileSync(path.join(bookDir,'cover.jpg'),jpeg);fs.writeFileSync(path.join(bookDir,'2.jpg'),jpeg);fs.writeFileSync(path.join(bookDir,'10.jpg'),jpeg);
  const packed=await fetch(connection.url+'/api/tasks/run',{method:'POST',headers:{...headers,'Content-Type':'application/json'},body:JSON.stringify({definition:'manga.pack',library_id:snapshot.libraries[0].id,directory:'Pack fixture',parameters:{output:'bundle.cbz'}})});
  if(!packed.ok)throw Error('Builtin pack task unavailable: '+await packed.text());
  const packJob=await packed.json();let complete=false;
  for(let i=0;i<200;i++){
   const state=await (await fetch(connection.url+'/api/tasks/catalog',{headers})).json();const job=state.tasks.find(job=>job.id===packJob.id);
   if(job&&!job.running){if(job.exit_code!==0){const errorLog=await(await fetch(connection.url+'/api/tasks/logs?id='+encodeURIComponent(job.id),{headers})).text();throw Error('Builtin pack failed: '+errorLog);}complete=true;break;}
   await delay(50);
  }
  if(!complete)throw Error('Builtin pack did not finish.');
  const books=await(await fetch(connection.url+'/api/media/manga/list?library_id='+encodeURIComponent(snapshot.libraries[0].id),{headers})).json();
  const book=books.find(book=>book.logical_path.endsWith('/Pack fixture/bundle.cbz'));if(!book)throw Error('Packed book not visible in actual API.');
  const pages=await(await fetch(connection.url+'/api/media/manga/page-list?path='+encodeURIComponent(book.logical_path),{headers})).json();
  if(pages.total!==2||pages.pages[0].entry!=='2.jpg'||pages.pages[1].entry!=='10.jpg')throw Error('Packed book reading order failed: '+JSON.stringify(pages));
  const cover=await fetch(connection.url+'/api/media/manga/cover?path='+encodeURIComponent(book.logical_path),{headers});if(!cover.ok||!Buffer.from(await cover.arrayBuffer()).equals(jpeg))throw Error('Packed cover unavailable.');
  console.log('PACK_TASK_PUBLISH_INDEX_BOOK_API_AND_COVER_OK');
 }
 const version = execFileSync(path.join(bundle, 'server'), ['--version'], {encoding: 'utf8'}).trim();
 if (!version.startsWith('omniHub ')) throw Error('Build identity unavailable.');
 fs.writeFileSync(path.join(test, 'result.json'), JSON.stringify({version, url: connection.url, native_ui: 'Qt Main.qml alive', sdk_fallback: false, isolated_configuration: true, initialized_account: true, zero_libraries: true, registered_task: 'actual worker and SSE passed'}, null, 2));
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
