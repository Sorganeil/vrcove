import assert from 'node:assert/strict';
// Smoke-test a running deployment: node scripts/verify-site.mjs https://vrcove.example
const base=(process.argv[2]||'').replace(/\/$/,'');
if(!/^https?:\/\//.test(base)){console.error('Usage: node scripts/verify-site.mjs <site origin>');process.exit(2);}
const req=(p,opts={})=>fetch(base+p,{...opts,signal:AbortSignal.timeout(30000),redirect:'error'});
let r=await req('/api/stats');assert.equal(r.status,200);const stats=await r.json();assert.equal(stats.indexed,1492676);assert.equal(stats.loginConfigured,true);console.log(JSON.stringify({fullCatalog:stats.indexed,loginConfigured:stats.loginConfigured}));
r=await req('/api/catalog?q=manuka&platform=both');assert.equal(r.status,200);const results=await r.json();assert.ok(results.total>0);assert.ok(results.avatars.every(a=>a.pc&&a.quest));assert.ok(results.avatars.every(a=>!a.updated||Number.isFinite(Date.parse(a.updated))));console.log(JSON.stringify({searchMatches:results.total,platformFilter:true}));
r=await req('/api/image?url='+encodeURIComponent('https://api.vrchat.cloud/api/1/image/file_0c853d9f-fb1d-4e38-8d35-57fe94949269/1/256'));assert.equal(r.status,200);assert.match(r.headers.get('content-type'),/^image\/(png|jpeg|webp|gif|avif)/);console.log(JSON.stringify({imageProxy:true}));
r=await req('/api/image?url='+encodeURIComponent('http://127.0.0.1/'));assert.equal(r.status,404);
r=await req('/api/account/me');assert.equal(r.status,401);
r=await req('/api/account/select',{method:'POST',headers:{'Content-Type':'application/json',Origin:'https://untrusted.example'},body:'{}'});assert.equal(r.status,403);
console.log(JSON.stringify({unauthenticatedActionsBlocked:true,crossOriginActionsBlocked:true,unsafeImageHostBlocked:true}));

