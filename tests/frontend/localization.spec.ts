import {test,expect} from '@playwright/test';
import {readFileSync} from 'node:fs';
const catalogs:Record<string,Record<string,string>>=JSON.parse(readFileSync(new URL('../../app/locales/messages.json',import.meta.url),'utf8'));

const homeNames:Record<string,string>={en:'Home',es:'Inicio',fr:'Accueil',de:'Startseite',ja:'ホーム',ru:'Главная',uk:'Головна','zh-CN':'首页',ko:'홈'};

test('all languages switch immediately, survive reload and fit the viewport',async({page},testInfo)=>{
 await page.goto('/');
 const picker=page.locator('.topbar select');
 await expect(picker.locator('option')).toHaveCount(9);
 for(const [code,name] of Object.entries(homeNames)){
  await picker.selectOption(code);
  await expect(page.locator('html')).toHaveAttribute('lang',code);
  await expect(page.getByRole('heading',{name,exact:true})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
  if(code==='ru')await page.screenshot({path:`/tmp/cove-localization-${testInfo.project.name}.png`,fullPage:true});
  const bounds=await picker.boundingBox();
  expect(bounds!.x).toBeGreaterThanOrEqual(0);
  expect(bounds!.x+bounds!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
 }
 await page.reload();
 await expect(picker).toHaveValue('ko');
 await expect(page.getByRole('heading',{name:'홈',exact:true})).toBeVisible();
});

test('switching language preserves account input and route, settings selectors stay in sync',async({page})=>{
 await page.goto('/#account');
 const username=page.locator('input[autocomplete="username"]');
 await username.fill('draft_username');
 await page.locator('.topbar select').selectOption('uk');
 await expect(username).toHaveValue('draft_username');
 await expect(page.getByRole('button',{name:'Створити обліковий запис',exact:true})).toBeVisible();
 // Old #account links are rewritten to the shareable path.
 expect(new URL(page.url()).pathname).toBe('/account');
 await page.goto('/#settings');
 await expect(page.locator('.language-settings select')).toHaveValue('uk');
 await page.locator('.language-settings select').selectOption('ja');
 await expect(page.locator('.topbar select')).toHaveValue('ja');
 await expect(page.getByRole('heading',{name:'プライバシーと設定',exact:true})).toBeVisible();
});

test('detects regional browser languages, falls back safely and works without storage',async({browser,baseURL})=>{
 for(const [browserLocale,expected] of [['ru-RU','ru'],['uk-UA','uk'],['zh-TW','zh-CN'],['ko-KR','ko'],['it-IT','en']]){
  const context=await browser.newContext({locale:browserLocale});const page=await context.newPage();
  await page.goto(baseURL!);
  await expect(page.locator('html')).toHaveAttribute('lang',expected);
  await context.close();
 }
 const context=await browser.newContext({locale:'fr-FR'});const page=await context.newPage();
 await page.addInitScript(()=>{Object.defineProperty(window,'localStorage',{get(){throw new DOMException('Blocked','SecurityError');}});});
 await page.goto(baseURL!);
 await expect(page.locator('html')).toHaveAttribute('lang','fr');
 await page.locator('.topbar select').selectOption('de');
 await expect(page.getByRole('heading',{name:'Startseite',exact:true})).toBeVisible();
 await context.close();
});

test('language changes sync across tabs and invalid preferences use browser fallback',async({page,context})=>{
 await page.addInitScript(()=>localStorage.setItem('veilchat-language','invalid-locale'));
 await page.goto('/');
 await expect(page.locator('html')).toHaveAttribute('lang','en');
 const other=await context.newPage();await other.goto('/');
 await page.locator('.topbar select').selectOption('es');
 await expect(other.getByRole('heading',{name:'Inicio',exact:true})).toBeVisible();
 await other.close();
});

test('catalogs have matching keys and preserve interpolation placeholders',()=>{
 const source=Object.keys(catalogs.es).sort();
 for(const messages of Object.values(catalogs)){
  expect(Object.keys(messages).sort()).toEqual(source);
  for(const [key,value] of Object.entries(messages)){
   expect(value.trim()).not.toBe('');
   expect(value.match(/\{\w+\}/g)?.sort()||[]).toEqual(key.match(/\{\w+\}/g)?.sort()||[]);
  }
 }
});

test('formats dates and numbers in the chosen language and retains English fallback',async({page})=>{
 await page.goto('/');
 await page.locator('.topbar select').selectOption('de');
 const result=await page.evaluate(async()=>{
  const path='/app/i18n.ts';const i18n=await import(/* @vite-ignore */ path);
  return {number:i18n.formatNumber(12345.5),date:i18n.formatDate('2026-10-15T12:00:00Z',{year:'numeric',month:'long',day:'numeric',timeZone:'UTC'}),missing:i18n.t('English fallback {value}',{value:'kept'}),count:i18n.t('{count} avatars',{count:12345})};
 });
 expect(result).toEqual({number:'12.345,5',date:'15. Oktober 2026',missing:'English fallback kept',count:'12.345 Avatare'});
});
