import { test, expect } from '@playwright/test';
import { randomBytes } from 'node:crypto';

const avatar = 'avtr_00000000-0000-0000-0000-000000000001';

test('first account session offers linking and remembers dismissal', async ({ page }) => {
  const name = 'welcome_' + randomBytes(5).toString('hex');
  const password = 'synthetic welcome test passphrase';
  const groups = randomBytes(12)
    .toString('hex')
    .match(/.{1,4}/g)!;
  await page.setExtraHTTPHeaders({ 'CF-Connecting-IP': 'fd00:' + groups.join(':') + ':1' });
  await page.goto('/#account');
  await page.getByRole('button', { name: 'Create account', exact: true }).click();
  await page.getByLabel('VRCove username').fill(name);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password').fill(password);
  await page.locator('form').getByRole('button', { name: 'Create account', exact: true }).click();
  const reminder = page.getByRole('status').filter({ hasText: 'Get the most out of VRCove' });
  await expect(reminder).toBeVisible();
  await expect(reminder.getByRole('button', { name: 'Link VRChat now' })).toBeVisible();
  await reminder.getByRole('button', { name: 'Maybe later' }).click();
  await expect(reminder).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByLabel('VRCove username').fill(name);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.locator('form').getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await expect(reminder).toHaveCount(0);
  // Profile links are shareable paths, so copied links get an embed.
  const profileLink = page.getByRole('link', { name: 'View my profile' });
  await expect(profileLink).toHaveAttribute('href', '/cove/' + name);
  await profileLink.click();
  await expect(page).toHaveURL(new RegExp('/cove/' + name + '$'));
  await expect(page.getByText('VRCOVE PROFILE / PRIVATE')).toBeVisible();
});

test('avatar deep links and legacy links open the correct page', async ({ page }) => {
  await page.route('**/api/catalog?*', (r) =>
    r.fulfill({
      json: {
        avatars: [{ id: avatar, name: 'Shared Avatar', author: 'Creator', rating: 'Good', tags: [] }],
        total: 1,
      },
    }),
  );
  await page.goto('/avatar/' + avatar);
  await expect(page.getByRole('heading', { name: 'Shared Avatar', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Copy link', exact: true })).toBeVisible();
  if (page.viewportSize()!.width < 650)
    await page.getByRole('button', { name: 'Open navigation', exact: true }).click();
  await page.locator('.sidebar').getByRole('link', { name: 'Home', exact: true }).click();
  await expect(page).toHaveURL(/:\d+\/$/);
  await page.goto('/#avatar/' + avatar);
  await expect(page).toHaveURL(new RegExp('/avatar/' + avatar + '$'));
  await expect(page.getByRole('heading', { name: 'Shared Avatar', exact: true })).toBeVisible();
});

test('old # section links become shareable paths and the app renders no # links', async ({ page }) => {
  await page.goto('/#about');
  await expect(page).toHaveURL(/\/about$/);
  await expect(
    page.getByRole('heading', { name: 'VRChat in your browser, without the tracking.' }),
  ).toBeVisible();
  await page.goto('/#home');
  await expect(page).toHaveURL(/:\d+\/$/);
  await page.goto('/account');
  await expect(page.locator('a[href^="#"]')).toHaveCount(0);
});
