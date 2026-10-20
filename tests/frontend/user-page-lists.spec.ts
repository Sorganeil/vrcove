import { test, expect } from '@playwright/test';

const me = 'usr_00000000-0000-0000-0000-000000000001';
const person = 'usr_00000000-0000-0000-0000-000000000009';
const group = (n: number) => ({
  id: `grp_00000000-0000-0000-0000-${String(n).padStart(12, '0')}`,
  name: `Group ${n}`,
  shortCode: 'GRP',
  discriminator: String(n).padStart(4, '0'),
  memberCount: 100 + n,
  isRepresenting: n === 20,
});

test('long group lists start short so reviews stay close', async ({ page }, testInfo) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: me }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: me, displayName: 'Tester' } }));
  await page.route('**/api/account/user?*', (r) =>
    r.fulfill({
      json: { id: person, displayName: 'Busy Joiner', bio: '', status: 'active', location: 'offline' },
    }),
  );
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/user-groups?*', (r) =>
    r.fulfill({ json: Array.from({ length: 20 }, (_, i) => group(i + 1)) }),
  );
  await page.goto(`/user/${person}`);
  const groups = page
    .locator('.entity-section')
    .filter({ has: page.getByRole('heading', { name: /^Groups/ }) });
  await expect(groups.locator('.entity-card')).toHaveCount(8);
  // The group they represent is shown first even though VRChat listed it last.
  await expect(groups.locator('.entity-card').first()).toContainText('Group 20');
  await expect(groups.getByText('1–8 of 20')).toBeVisible();
  await expect(groups.getByRole('button', { name: 'Previous' })).toBeDisabled();
  await groups.getByRole('button', { name: 'Next' }).click();
  await expect(groups.getByText('9–16 of 20')).toBeVisible();
  await expect(groups.locator('.entity-card').first()).toContainText('Group 8');
  await groups.getByRole('button', { name: 'Next' }).click();
  await expect(groups.getByText('17–20 of 20')).toBeVisible();
  await expect(groups.locator('.entity-card')).toHaveCount(4);
  await expect(groups.getByRole('button', { name: 'Next' })).toBeDisabled();
  await groups.getByRole('button', { name: 'Previous' }).click();
  await expect(groups.getByText('9–16 of 20')).toBeVisible();
  await groups.locator('.show-more').screenshot({ path: testInfo.outputPath('group-paging.png') });
  await groups.getByRole('button', { name: 'Show all 20 groups' }).click();
  await expect(groups.locator('.entity-card')).toHaveCount(20);
  await groups.getByRole('button', { name: 'Show fewer' }).click();
  await expect(groups.locator('.entity-card')).toHaveCount(8);
  await expect(page.getByRole('heading', { name: /Ratings & comments/ })).toBeVisible();
});
