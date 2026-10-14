// Run under a private dbus-run-session; verifies real full-client routing.
import fs from 'node:fs';import path from 'node:path';import {spawn,execFile} from 'node:child_process';import {promisify} from 'node:util';
const exec=promisify(execFile),root=process.cwd(),video=path.resolve(process.argv[2]||''),second=path.resolve(process.argv[3]||'');
if(!process.argv[3]||!video.startsWith(root+path.sep)||!second.startsWith(root+path.sep))throw Error('Provide two project fixture files.');
const dir=fs.mkdtempSync(path.join(root,'.test-tmp','external-')),log=fs.openSync(path.join(dir,'client.log'),'w'),env={...process.env,OMNIHUB_CONFIG_DIR:dir,QT_QPA_PLATFORM:'offscreen',QT_QUICK_BACKEND:'rhi',QSG_RHI_BACKEND:'opengl',XDG_CACHE_HOME:path.join(dir,'cache'),MESA_SHADER_CACHE_DIR:path.join(dir,'cache','mesa')};
const binary=path.join(root,'.build/qt-native/omnihub_client');const children=[];const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
fs.writeFileSync(path.join(dir,'native-ui.json'),JSON.stringify({source:video,position:0,paused:false,page:0,entries:[video],index:0}),{mode:0o600});
const call=async(name,method,...args)=>(await exec('gdbus',['call','--session','--dest',name,'--object-path','/org/mpris/MediaPlayer2','--method',method,...args],{timeout:2000})).stdout;
const get=(name,property)=>call(name,'org.freedesktop.DBus.Properties.Get','org.mpris.MediaPlayer2.Player',property);
const owner=async name=>(await exec('gdbus',['call','--session','--dest','org.freedesktop.DBus','--object-path','/org/freedesktop/DBus','--method','org.freedesktop.DBus.GetNameOwner',name],{timeout:2000})).stdout;
try{
 const hub=spawn(binary,['--resume-ui'],{env,stdio:['ignore',log,log]});children.push(hub);
 const name='org.mpris.MediaPlayer2.omnihub.external',base='org.mpris.MediaPlayer2.omnihub';
 for(let i=0;i<50;i++){try{if((await get(base,'PlaybackStatus')).includes('Playing'))break}catch{}await delay(100)}const hubOwner=await owner(base);
 if(!(await get(base,'PlaybackStatus')).includes('Playing'))throw Error('Hub fixture did not play.');
 const hubTrack=(await get(base,'Metadata')).match(/objectpath '([^']+)'/)[1];
 const player=spawn(binary,[video],{env,stdio:['ignore',log,log]});children.push(player);
 let initial='';for(let i=0;i<60;i++){try{initial=await get(name,'Metadata');if(initial.includes('Fixture title'))break}catch{}await delay(100)}
 if(!initial.includes('Fixture title'))throw Error('External window did not load actual first fixture.');
 const playerOwner=await owner(name);await call(name,'org.mpris.MediaPlayer2.Player.Pause');
 await exec(binary,[second],{env,timeout:6000});
 let changed='';for(let i=0;i<40;i++){changed=await get(name,'Metadata');if(changed.includes(path.basename(second)))break;await delay(100)}
 if(!changed.includes(path.basename(second))||await owner(name)!==playerOwner||await owner(base)!==hubOwner||hub.exitCode!==null||player.exitCode!==null)throw Error('File routing changed owner or interfered with hub.');
 try{await exec(binary,['/nonexistent-omnihub-fixture'],{env,timeout:4000});throw Error('Invalid path accepted.')}catch(error){if(error.code!==2)throw error}
 if(await owner(name)!==playerOwner)throw Error('Rejected invocation changed window.');
 if(!(await get(base,'PlaybackStatus')).includes('Playing')||(await get(base,'Metadata')).match(/objectpath '([^']+)'/)[1]!==hubTrack)throw Error('External open paused or replaced hub playback.');
 await call(name,'org.mpris.MediaPlayer2.Quit');
 if(player.exitCode===null&&player.signalCode===null)await new Promise(resolve=>player.once('exit',resolve));
 await call(base,'org.mpris.MediaPlayer2.Quit');if(hub.exitCode===null&&hub.signalCode===null)await new Promise(resolve=>hub.once('exit',resolve));
 console.log('EXTERNAL_SINGLE_WINDOW_ROUTING_AND_HUB_ISOLATION_OK\nEvidence: '+dir);
}finally{for(const child of children)if(child.exitCode===null&&child.signalCode===null){const exited=new Promise(resolve=>child.once('exit',resolve));child.kill('SIGTERM');await exited}fs.closeSync(log)}
