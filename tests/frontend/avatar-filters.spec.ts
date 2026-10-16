import {test,expect,type Page} from '@playwright/test';

const avatar={id:'avtr_00000000-0000-0000-0000-000000000901',name:'Fox Scout',author:'Kit Maker',authorId:'usr_00000000-0000-0000-0000-00000000000a',description:'',image:'',thumbnail:'',pc:true,quest:true,rating:'Good',tags:['author_tag_furry'],updated:'2026-01-01T00:00:00Z',created:'2026-01-01T00:00:00Z'};

// Answers every catalog request with the avatar and records the query parameters.
async function catalog(page:Page){
 const seen:URLSearchParams[]=[];
 await page.route('**/api/catalog?*',r=>{seen.push(new URL(r.request().url()).searchParams);return r.fulfill({json:{avatars:[avatar],total:1,page:0}});});
 return seen;
}
const lastWith=(seen:URLSearchParams[],check:(p:URLSearchParams)=>boolean)=>expect.poll(()=>seen.some(check)).toBe(true);

test('creator, tag, content, rating and sort filters reach the catalog and can be cleared',async({page})=>{
 const seen=await catalog(page);
 await page.goto('/avatars');
 await page.getByRole('button',{name:'Filters'}).click();
 await page.getByLabel('Filter by creator').fill('Kit Maker');
 await page.getByLabel('Filter by tag').fill('furry');
 await page.getByLabel('Hide adult content').check();
 await page.getByLabel('Hide horror').check();
 await page.getByLabel('Quest fallback avatars only').check();
 await page.getByLabel('Performance rating').selectOption('Good+');
 await page.getByLabel('Sort avatars').selectOption('author');
 await lastWith(seen,p=>p.get('author')==='Kit Maker'&&p.get('tag')==='furry'&&p.get('hide')==='adult,horror'&&p.get('fallback')==='1'&&p.get('rating')==='Good+'&&p.get('sort')==='author');
 const chip=page.locator('.active-filters').getByRole('button',{name:/Kit Maker/});
 await expect(chip).toBeVisible();
 await chip.click();
 await lastWith(seen,p=>!p.has('author')&&p.get('tag')==='furry');
 await page.getByRole('button',{name:'Reset filters'}).click();
 await lastWith(seen,p=>!p.has('tag')&&!p.has('hide')&&!p.has('fallback')&&p.get('rating')==='all');
});

test('an avatar links to more by its creator and to its tags',async({page})=>{
 const seen=await catalog(page);
 await page.goto('/avatar/'+avatar.id);
 await page.getByRole('button',{name:'More by Kit Maker'}).click();
 await expect(page).toHaveURL(/\/avatars$/);
 await lastWith(seen,p=>p.get('author')==='Kit Maker'&&p.get('authorId')===avatar.authorId);
 await expect(page.locator('.active-filters')).toContainText('Creator: Kit Maker');
 await page.goto('/avatar/'+avatar.id);
 await page.locator('.tags').getByRole('button',{name:'furry'}).click();
 await lastWith(seen,p=>p.get('tag')==='furry');
});

test('collection items open in the quick view instead of leaving the page',async({page})=>{
 await catalog(page);
 const id='AbCdEfGhIjKlMnOp';
 await page.route('**/api/cove/collection?*',r=>r.fulfill({json:{id,kind:'avatar',name:'Favourite foxes',description:'',public:true,updated:'2026-10-16',owner:null,mine:false,items:[{id:avatar.id,name:avatar.name,image:'',added:'2026-10-16'}]}}));
 await page.goto('/collection/'+id);
 await page.locator('.collection-items').getByRole('link',{name:/Fox Scout/}).click();
 await expect(page.getByRole('dialog',{name:'Fox Scout'})).toBeVisible();
 await expect(page).toHaveURL(new RegExp('/collection/'+id+'$'));
});

test('phone header has a home logo and comfortable tap targets',async({page,isMobile})=>{
 test.skip(!isMobile,'phone layout only');
 await catalog(page);
 await page.goto('/avatars');
 const home=page.locator('.topbar').getByRole('link',{name:'Home'});
 await expect(home).toHaveAttribute('href','/');
 for(const control of [page.locator('.topbar .header-search'),page.locator('.topbar .language-picker'),page.locator('.topbar .mobile-menu')]){
  const box=(await control.boundingBox())!;
  expect(box.width).toBeGreaterThanOrEqual(40);
  expect(box.height).toBeGreaterThanOrEqual(40);
 }
 expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
 await home.click();
 await expect(page).toHaveURL(/:\d+\/$/);
 await expect(page.getByRole('heading',{name:'Home',exact:true})).toBeVisible();
});

