import {test,expect} from '@playwright/test';

async function openNavigation(page:import('@playwright/test').Page){
 const menu=page.getByRole('button',{name:'Open navigation',exact:true});
 if(await menu.isVisible())await menu.click();
}

test('install button appears when the browser offers installation',async({page})=>{
 await page.goto('/');
 await openNavigation(page);
 const install=page.getByRole('button',{name:'Install app'});
 const isApple=await page.evaluate(()=>/iPad|iPhone|iPod/.test(navigator.userAgent));
 if(!isApple)await expect(install).toHaveCount(0);
 await page.evaluate(()=>{
  const e:any=new Event('beforeinstallprompt',{cancelable:true});
  e.prompt=async()=>{(window as any).__prompted=true;};
  e.userChoice=Promise.resolve({outcome:'accepted'});
  window.dispatchEvent(e);
 });
 await expect(install).toBeVisible();
 await install.click();
 await expect.poll(()=>page.evaluate(()=>(window as any).__prompted)).toBe(true);
 await expect(install).toHaveCount(0);
});

test('iOS shows Add to Home Screen steps',async({browser})=>{
 const context=await browser.newContext({userAgent:'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1',viewport:{width:390,height:844},hasTouch:true});
 const page=await context.newPage();
 await page.goto('/');
 await openNavigation(page);
 await page.getByRole('button',{name:'Install app'}).click();
 const dialog=page.getByRole('dialog',{name:'Install VRCove'});
 await expect(dialog).toContainText('Add to Home Screen');
 await dialog.getByRole('button',{name:'Close dialog'}).click();
 await expect(dialog).toHaveCount(0);
 await context.close();
});

test('manifest describes an installable app',async({request})=>{
 const manifest=await (await request.get('/manifest.webmanifest')).json();
 expect(manifest).toMatchObject({name:'VRCove',start_url:'/',display:'standalone'});
 expect(manifest.icons.map((i:any)=>i.sizes)).toEqual(expect.arrayContaining(['192x192','512x512']));
 for(const icon of manifest.icons)expect((await request.get(icon.src)).headers()['content-type']).toBe('image/png');
});
