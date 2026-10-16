import {test,expect,type Page} from '@playwright/test';

const user={id:'usr_00000000-0000-0000-0000-000000000001',displayName:'VRChat Handle',status:'active'};
const me={username:'cove_identity',created:'2026-10-15',bio:'',public:false,picture:'',links:[{id:1,vrchatId:user.id,displayName:user.displayName,username:'synthetic',linked:'2026-10-15'}]};
async function setup(page:Page){
 await page.route('**/api/account/user?*',r=>r.fulfill({json:user}));
 await page.route('**/api/account/user-worlds?*',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/user-groups?*',r=>r.fulfill({json:[]}));
 await page.route('**/api/stats',r=>r.fulfill({json:{accountsConfigured:true,loginConfigured:true,indexed:0}}));
 await page.route('**/api/cove/me',r=>r.fulfill({json:me}));
 await page.route('**/api/catalog?*',r=>r.fulfill({json:{avatars:[],total:0}}));
 await page.route('**/api/account/friends?*',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/friends',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/worlds',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/notifications',r=>r.fulfill({json:[]}));
}
const banner=(page:Page)=>page.locator('.vrchat-session-notice');

test('a missing linked session is explained on reload and reconnect preserves VRCove identity',async({page})=>{
 await setup(page);
 let connected=false;
 const expires=Date.now()+3_600_000;
 await page.route('**/api/account/me',r=>connected?r.fulfill({headers:{'X-VRChat-Session-Expires':String(expires)},json:user}):r.fulfill({status:401,json:{code:'vrchat_session_required',error:'Your linked VRChat account is disconnected or its session has expired.'}}));
 await page.route('**/api/account/login',r=>{connected=true;return r.fulfill({headers:{'X-VRChat-Session-Expires':String(expires)},json:{user}});});
 await page.goto('/#account');
 await expect(banner(page)).toContainText('Reconnect your linked VRChat account');
 await expect(banner(page)).toContainText('still signed in to VRCove');
 await expect(page.locator('.account-chip')).toHaveText(me.username);
 await banner(page).getByRole('button',{name:'Reconnect VRChat'}).click();
 const dialog=page.getByRole('dialog',{name:'Connect VRChat'});
 await expect(dialog.getByLabel('VRChat username')).toHaveValue('synthetic');
 await dialog.getByLabel('Password',{exact:true}).fill('synthetic password');
 await dialog.getByRole('button',{name:'Connect to VRChat',exact:true}).click();
 await expect(dialog).toHaveCount(0);
 await expect(banner(page)).toHaveCount(0);
 await expect(page.getByText('VRChat session expires',{exact:false})).toBeVisible();
 await expect(page.locator('.account-chip')).toHaveText(me.username);
});

test('session expiry is announced without waiting for an action and closes stale action dialogs',async({page},testInfo)=>{
 const now=Date.now(),expires=now+30_000;
 await page.clock.install({time:new Date(now)});
 await setup(page);
 await page.route('**/api/account/me',r=>r.fulfill({headers:{'X-VRChat-Session-Expires':String(expires)},json:user}));
 await page.goto('/#profile');
 await page.getByRole('button',{name:'Edit profile',exact:true}).click();
 await expect(page.getByRole('dialog',{name:'Edit profile'})).toBeVisible();
 await page.clock.fastForward(31_000);
 await expect(banner(page)).toContainText('VRChat session expired');
 await expect(page.getByRole('dialog')).toHaveCount(0);
 await expect(page.locator('.account-chip')).toHaveText(me.username);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
 await page.evaluate(()=>window.scrollTo(0,0));
 const notice=await banner(page).boundingBox();
 const topbar=await page.locator('.topbar').boundingBox();
 expect(notice!.y).toBeGreaterThanOrEqual(topbar!.y+topbar!.height);
 await page.screenshot({path:testInfo.outputPath('session-expired.png'),fullPage:true});
});

test('authentication failures in VRChat panels invalidate the connection but permission errors do not',async({page})=>{
 await setup(page);
 let invalid=false;
 await page.route('**/api/account/me',r=>invalid?r.fulfill({status:401,json:{code:'vrchat_session_invalid',error:'VRChat session is no longer valid.'}}):r.fulfill({json:user}));
 await page.route('**/api/account/friends?*',r=>new URL(r.request().url()).searchParams.get('offline')==='true'?(invalid=true,r.fulfill({status:401,json:{code:'vrchat_session_invalid',error:'VRChat session is no longer valid.'}})):r.fulfill({status:403,json:{error:'VRChat denied this action.'}}));
 await page.goto('/#friends');
 await expect(page.locator('.error-banner')).toContainText('VRChat denied this action.');
 await expect(banner(page)).toHaveCount(0);
 await page.getByRole('button',{name:'Offline',exact:true}).click();
 await expect(banner(page)).toContainText('VRChat session is no longer valid');
 await expect(page.locator('.account-chip')).toHaveText(me.username);
 await expect(page.getByRole('heading',{name:'Reconnect your VRChat account'})).toBeVisible();
});

test('returning to the tab detects a revoked session without mislabeling temporary failures',async({page})=>{
 await setup(page);
 let state:'valid'|'unavailable'|'invalid'='valid';
 await page.route('**/api/account/me',r=>state==='valid'?r.fulfill({json:user}):state==='unavailable'?r.fulfill({status:503,json:{error:'Temporarily unavailable'}}):r.fulfill({status:401,json:{code:'vrchat_session_invalid',error:'Session revoked'}}));
 await page.goto('/#profile');
 await expect(page.getByRole('button',{name:'Edit profile',exact:true})).toBeVisible();
 state='unavailable';
 const failed=page.waitForResponse(r=>r.url().endsWith('/api/account/me')&&r.status()===503);
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await failed;
 await expect(banner(page)).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Edit profile',exact:true})).toBeVisible();
 state='invalid';
 await page.evaluate(()=>window.dispatchEvent(new Event('focus')));
 await expect(banner(page)).toContainText('VRChat session is no longer valid');
 await expect(page.locator('.account-chip')).toHaveText(me.username);
});

test('visible pages periodically detect revocation without an account action',async({page})=>{
 await page.clock.install();
 await setup(page);
 let revoked=false;
 await page.route('**/api/account/me',r=>revoked?r.fulfill({status:401,json:{code:'vrchat_session_invalid',error:'Session revoked'}}):r.fulfill({json:user}));
 await page.goto('/#profile');
 await expect(page.getByRole('button',{name:'Edit profile',exact:true})).toBeVisible();
 revoked=true;
 await page.clock.fastForward(60_001);
 await expect(banner(page)).toContainText('VRChat session is no longer valid');
 await expect(page.locator('.account-chip')).toHaveText(me.username);
});
