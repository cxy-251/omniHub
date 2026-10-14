// Run inside dbus-run-session. Real Qt/mpv client, independent bus and local
// fixtures; no user desktop service, browser, plugin, or media is touched.
import fs from 'node:fs';import path from 'node:path';import {spawn,execFile} from 'node:child_process';import {promisify} from 'node:util';
const execute=promisify(execFile), root=process.cwd(), video=path.resolve(process.argv[2]||'');
if(!process.argv[2]||!video.startsWith(root+path.sep))throw Error('Provide a fixture inside the checkout.');
const temp=fs.mkdtempSync(path.join(root,'.test-tmp','mpris-')), log=fs.openSync(path.join(temp,'client.log'),'w');
const client=spawn(path.join(root,'.build/qt-native/omnihub_client'),[video],{stdio:['ignore',log,log],env:{...process.env,OMNIHUB_CONFIG_DIR:temp,QT_QPA_PLATFORM:'offscreen',QT_QUICK_BACKEND:'rhi',QSG_RHI_BACKEND:'opengl',XDG_CACHE_HOME:path.join(temp,'cache'),MESA_SHADER_CACHE_DIR:path.join(temp,'cache','mesa')}});
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
const call=async(method,...args)=>(await execute('gdbus',['call','--session','--dest','org.mpris.MediaPlayer2.omnihub.external','--object-path','/org/mpris/MediaPlayer2','--method',method,...args],{timeout:2000})).stdout.trim();
const get=name=>call('org.freedesktop.DBus.Properties.Get','org.mpris.MediaPlayer2.Player',name);
const invoke=name=>call('org.mpris.MediaPlayer2.Player.'+name);
const expect=async(name,pattern)=>{for(let i=0;i<30;i++){if(pattern.test(await get(name)))return;await delay(100)}throw Error('Unexpected '+name+': '+await get(name));};
let monitor,signals='';
try{
 let ready=false,last='';for(let i=0;i<60;i++){try{last=await get('PlaybackStatus');if(last.includes('Playing')){ready=true;break}}catch(error){last=error.message}await delay(100)}if(!ready)throw Error('Actual player unavailable: '+last+'\n'+fs.readFileSync(path.join(temp,'client.log'),'utf8'));
 const metadata=await get('Metadata');if(!metadata.includes('Fixture title')||!metadata.includes('Fixture artist')||!metadata.includes('objectpath')||!metadata.includes('mpris:artUrl'))throw Error('Real typed metadata missing: '+metadata);
 monitor=spawn('gdbus',['monitor','--session','--dest','org.mpris.MediaPlayer2.omnihub.external','--object-path','/org/mpris/MediaPlayer2'],{stdio:['ignore','pipe','ignore']});
 monitor.stdout.on('data',data=>{signals=(signals+data.toString()).slice(-65536)});await delay(150);
 await invoke('Pause');await expect('PlaybackStatus',/Paused/);
 await invoke('PlayPause');await expect('PlaybackStatus',/Playing/);
 await invoke('PlayPause');await expect('PlaybackStatus',/Paused/);
 await call('org.freedesktop.DBus.Properties.Set','org.mpris.MediaPlayer2.Player','Volume','<0.42>');
 const volume=Number((await get('Volume')).match(/<([^>]+)>/)[1]);if(Math.abs(volume-0.42)>0.000001)throw Error('Incorrect volume: '+volume);
 await call('org.freedesktop.DBus.Properties.Set','org.mpris.MediaPlayer2.Player','Rate','<1.5>');await expect('Rate',/1\.5/);
 const track=metadata.match(/objectpath '([^']+)'/)[1];
 await call('org.mpris.MediaPlayer2.Player.SetPosition',track,'4000000');await expect('Position',/4000000/);
 await call('org.mpris.MediaPlayer2.Player.SetPosition','/org/mpris/MediaPlayer2/track/stale','0');await expect('Position',/4000000/);
 await invoke('Next');await expect('Position',/4000000/); // single-item list advertises no next
 await invoke('Stop');await expect('PlaybackStatus',/Stopped/);
 await invoke('Play');await expect('PlaybackStatus',/Playing/);
 const position=await get('Position');if(Number(position.match(/int64 (\d+)/)?.[1])>1500000)throw Error('Stop/Play did not restart: '+position);
 const rootProperties=await call('org.freedesktop.DBus.Properties.GetAll','org.mpris.MediaPlayer2');if(!rootProperties.includes('Identity')||!rootProperties.includes('HasTrackList'))throw Error('Root interface missing.');
 const introspection=(await execute('gdbus',['introspect','--session','--dest','org.mpris.MediaPlayer2.omnihub.external','--object-path','/org/mpris/MediaPlayer2'],{timeout:2000})).stdout;
 if(introspection.includes('updateMetadata')||/\bbind\(/.test(introspection))throw Error('Internal helper exported.');
 if(!signals.includes('PropertiesChanged')||!signals.includes('Seeked'))throw Error('Protocol notifications missing: '+signals);
 const exited=new Promise(resolve=>client.once('exit',resolve));await call('org.mpris.MediaPlayer2.Quit');await exited;
 fs.writeFileSync(path.join(temp,'result.json'),JSON.stringify({metadata,controls:true,stale_track_ignored:true,internal_helpers_private:true},null,2));
 console.log('MPRIS_ACTUAL_PLAYER_OK\nEvidence: '+temp);
}finally{if(client.exitCode===null&&client.signalCode===null){const exited=new Promise(resolve=>client.once('exit',resolve));client.kill('SIGTERM');await exited}if(monitor&&monitor.exitCode===null&&monitor.signalCode===null)monitor.kill('SIGTERM');fs.closeSync(log)}
