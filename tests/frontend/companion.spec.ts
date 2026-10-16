import { test, expect } from '@playwright/test';

test('companion page opens by path and from the banner, and downloads from GitHub', async ({ page }) => {
  await page.goto('/avatars');
  await page.locator('.companion-download').getByRole('link', { name: 'Get the companion' }).click();
  await expect(page).toHaveURL(/\/companion$/);
  await expect(page.getByRole('heading', { name: 'Help grow the avatar database.' })).toBeVisible();
  for (const name of [
    'Setup',
    'Checked twice',
    'Your credentials',
    'Gentle on rate limits',
    'Small and native',
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
  await page.locator('footer').getByRole('link', { name: 'Desktop companion' }).click();
  await expect(page).toHaveURL(/\/companion$/);
  await page.locator('.topbar select').selectOption('ja');
  await expect(
    page.getByRole('heading', { name: 'アバターデータベースの拡充にご協力ください。' }),
  ).toBeVisible();
});
