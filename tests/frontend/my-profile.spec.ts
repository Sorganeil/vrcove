import {test,expect} from '@playwright/test';

const id='usr_00000000-0000-0000-0000-000000000001';
const world='wrld_00000000-0000-0000-0000-000000000002';
const group='grp_00000000-0000-0000-0000-000000000003';

test('my profile shows the full VRChat profile and refreshes after editing',async({page},testInfo)=>{
 let bio='My complete VRChat biography';
 await page.route('**/api/veil/me',r=>r.fulfill({json:{username:'tester',links:[{vrchatId:id}]}}));
 await page.route('**/api/account/me',r=>r.fulfill({json:{id,displayName:'Tester',bio:'',status:'active'}}));
 await page.route('**/api/account/user?id=*',r=>r.fulfill({json:{id,displayName:'Tester',bio,pronouns:'they/them',bioLinks:['https://example.com/profile','javascript:alert(1)'],tags:['system_trust_veteran'],status:'active',statusDescription:'Exploring worlds',location:'private',dateJoined:'2020-01-02',lastLogin:'2026-10-14',lastPlatform:'standalonewindows',badges:[{id:'badge-1',name:'Community badge',description:'A showcased badge'}]}}));
 await page.route('**/api/account/user-worlds?id=*',r=>r.fulfill({json:[{id:world,name:'My published world',occupants:3}]}));
 await page.route('**/api/account/user-groups?id=*',r=>r.fulfill({json:[{id:group,name:'My community',shortCode:'MINE',discriminator:'1234',memberCount:12}]}));
 await page.route('**/api/account/profile',async r=>{bio=r.request().postDataJSON().bio;await r.fulfill({json:{id,displayName:'Tester',bio,status:'active'}});});
 await page.goto('/#profile');
 await expect(page.getByRole('heading',{name:'Tester',exact:true})).toBeVisible();
 await expect(page.locator('.entity-main .description')).toHaveText(bio);
 await expect(page.locator('.entity-hero-copy')).toContainText('they/them');
 await expect(page.locator('.entity-hero-copy')).toContainText('TRUSTED USER');
 await expect(page.locator('.entity-aside')).toContainText('Joined VRChat');
 await expect(page.locator('.entity-aside')).toContainText('Jan 2, 2020');
 await expect(page.locator('.entity-aside')).toContainText('In a private instance');
 await expect(page.locator('.entity-aside')).toContainText('PC');
 await expect(page.getByRole('link',{name:'example.com/profile'})).toHaveAttribute('href','https://example.com/profile');
 await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);
 await expect(page.locator('.badge-row [title="A showcased badge"]')).toBeVisible();
 await expect(page.getByRole('link',{name:'My published world'})).toHaveAttribute('href','/world/'+world);
 await expect(page.getByRole('link',{name:/My community/})).toHaveAttribute('href','/group/'+group);
 await expect(page.getByRole('button',{name:'Add friend',exact:true})).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth)).toBeLessThanOrEqual(page.viewportSize()!.width);
 await page.screenshot({path:`/tmp/veil-full-profile-${testInfo.project.name}.png`,fullPage:true});
 await page.getByRole('button',{name:'Edit profile',exact:true}).click();
 const dialog=page.getByRole('dialog',{name:'Edit profile',exact:true});
 await expect(dialog.getByRole('textbox',{name:/^Bio/})).toHaveValue(bio);
 await dialog.getByRole('textbox',{name:/^Bio/}).fill('Updated biography');
 await dialog.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(dialog).toBeHidden();
 await expect(page.locator('.entity-main .description')).toHaveText('Updated biography');
});
