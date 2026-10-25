import { test, expect } from '@playwright/test';

const user = 'usr_00000000-0000-0000-0000-000000000001';
const world = 'wrld_00000000-0000-0000-0000-000000000003';
const avatar = (n: number, name: string, author = 'Creator') => ({
  id: 'avtr_00000000-0000-0000-0000-' + String(n).padStart(12, '0'),
  name,
  author,
  rating: 'Good',
  tags: [],
  thumbnail: '',
  pc: true,
  quest: false,
});

test.beforeEach(async ({ page }) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: user }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: user, displayName: 'Tester' } }));
});

test('one search page keeps the search across tabs, and avatar sorting follows it', async ({ page }) => {
  const catalog: URLSearchParams[] = [];
  await page.route('**/api/catalog?*', (r) => {
    const q = new URL(r.request().url()).searchParams;
    catalog.push(q);
    return r.fulfill({ json: { avatars: [avatar(1, q.get('q') ? 'Robot' : 'Newest avatar')], total: 1 } });
  });
  await page.route('**/api/account/search?*', (r) => {
    const type = new URL(r.request().url()).searchParams.get('type');
    return r.fulfill({
      json:
        type === 'worlds' ? [{ id: world, name: 'Robot world' }] : [{ id: user, displayName: 'Robot fan' }],
    });
  });
  await page.goto('/avatars');
  const tabs = page.getByRole('navigation', { name: 'Search for' });
  await expect(tabs.getByRole('link', { name: 'Avatars', exact: true })).toHaveAttribute(
    'aria-current',
    'page',
  );
  await expect(page.locator('.avatar-card')).toContainText('Newest avatar');
  const sort = page.getByLabel('Sort avatars');
  await expect(sort).toHaveValue('newest');
  // Playwright's toBeDisabled ignores <option>, so check the property itself.
  await expect(sort.locator('option[value="relevance"]')).toHaveJSProperty('disabled', true);

  // Typing switches to Best match, and the search goes into the address.
  await page.getByRole('searchbox', { name: 'Search avatars', exact: true }).fill('Robot');
  await expect(page.locator('.avatar-card')).toContainText('Robot');
  await expect(sort).toHaveValue('relevance');
  expect(catalog.at(-1)!.get('sort')).toBe('relevance');
  await expect(page).toHaveURL(/\/avatars\?q=Robot$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Avatar search results' })).toBeVisible();
  await sort.selectOption('rating');
  await expect.poll(() => catalog.at(-1)!.get('sort')).toBe('rating');

  // Other tabs open with the same search.
  await tabs.getByRole('link', { name: 'Worlds', exact: true }).click();
  await expect(page).toHaveURL(/\/worlds\?q=Robot$/);
  await expect(page.getByRole('heading', { level: 1, name: 'Worlds' })).toBeVisible();
  await expect(page.getByRole('searchbox', { name: 'Search worlds', exact: true })).toHaveValue('Robot');
  await expect(page.locator('.world-card')).toContainText('Robot world');
  await tabs.getByRole('link', { name: 'Users', exact: true }).click();
  await expect(page.getByRole('searchbox', { name: 'Search users', exact: true })).toHaveValue('Robot');
  await expect(page.locator('.search-result')).toContainText('Robot fan');
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
});

test('quick search links to the full search with the same text', async ({ page }) => {
  await page.route('**/api/catalog?*', (r) =>
    r.fulfill({ json: { avatars: [avatar(2, 'Robot')], total: 1 } }),
  );
  await page.goto('/about');
  // The app closes the quick search when the signed-in account changes, so let it load first.
  await page.waitForLoadState('networkidle');
  await page.keyboard.press('Control+k');
  const dialog = page.getByRole('dialog', { name: 'Search' });
  await dialog.getByRole('combobox', { name: 'Search' }).fill('Robot');
  await expect(dialog.getByRole('option')).toContainText('Robot');
  await dialog.getByRole('link', { name: 'See all results' }).click();
  await expect(page).toHaveURL(/\/avatars\?q=Robot$/);
  await expect(page.getByRole('searchbox', { name: 'Search avatars', exact: true })).toHaveValue('Robot');
  await expect(dialog).toHaveCount(0);
});

test('bookmarks can be sorted by when they were saved, name or creator', async ({ page }) => {
  await page.addInitScript(
    (saved) => localStorage.setItem('atlas-bookmarks', JSON.stringify(saved)),
    [avatar(11, 'Zebra', 'Mia'), avatar(12, 'apple', 'Zed'), avatar(13, 'Mango', 'amy')],
  );
  await page.goto('/bookmarks');
  const names = page.locator('.avatar-card h3');
  await expect(names).toHaveText(['Zebra', 'apple', 'Mango']);
  await page.getByLabel('Sort bookmarks').selectOption('name');
  await expect(names).toHaveText(['apple', 'Mango', 'Zebra']);
  await page.getByLabel('Sort bookmarks').selectOption('author');
  await expect(names).toHaveText(['Mango', 'Zebra', 'apple']);
  await page.getByRole('searchbox', { name: 'Search bookmarks', exact: true }).fill('zed');
  await expect(names).toHaveText(['apple']);
});

test('friends can be sorted by name', async ({ page }) => {
  await page.route('**/api/account/friends*', (r) =>
    r.fulfill({
      json: [
        { id: 'usr_00000000-0000-0000-0000-000000000021', displayName: 'Zoe', location: world + ':1' },
        { id: 'usr_00000000-0000-0000-0000-000000000022', displayName: 'adam', location: 'offline' },
        { id: 'usr_00000000-0000-0000-0000-000000000023', displayName: 'Mia', location: world + ':2' },
      ],
    }),
  );
  await page.goto('/friends');
  await page.getByLabel('Sort friends').selectOption('name');
  await expect(page.locator('.friend-card h3')).toHaveText(['adam', 'Mia', 'Zoe']);
});
