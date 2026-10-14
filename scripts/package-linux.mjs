// Build a self-contained Linux preview from existing Qt/mpv SDKs. No downloads,
// installations or writes outside this checkout are performed by this script.
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
if (process.platform !== 'linux') throw Error('This builder targets Linux; server builds support other Go targets.');
const qtRuntime = process.env.QT_RUNTIME_DIR;
if (!qtRuntime || !fs.statSync(qtRuntime).isDirectory()) throw Error('Set QT_RUNTIME_DIR to the SDK directory containing qml/ and plugins/.');
const build = path.resolve(root, process.env.OMNIHUB_CLIENT_BUILD_DIR || '.build/qt-native');
if (!build.startsWith(root + path.sep)) throw Error('Client build directory must be inside this checkout.');
const run = (program, args, extra = {}) => execFileSync(program, args, {cwd: root, stdio: 'inherit', ...extra});
const capture = (program, args, extra = {}) => execFileSync(program, args, {cwd: root, encoding: 'utf8', ...extra}).trim();
const revision = capture('git', ['rev-parse', '--short=12', 'HEAD']) + (capture('git', ['status', '--porcelain']) ? '-dirty' : '');
const time = new Date().toISOString().replace(/\.\d{3}Z$/, 'Z');
const name = `omniHub-linux-${process.arch}-${revision}-${time.replaceAll(':', '')}`;
const directory = path.join(root, '.build', 'releases', name);
fs.mkdirSync(path.dirname(directory), {recursive: true});
fs.mkdirSync(directory, {recursive: false});
const bin = path.join(directory, 'bin'), lib = path.join(directory, 'lib');
fs.mkdirSync(bin); fs.mkdirSync(lib);
run(process.env.PNPM || 'pnpm', ['--dir', 'web', 'build']);
run(process.env.CMAKE || 'cmake', ['--build', build, '--parallel', '2', '--target', 'omnihub_client']);
run('go', ['build', '-trimpath', '-ldflags', `-s -w -X omnihub/internal/buildinfo.Revision=${revision} -X omnihub/internal/buildinfo.BuildTime=${time}`, '-o', path.join(bin, 'omnihub-server'), './cmd/server']);
fs.copyFileSync(path.join(build, 'omnihub_client'), path.join(bin, 'omnihub_client'));
fs.chmodSync(path.join(bin, 'omnihub_client'), 0o755);
for (const module of ['QtQml', 'QtQuick']) fs.cpSync(path.join(qtRuntime, 'qml', module), path.join(directory, 'qml', module), {recursive: true, dereference: true});
for (const plugin of ['platforms', 'imageformats', 'iconengines', 'platforminputcontexts', 'platformthemes', 'egldeviceintegrations', 'xcbglintegrations', 'tls', 'networkinformation']) {
 const source = path.join(qtRuntime, 'plugins', plugin);
 if (fs.existsSync(source)) fs.cpSync(source, path.join(directory, 'plugins', plugin), {recursive: true, dereference: true});
}
const files = folder => fs.readdirSync(folder, {withFileTypes: true}).flatMap(item => item.isDirectory() ? files(path.join(folder, item.name)) : [path.join(folder, item.name)]);
// libc/loader and graphics dispatch/driver libraries belong to the target OS.
// Bundling them would conflict with the user's Mesa/NVIDIA stack.
const hostLibrary = /^(?:lib(?:c|m|pthread|dl|rt|resolv|util|anl)\.so|ld-linux|lib(?:GL|GLX|EGL|OpenGL|GLdispatch|gbm|drm|vulkan)(?:[._-]|\.so))/;
const copied = new Map(), host = new Set();
for (const file of [path.join(bin, 'omnihub_client'), ...files(path.join(directory, 'qml')), ...files(path.join(directory, 'plugins'))]) {
 if (!fs.readFileSync(file).subarray(0, 4).equals(Buffer.from([0x7f, 0x45, 0x4c, 0x46]))) continue;
 const dependencies = capture('ldd', [file]);
 if (/=> not found/.test(dependencies)) throw Error(`Missing build dependency for ${file}:\n${dependencies}`);
 for (const line of dependencies.split('\n')) {
  const match = line.match(/^\s*(\S+)\s+=>\s+(.+?)\s+\(0x/);
  if (!match) continue;
  const soname = path.basename(match[1]), source = match[2];
  if (hostLibrary.test(soname)) { host.add(soname); continue; }
  const real = fs.realpathSync(source);
  if (copied.has(soname)) { if (copied.get(soname) !== real) throw Error(`Conflicting dependency ${soname}`); continue; }
  fs.copyFileSync(real, path.join(lib, soname)); copied.set(soname, real);
 }
}
const wrapper = mode => `#!/bin/sh\nset -eu\nrelease_dir=$(CDPATH= cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)\nexport LD_LIBRARY_PATH="$release_dir/lib\${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"\nexport QT_PLUGIN_PATH="$release_dir/plugins"\nexport QML_IMPORT_PATH="$release_dir/qml"\nexport QML2_IMPORT_PATH="$release_dir/qml"\nexec "$release_dir/bin/${mode === 'client' ? 'omnihub_client' : 'omnihub-server'}"${mode === 'desktop' ? ' --desktop' : ''} "$@"\n`;
for (const [entry, mode] of [['omniHub', 'desktop'], ['server', 'server'], ['client', 'client'], ['omnihub-client', 'client']]) fs.writeFileSync(path.join(directory, entry), wrapper(mode), {mode: 0o755});
fs.copyFileSync(path.join(root,'client','desktop','omnihub.desktop'),path.join(directory,'omnihub.desktop'));
fs.writeFileSync(path.join(bin, 'qt.conf'), '[Paths]\nPrefix=..\nLibraries=lib\nPlugins=plugins\nQmlImports=qml\n');
fs.copyFileSync(path.join(root, 'RUNNING.md'), path.join(directory, 'README.md'));
fs.copyFileSync(path.join(root, 'DELIVERY_STATUS.md'), path.join(directory, 'DELIVERY_STATUS.md'));
const licenseSources = [path.join(qtRuntime, '..', '..', 'share', 'licenses'), path.join(root, '.build', 'qt-deps', 'usr', 'share', 'licenses')];
const copyLicenses = (source, destination) => {
 fs.mkdirSync(destination, {recursive: true});
 const aliases = {};
 for (const entry of fs.readdirSync(source, {withFileTypes: true})) {
  const from = path.join(source, entry.name), to = path.join(destination, entry.name);
  // SDK license aliases may be absolute /usr links. Retain their names as
  // metadata; copy real license folders instead of shipping links outside the bundle.
  if (entry.isSymbolicLink()) { aliases[entry.name] = fs.readlinkSync(from); continue; }
  if (entry.isDirectory()) copyLicenses(from, to); else fs.copyFileSync(from, to);
 }
 if (Object.keys(aliases).length) fs.writeFileSync(path.join(destination, 'ALIASES.json'), JSON.stringify(aliases, null, 2) + '\n');
};
for (const [index, source] of licenseSources.entries()) if (fs.existsSync(source)) copyLicenses(source, path.join(directory, 'licenses', `sdk-${index}`));
const common = path.join(path.parse(root).root, 'usr', 'share', 'licenses', 'common');
if (fs.existsSync(common)) copyLicenses(common, path.join(directory, 'licenses', 'system-common'));
for (const runtime of ['ruffle', 'emulatorjs']) {
 const source = path.join(root, 'web', 'vendor', 'players', runtime);
 for (const file of files(source).filter(file => /(?:LICENSE[^/]*|NOTICE\.md|(?:UPSTREAM_FILES|LOCAL_CHANGES)\.json)$/.test(file))) {
  const destination = path.join(directory, 'licenses', 'web', runtime, path.relative(source, file));
  fs.mkdirSync(path.dirname(destination), {recursive: true}); fs.copyFileSync(file, destination);
 }
}
const manifest = {revision, build_time: time, platform: process.platform, architecture: process.arch, status: 'development preview', host_libraries: [...host].sort(), copied_libraries: Object.fromEntries([...copied].sort()), files: {}};
for (const file of files(directory)) manifest.files[path.relative(directory, file)] = crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
fs.writeFileSync(path.join(directory, 'MANIFEST.json'), JSON.stringify(manifest, null, 2) + '\n');
run('tar', ['-czf', directory + '.tar.gz', '-C', path.dirname(directory), name]);
console.log(`Linux preview: ${directory}\nArchive: ${directory}.tar.gz`);
