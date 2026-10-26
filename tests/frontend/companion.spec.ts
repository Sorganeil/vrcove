import { test, expect } from '@playwright/test';

test('desktop app page opens by path and from the banner, and downloads from GitHub', async ({
  page,
  isMobile,
}) => {
  await page.goto('/avatars');
  const banner = page.locator('.companion-download');
  if (isMobile) {
    // Phones can't run the desktop app, so the banner stays hidden there.
    await expect(banner).toBeHidden();
    await page.goto('/companion');
  } else {
    await banner.getByRole('link', { name: 'Get VRCove Desktop' }).click();
  }
  await expect(page).toHaveURL(/\/companion$/);
  await expect(page.getByRole('heading', { name: 'VRCove on your desktop.' })).toBeVisible();
  for (const name of [
    'Setup',
    'Straight to VRChat',
    'Avatar logger built in',
    'Your history, on your computer',
    'Tools',
    'Open source',
  ])
    await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  const download = page.getByRole('link', { name: 'Download for Windows or Linux' });
  await expect(download).toHaveAttribute('href', 'https://github.com/Sorganeil/vrcove/releases/latest');
  await expect(download).toHaveAttribute('target', '_blank');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
  await page.locator('.companion-setup').getByRole('link', { name: 'Open Account page' }).click();
  await expect(page).toHaveURL(/\/account$/);
  await page.locator('footer').getByRole('link', { name: 'Desktop app' }).click();
  await expect(page).toHaveURL(/\/companion$/);
  await page.locator('.topbar select').selectOption('ja');
  await expect(
    page.getByRole('heading', { name: 'デスクトップで使う VRCove。' }),
  ).toBeVisible();
});

test('the desktop app banner can be dismissed and stays hidden', async ({ page, isMobile }, testInfo) => {
  test.skip(isMobile, 'The banner is desktop only.');
  await page.goto('/avatars');
  const banner = page.locator('.companion-download');
  await banner.screenshot({ path: testInfo.outputPath('companion-banner.png') });
  await banner.getByRole('button', { name: 'Dismiss' }).click();
  await expect(banner).toHaveCount(0);
  await page.reload();
  await expect(page.locator('.companion-download')).toHaveCount(0);
  await page.goto('/');
  await expect(page.locator('.companion-download')).toHaveCount(0);
  // The desktop app page is still one click away in the sidebar.
  await expect(page.getByRole('link', { name: 'Desktop app' }).first()).toBeVisible();
});
