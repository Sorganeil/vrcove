import { test, expect, type Page } from '@playwright/test';

const me = 'usr_00000000-0000-0000-0000-000000000001';
const id = (n: number) => `usr_00000000-0000-0000-0000-${String(n).padStart(12, '0')}`;
const friends = [
  { id: id(11), displayName: 'Game Friend', status: 'ask me', state: 'online', location: 'private' },
  {
    id: id(12),
    displayName: 'Web Friend',
    status: 'ask me',
    state: 'active',
    location: 'offline',
    platform: 'web',
  },
  { id: id(13), displayName: 'Close Gamer', status: 'join me', state: 'online', location: 'private' },
  { id: id(14), displayName: 'Close Web', status: 'active', state: 'active', location: 'offline' },
];

async function mock(page: Page) {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: me }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: me, displayName: 'Tester' } }));
  await page.route('**/api/account/friends*', (r) =>
    r.fulfill({ json: new URL(r.request().url()).searchParams.get('offline') === 'true' ? [] : friends }),
  );
  await page.route('**/api/account/favorite-groups?type=friend', (r) =>
    r.fulfill({
      json: [
        { name: 'group_1', displayName: 'Squad' },
        { name: 'group_0', displayName: 'Close friends' },
      ],
    }),
  );
  await page.route('**/api/account/favorites?type=friend*', (r) =>
    r.fulfill({
      json: [
        { id: 'fvrt_1', favoriteId: id(13), tags: ['group_0'] },
        { id: 'fvrt_2', favoriteId: id(14), tags: ['group_0'] },
      ],
    }),
  );
  await page.route('**/api/account/worlds', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/notifications', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars: [], total: 0 } }));
}

const headings = (page: Page) => page.locator('.friend-section > h2');
const card = (page: Page, name: string) => page.locator('.friend-card').filter({ hasText: name });

test('people on the website are not shown as offline or in game', async ({ page }, testInfo) => {
  await mock(page);
  await page.goto('/friends');
  await expect(headings(page)).toHaveText([/^In game\s*2$/, /^Not in game\s*2$/]);
  await expect(card(page, 'Web Friend').locator('.user-availability')).toHaveText('Not in game');
  await expect(card(page, 'Web Friend')).toContainText('On the website or app');
  await expect(card(page, 'Game Friend').locator('.user-availability')).toHaveText('Ask me');

  // Home counts only friends who are actually in game.
  await page.goto('/');
  const panel = page.locator('.dashboard-panel').filter({ hasText: 'Friends online' });
  await expect(panel.locator('h2 small')).toHaveText('2');
  // People on the website or app follow under their own heading.
  await expect(panel.locator('.online-grid > a')).toHaveCount(4);
  await expect(panel.locator('.online-subheading')).toHaveText(/^Not in game\s*2$/);
  await panel.screenshot({ path: testInfo.outputPath('home-friends.png') });
  await expect(panel.locator('.online-grid > a').nth(2)).toContainText('Web Friend');
  await expect(panel.locator('.online-grid > a').nth(2).locator('.user-availability')).toHaveText(
    'Not in game',
  );
});

test('friends can be grouped by VRChat favorite groups, and the choice is remembered', async ({
  page,
}, testInfo) => {
  await mock(page);
  await page.goto('/friends');
  await page.getByLabel('Sort friends').selectOption('favorites');
  await expect(headings(page)).toHaveText([/^Close friends\s*2$/, /^Not in a favorite group\s*2$/]);
  // In game comes first inside each group.
  const close = page.locator('.friend-section').first().locator('.friend-card h3');
  await expect(close).toHaveText(['Close Gamer', 'Close Web']);
  await page.screenshot({ path: testInfo.outputPath('friend-groups.png'), fullPage: true });
  await page.reload();
  await expect(page.getByLabel('Sort friends')).toHaveValue('favorites');
  await expect(headings(page)).toHaveText([/^Close friends\s*2$/, /^Not in a favorite group\s*2$/]);
});
