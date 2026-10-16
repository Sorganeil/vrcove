import {test,expect} from '@playwright/test';

const id='usr_00000000-0000-0000-0000-000000000001';
const friendId='usr_00000000-0000-0000-0000-000000000002';
const friends=[
 {id:'active',displayName:'Active Friend',status:'active',statusDescription:'Exploring worlds'},
 {id:'join',displayName:'Join Friend',status:'join me',statusDescription:'Come hang out'},
 {id:'ask',displayName:'Ask Friend',status:'ask me',statusDescription:'Send a request'},
 {id:'busy',displayName:'Busy Friend',status:'busy',statusDescription:'Working on an avatar'},
 {id:'unknown',displayName:'Unknown Friend'},
];

test.beforeEach(async({page})=>{
 await page.route('**/api/veil/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:id}]}}));
 await page.route('**/api/account/me',r=>r.fulfill({json:{id,displayName:'Tester',status:'active'}}));
 await page.route('**/api/account/friends*',r=>r.fulfill({json:new URL(r.request().url()).searchParams.get('offline')==='true'?[{id:friendId,displayName:'Offline Friend',status:'busy',statusDescription:'Back tomorrow',location:'offline'}]:friends}));
 await page.route('**/api/account/worlds',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/notifications',r=>r.fulfill({json:[]}));
 await page.route('**/api/catalog?*',r=>r.fulfill({json:{avatars:[],total:0}}));
});

test('friend availability and full messages are visible together with distinct colors',async({page})=>{
 await page.goto('/#friends');
 const labels=['Online','Join me','Ask me','Do not disturb','Status unavailable'];
 const colors=[];
 for(let i=0;i<friends.length;i++){
  const card=page.locator('.friend-card').filter({hasText:friends[i].displayName});
  await expect(card.locator('.user-availability')).toHaveText(labels[i]);
  if(friends[i].statusDescription)await expect(card.locator('.user-status-message')).toHaveText(friends[i].statusDescription!);
  colors.push(await card.locator('.presence-dot').evaluate(dot=>getComputedStyle(dot).backgroundColor));
 }
 expect(new Set(colors).size).toBe(5);
 await page.getByRole('button',{name:'Offline',exact:true}).click();
 await expect(page.locator('.friend-card .user-availability')).toHaveText('Offline');
 await expect(page.locator('.friend-card .user-status-message')).toHaveText('Back tomorrow');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
});

test('offline profiles retain their status message',async({page},testInfo)=>{
 await page.route('**/api/account/user?*',r=>r.fulfill({json:{id:friendId,displayName:'Offline Friend',state:'offline',status:'join me',statusDescription:"32 characters isn't enough to sa"}}));
 await page.route('**/api/account/user-worlds?*',r=>r.fulfill({json:[]}));
 await page.route('**/api/account/user-groups?*',r=>r.fulfill({json:[]}));
 await page.goto('/#user/'+friendId);
 await expect(page.locator('.entity-hero-copy .user-availability')).toHaveText('Offline');
 await expect(page.locator('.entity-hero-copy .user-status-message')).toHaveText("32 characters isn't enough to sa");
 const badge=await page.locator('.entity-hero-copy .user-availability').boundingBox();
 const message=await page.locator('.entity-hero-copy .user-status-message').boundingBox();
 expect(message!.y).toBeGreaterThan(badge!.y+badge!.height);
 await page.screenshot({path:testInfo.outputPath('status-layout.png'),fullPage:true});
});

test('home friend previews show availability alongside messages',async({page})=>{
 await page.goto('/#home');
 const friend=page.locator('.online-grid>a').filter({hasText:'Join Friend'});
 await expect(friend.locator('.user-availability')).toHaveText('Join me');
 await expect(friend.locator('.user-status-message')).toHaveText('Come hang out');
 await expect(friend.locator('.status-dot')).toHaveClass('status-dot join-me');
});
