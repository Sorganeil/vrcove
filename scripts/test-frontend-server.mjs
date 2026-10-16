import {spawn,execFileSync} from 'node:child_process';
import {randomBytes} from 'node:crypto';
import {mkdtempSync,rmSync,writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

// Tests never open the deployment's databases or reuse its secret keys.
const frontendPort=process.env.VRCOVE_TEST_FRONTEND_PORT||'15173';
const backendPort=process.env.VRCOVE_TEST_BACKEND_PORT||'18080';
const dir=mkdtempSync(join(tmpdir(),'vrcove-ui-'));
execFileSync('go',['build','-o',join(dir,'vrcove'),'./cmd/vrcove'],{stdio:'inherit'});
const accountKey=randomBytes(32).toString('base64');
const fixtureFile=join(tmpdir(),`vrcove-ui-fixture-${frontendPort}.json`);
writeFileSync(fixtureFile,JSON.stringify({database:join(dir,'accounts.sqlite'),accountKey}),{mode:0o600});
const backend=spawn(join(dir,'vrcove'),[],{stdio:'inherit',env:{...process.env,
 PUBLIC_ORIGIN:`http://localhost:${frontendPort}`,LISTEN_ADDR:`127.0.0.1:${backendPort}`,TUNNEL_ONLY:'false',
 DATABASE_PATH:join(dir,'catalog.sqlite'),ACCOUNTS_PATH:join(dir,'accounts.sqlite'),
 ACCOUNT_KEY:accountKey,SESSION_KEY:'',VRC_CONTACT:'',
}});
const frontend=spawn(process.execPath,['node_modules/vite/bin/vite.js','--host','localhost','--port',frontendPort,'--strictPort'],{stdio:'inherit',env:{...process.env,VRCOVE_API_TARGET:`http://127.0.0.1:${backendPort}`}});
let stopping=false;
function stop(){if(stopping)return;stopping=true;backend.kill('SIGTERM');frontend.kill('SIGTERM');}
process.on('SIGINT',()=>stop());process.on('SIGTERM',()=>stop());
backend.on('exit',code=>{stop();process.exitCode=code||0;});
frontend.on('exit',code=>{stop();process.exitCode=code||0;});
process.on('exit',()=>{rmSync(dir,{recursive:true,force:true});rmSync(fixtureFile,{force:true});});
