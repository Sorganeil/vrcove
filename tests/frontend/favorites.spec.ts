import {test,expect} from '@playwright/test';

const world='wrld_00000000-0000-0000-0000-000000000003';
const missing='wrld_00000000-0000-0000-0000-000000000004';
const avatar='avtr_00000000-0000-0000-0000-000000000005';

test('favorites switches between avatars and paginated worlds, including unavailable worlds',async({page})=>{
 await page.route('**/api/veil/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:'usr_test'}]}}));
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
