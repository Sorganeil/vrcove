import { test, expect } from '@playwright/test';

test('about page opens by path and from the menu, and links onward', async ({ page, isMobile }) => {
  await page.goto('/about');
  await expect(
    page.getByRole('heading', { name: 'VRChat in your browser, without the tracking.' }),
  ).toBeVisible();
  for (const name of [
    'Private by default',
    'Avatar search',
    'Mobile compatible',
    'Install as an app',
    'Reviews and collections',
    'Your VRChat account',
    'Open source',
  ])
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
  await page.locator('.about-actions').getByRole('button', { name: 'Support VRCove' }).click();
  await expect(page.getByRole('heading', { name: 'A little support goes a long way.' })).toBeVisible();
  if (isMobile) await page.getByRole('button', { name: 'Open navigation' }).click();
  await page.locator('.sidebar-links').getByRole('link', { name: 'About', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Open source', exact: true })).toBeVisible();
  await page.locator('.about-actions').getByRole('button', { name: 'Privacy & data' }).click();
  await expect(page.getByRole('dialog')).toBeVisible();
});

test('about page explains how to add VRCove to VRCX and the provider answers', async ({ page, context }) => {
  await page.goto('/about');
  const card = page.getByRole('article', { name: 'Use it in VRCX' });
  const address = new URL('/api/vrcx/avatars', page.url()).href;
  await expect(card.locator('code')).toHaveText(address);
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  await card.getByRole('button', { name: 'Copy address' }).click();
  await expect(card.getByRole('button', { name: 'Copied' })).toBeVisible();
  const response = await page.request.get(address + '?search=zzzz-no-such-avatar&n=5000', {
    headers: { Referer: 'https://vrcx.app' },
  });
  expect(response.status()).toBe(200);
  expect(await response.json()).toEqual([]);
});
