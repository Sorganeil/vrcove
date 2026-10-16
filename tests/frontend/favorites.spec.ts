import {test,expect} from '@playwright/test';

const world='wrld_00000000-0000-0000-0000-000000000003';
const missing='wrld_00000000-0000-0000-0000-000000000004';
const avatar='avtr_00000000-0000-0000-0000-000000000005';

test('favorites switches between avatars and paginated worlds, including unavailable worlds',async({page})=>{
 await page.route('**/api/cove/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:'usr_test'}]}}));
 await page.route('**/api/account/me',r=>r.fulfill({json:{id:'usr_test',displayName:'Tester'}}));
 await page.route('**/api/catalog?*',r=>r.fulfill({json:{avatars:[{id:avatar,name:'Saved avatar',author:'Creator',rating:'Good',tags:[]}],total:1}}));
 await page.route('**/api/account/favorites?*',r=>{
  const q=new URL(r.request().url()).searchParams;
  return r.fulfill({json:q.get('type')==='world'?(q.get('offset')==='100'?[]:Array.from({length:100},(_,i)=>({id:`fvrt_${i}`,favoriteId:i===0?missing:world}))):[{id:'fvrt_avatar',favoriteId:avatar}]});
 });
 await page.route('**/api/account/world?*',r=>new URL(r.request().url()).searchParams.get('id')===missing?r.fulfill({status:404,json:{error:'Unavailable'}}):r.fulfill({json:{id:world,name:'Saved world',authorName:'World creator'}}));
 await page.goto('/#favorites');
 await expect(page.locator('.avatar-card')).toContainText('Saved avatar');
 const tabs=page.getByLabel('Favorite type');
 await tabs.getByRole('button',{name:'Worlds',exact:true}).click();
 await expect(page.locator('.avatar-card')).toHaveCount(0);
 await expect(page.locator('.world-card')).toHaveCount(100);
 await expect(page.locator('.world-card').first()).toContainText('Unavailable world');
 await expect(page.locator('.world-card').nth(1)).toHaveAttribute('href','/world/'+world);
 await page.getByRole('button',{name:'Next',exact:true}).click();
 await expect(page.getByRole('heading',{name:'No favorite worlds found'})).toBeVisible();
 await tabs.getByRole('button',{name:'Avatars',exact:true}).click();
 await expect(page.locator('.avatar-card')).toContainText('Saved avatar');
 await tabs.getByRole('button',{name:'Worlds',exact:true}).click();
 await expect(page.locator('.world-card')).toHaveCount(100);
});

test('avatars already in VRChat favorites cannot be favorited again',async({page})=>{
 const other='avtr_00000000-0000-0000-0000-000000000006';
 await page.route('**/api/cove/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:'usr_test'}]}}));
 await page.route('**/api/account/me',r=>r.fulfill({json:{id:'usr_test',displayName:'Tester'}}));
 await page.route('**/api/catalog?*',r=>r.fulfill({json:{avatars:[{id:avatar,name:'Saved avatar',author:'Creator',rating:'Good',tags:[],pc:true},{id:other,name:'New avatar',author:'Creator',rating:'Good',tags:[],pc:true}],total:2}}));
 await page.route('**/api/account/favorites?*',r=>r.fulfill({json:[{id:'fvrt_avatar',favoriteId:avatar}]}));
 await page.route('**/api/account/favorite-groups',r=>r.fulfill({json:[{name:'avatars1',displayName:'Avatars'}]}));
 let added=false,removed='';
 await page.route('**/api/account/favorites',r=>{added=true;return r.fulfill({json:{id:'fvrt_new',favoriteId:other}});});
 await page.route('**/api/account/unfavorite',r=>{removed=r.request().postDataJSON().favoriteId;return r.fulfill({json:{}});});
 await page.goto('/#avatars');
 await page.locator('.avatar-card').filter({hasText:'Saved avatar'}).click();
 const dialog=page.getByRole('dialog').first();
 await expect(dialog.getByRole('button',{name:'Favorited in VRChat'})).toBeDisabled();
 await expect(dialog.getByRole('button',{name:'Favorite in VRChat',exact:true})).toHaveCount(0);
 await dialog.getByRole('button',{name:'Remove VRChat favorite'}).click();
 await expect.poll(()=>removed).toBe('fvrt_avatar');
 await page.locator('.avatar-card').filter({hasText:'New avatar'}).click();
 await expect(dialog.getByRole('button',{name:'Remove VRChat favorite'})).toHaveCount(0);
 await dialog.getByRole('button',{name:'Favorite in VRChat',exact:true}).click();
 await page.getByRole('dialog',{name:'Favorite avatar'}).getByRole('button',{name:'Add favorite'}).click();
 await expect.poll(()=>added).toBe(true);
 await expect(dialog.getByRole('button',{name:'Favorited in VRChat'})).toBeDisabled();
 expect(added).toBe(true);
});

test('switching into favorites never draws leftover rows as avatars',async({page})=>{
 const errors:string[]=[];
 page.on('pageerror',e=>errors.push(e.message));
 await page.route('**/api/cove/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:'usr_test'}]}}));
 await page.route('**/api/account/me',r=>r.fulfill({json:{id:'usr_test',displayName:'Tester'}}));
 await page.route('**/api/account/friends?*',r=>r.fulfill({json:[{id:'usr_friend',displayName:'A friend',status:'active'}]}));
 await page.route('**/api/catalog?*',r=>r.fulfill({json:{avatars:[{id:avatar,name:'Saved avatar',author:'Creator',tags:[]}],total:1}}));
 await page.route('**/api/account/world?*',r=>r.fulfill({json:{id:world,name:'Saved world',authorName:'World creator'}}));
 await page.route('**/api/account/favorites?*',async r=>{
  await new Promise(done=>setTimeout(done,400));
  const type=new URL(r.request().url()).searchParams.get('type');
  return r.fulfill({json:type==='world'?[{id:'fvrt_world',favoriteId:world}]:[{id:'fvrt_avatar',favoriteId:avatar}]});
 });
 await page.goto('/#friends');
 await expect(page.getByText('A friend').first()).toBeVisible();
 await page.evaluate(()=>{window.location.hash='#favorites';});
 await expect(page.locator('.avatar-card')).toContainText('Saved avatar');
 const tabs=page.getByLabel('Favorite type');
 await tabs.getByRole('button',{name:'Worlds',exact:true}).click();
 await expect(page.locator('.world-card')).toContainText('Saved world');
 await tabs.getByRole('button',{name:'Avatars',exact:true}).click();
 await expect(page.locator('.avatar-card')).toContainText('Saved avatar');
 await expect(page.locator('.avatar-card .rating-dot')).toHaveClass(/unknown/);
 expect(errors).toEqual([]);
});
