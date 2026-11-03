import { test, expect } from '@playwright/test';

const user = {
  id: 'usr_00000000-0000-0000-0000-000000000001',
  displayName: 'A long VRChat display name',
  status: 'active',
  location: 'offline',
};

test('mobile header controls do not overlap and dashboard cards fill the column', async ({
  page,
}, testInfo) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({
      json: {
        username: 'cove_identity',
        links: [{ id: 1, vrchatId: user.id, displayName: user.displayName }],
      },
    }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: user }));
  await page.route('**/api/account/friends', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/worlds', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/notifications', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars: [], total: 0 } }));
  await page.goto('/');
  await expect(page.locator('.dashboard-aside > section')).toHaveCount(3);
  for (const width of [320, 360, 390, 430, 650]) {
    await page.setViewportSize({ width, height: 844 });
    const controls = page.locator(
      '.mobile-menu, .topbar-home, .topbar .vrchat-account-switcher, .topbar .language-picker, .header-search, .account-chip',
    );
    const boxes = await controls.evaluateAll((elements) =>
      elements.map((el) => {
        const { x, y, width, height } = el.getBoundingClientRect();
        return { x, y, width, height };
      }),
    );
    for (const [i, box] of boxes.entries()) {
      expect(box.width).toBeGreaterThan(0);
      expect(box.x).toBeGreaterThanOrEqual(0);
      expect(box.x + box.width).toBeLessThanOrEqual(width);
      for (const other of boxes.slice(i + 1)) {
        const overlapX = Math.min(box.x + box.width, other.x + other.width) - Math.max(box.x, other.x);
        const overlapY = Math.min(box.y + box.height, other.y + other.height) - Math.max(box.y, other.y);
        expect(overlapX > 0 && overlapY > 0, `Header controls overlap at ${width}px`).toBe(false);
      }
    }
    const aside = await page.locator('.dashboard-aside').boundingBox();
    for (const card of await page.locator('.dashboard-aside > section').all()) {
      const box = await card.boundingBox();
      expect(box!.x).toBeCloseTo(aside!.x, 0);
      expect(box!.width).toBeCloseTo(aside!.width, 0);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
    if (width === 390) {
      await page.screenshot({ path: testInfo.outputPath('mobile-header.png') });
      await page.locator('.dashboard-aside').screenshot({ path: testInfo.outputPath('mobile-cards.png') });
    }
  }
  await page.getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'Search', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Close search', exact: true }).click();
  await page.getByRole('button', { name: 'Open navigation' }).click();
  await expect(page.locator('.sidebar')).toHaveClass(/open/);
});

test('the signed-out mobile header fits without an empty account row', async ({ page }) => {
  await page.route('**/api/cove/me', (r) => r.fulfill({ status: 401, json: { error: 'Sign in' } }));
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars: [], total: 0 } }));
  await page.goto('/');
  for (const width of [320, 390, 650]) {
    await page.setViewportSize({ width, height: 844 });
    await expect(page.getByRole('button', { name: 'Sign in to VRCove', exact: true })).toBeVisible();
    const logo = await page.locator('.topbar-home').boundingBox();
    const actions = await page.locator('.top-actions').boundingBox();
    expect(logo!.x + logo!.width).toBeLessThanOrEqual(actions!.x);
    expect((await page.locator('.topbar').boundingBox())!.height).toBeLessThan(80);
    expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBe(width);
  }
});
