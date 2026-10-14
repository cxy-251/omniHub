// Exercise the built browser worker in a real isolated JS worker. This checks
// both filtering and termination of a pathological regex without a browser.
import fs from 'node:fs';
import path from 'node:path';
import {Worker} from 'node:worker_threads';
import {fileURLToPath} from 'node:url';
const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'..');
const folder=path.join(root,'web/dist/assets');
const file=fs.readdirSync(folder).find(name=>/^taskFilter\.worker-.*\.js$/.test(name));
if(!file)throw Error('Build web first.');
const source=fs.readFileSync(path.join(folder,file),'utf8');
const worker=new Worker(`const {parentPort}=require('node:worker_threads');global.self={postMessage:value=>parentPort.postMessage(value)};${source};parentPort.on('message',data=>self.onmessage({data}));`,{eval:true});
const call=data=>new Promise((resolve,reject)=>{
 const timeout=setTimeout(()=>reject(Error('Worker did not respond.')),2000);
 worker.once('message',result=>{clearTimeout(timeout);resolve(result);});
 worker.postMessage(data);
});
try {
 const result=await call({pattern:'error',entries:[{id:1,message:'INFO ready'},{id:2,message:'ERROR bad'}]});
 if(JSON.stringify(result.ids)!=='[2]')throw Error('Case-insensitive filtering failed.');
 if(!(await call({pattern:'[',entries:[]})).error)throw Error('Invalid regex accepted.');
 let ticks=0;
 const heartbeat=setInterval(()=>ticks++,10);
 worker.postMessage({pattern:'^(a+)+$',entries:[{id:1,message:'a'.repeat(4096)+'!'}]});
 await new Promise(resolve=>setTimeout(resolve,150));
 await worker.terminate();clearInterval(heartbeat);
 if(ticks<3)throw Error('Host loop blocked by regex.');
 console.log('TASK_FILTER_WORKER_AND_PATHOLOGICAL_REGEX_TERMINATION_OK');
} finally {await worker.terminate();}
