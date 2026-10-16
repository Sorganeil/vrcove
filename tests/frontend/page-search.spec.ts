import { test, expect } from '@playwright/test';

const user = 'usr_00000000-0000-0000-0000-000000000001';
const group = 'grp_00000000-0000-0000-0000-000000000002';
const world = 'wrld_00000000-0000-0000-0000-000000000003';

test.beforeEach(async ({ page }) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: user }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: user, displayName: 'Tester' } }));
});

test('friends search includes later pages and follows online/offline tabs', async ({ page }) => {
  await page.route('**/api/account/friends?*', (r) => {
    const query = new URL(r.request().url()).searchParams;
    const rows =
      query.get('offline') === 'true'
        ? [{ id: user, displayName: 'Offline Match' }]
        : query.get('offset') === '100'
          ? [{ id: user, displayName: 'Later Match' }]
          : Array.from({ length: 100 }, (_, i) => ({ id: `friend-${i}`, displayName: `Friend ${i}` }));
    return r.fulfill({ json: rows });
  });
  await page.goto('/#friends');
  const search = page.getByRole('searchbox', { name: 'Search friends', exact: true });
  await search.fill('  MATCH  ');
  await expect(page.locator('.friend-card')).toHaveCount(1);
  await expect(page.locator('.friend-card')).toContainText('Later Match');
  await page.getByRole('button', { name: 'Offline', exact: true }).click();
  await expect(page.locator('.friend-card')).toContainText('Offline Match');
  await search.fill('missing');
  await expect(page.getByRole('heading', { name: 'No friends found' })).toBeVisible();
  await page.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(page.locator('.friend-card')).toContainText('Offline Match');
});

test('world search queries VRChat, paginates and restores popular worlds on clear', async ({ page }) => {
  await page.route('**/api/account/worlds', (r) =>
    r.fulfill({ json: [{ id: world, name: 'Popular world' }] }),
  );
  await page.route('**/api/account/search?*', (r) => {
    const q = new URL(r.request().url()).searchParams;
    expect(q.get('type')).toBe('worlds');
    expect(q.get('q')).toBe('Forest');
    return r.fulfill({
      json:
        q.get('offset') === '50'
          ? []
          : Array.from({ length: 50 }, (_, i) => ({ id: `${world}-${i}`, name: `Forest ${i}` })),
    });
  });
  await page.goto('/#worlds');
  await expect(page.locator('.world-card')).toContainText('Popular world');
  await page.getByRole('searchbox', { name: 'Search worlds', exact: true }).fill('Forest');
  await expect(page.locator('.world-card')).toHaveCount(50);
  await page.getByRole('button', { name: 'Next', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'No worlds found' })).toBeVisible();
  await page.getByRole('button', { name: 'Previous', exact: true }).click();
  await expect(page.locator('.world-card')).toHaveCount(50);
  await page.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(page.locator('.world-card')).toHaveCount(1);
  await expect(page.locator('.world-card')).toContainText('Popular world');
});

test('groups search defaults to discovery and can switch to memberships', async ({ page }, testInfo) => {
  await page.route('**/api/account/groups', (r) =>
    r.fulfill({ json: [{ groupId: group, name: 'My community' }] }),
  );
  await page.route('**/api/account/search?*', (r) => {
    const q = new URL(r.request().url()).searchParams;
    expect(q.get('type')).toBe('groups');
    return r.fulfill({ json: q.get('q') === 'empty' ? [] : [{ id: group, name: 'Found community' }] });
  });
  await page.goto('/#groups');
  const scope = page.getByRole('group', { name: 'Group search scope' });
  await expect(scope.getByRole('button', { name: 'All groups', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(page.getByRole('heading', { name: 'Discover groups', exact: true })).toBeVisible();
  const search = page.getByRole('searchbox', { name: 'Search groups', exact: true });
  await expect(search).toHaveAttribute('placeholder', 'Search all groups…');
  await search.fill('community');
  await page.locator('form').getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.locator('.search-result')).toContainText('Found community');
  await expect(page.locator('.search-result-name')).toHaveAttribute('href', '/group/' + group);
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
  await page.screenshot({ path: `/tmp/cove-group-search-${testInfo.project.name}.png`, fullPage: true });
  await search.fill('empty');
  await search.press('Enter');
  await expect(page.getByRole('heading', { name: 'No results found' })).toBeVisible();
  await scope.getByRole('button', { name: 'My groups', exact: true }).click();
  await expect(search).toHaveValue('empty');
  await expect(page.getByRole('heading', { name: 'No results found' })).toBeVisible();
  await page.getByRole('button', { name: 'Clear search', exact: true }).click();
  await expect(page.locator('.search-result')).toContainText('My community');
  await scope.getByRole('button', { name: 'All groups', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Discover groups', exact: true })).toBeVisible();
  await expect(page.locator('.search-result')).toHaveCount(0);
});

test('my groups page lists memberships from the VRChat section', async ({ page }) => {
  await page.route('**/api/account/groups', (r) =>
    r.fulfill({
      json: [
        { groupId: group, name: 'My community' },
        { groupId: 'grp_00000000-0000-0000-0000-000000000004', name: 'Other club' },
      ],
    }),
  );
  await page.route('**/api/account/search?*', (r) => r.abort());
  await page.goto('/#home');
  const nav = page.locator('section.nav-group[aria-label="VRChat"]');
  const menu = page.getByRole('button', { name: 'Open navigation', exact: true });
  if (await menu.isVisible()) await menu.click();
  await nav.getByRole('link', { name: 'My groups' }).click();
  await expect(page).toHaveURL(/\/my-groups$/);
  await expect(page.getByRole('heading', { level: 1, name: 'My groups' })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Group search scope' })).toHaveCount(0);
  await expect(page.locator('.search-result')).toHaveCount(2);
  const search = page.getByRole('searchbox', { name: 'Search groups', exact: true });
  await expect(search).toHaveAttribute('placeholder', 'Search my groups…');
  await search.fill('other');
  await expect(page.locator('.search-result')).toHaveCount(1);
  await expect(page.locator('.search-result')).toContainText('Other club');
  await page.reload();
  await expect(page.getByRole('heading', { level: 1, name: 'My groups' })).toBeVisible();
});

test('users and avatars keep their own search routes and show results', async ({ page }) => {
  await page.route('**/api/account/search?*', (r) => {
    const q = new URL(r.request().url()).searchParams;
    expect(q.get('type')).toBe('users');
    expect(q.get('q')).toBe('Alice');
    return r.fulfill({ json: [{ id: user, displayName: 'Alice' }] });
  });
  await page.route('**/api/catalog?*', (r) => {
    const q = new URL(r.request().url()).searchParams;
    return r.fulfill({
      json: {
        avatars:
          q.get('q') === 'Robot'
            ? [
                {
                  id: 'avtr_00000000-0000-0000-0000-000000000004',
                  name: 'Robot',
                  author: 'Creator',
                  rating: 'Good',
                  tags: [],
                },
              ]
            : [],
        total: q.get('q') === 'Robot' ? 1 : 0,
      },
    });
  });
  await page.goto('/#users');
  await page.getByRole('searchbox', { name: 'Search users', exact: true }).fill('Alice');
  await page.locator('form').getByRole('button', { name: 'Search', exact: true }).click();
  await expect(page.locator('.search-result')).toContainText('Alice');
  await page.goto('/#avatars');
  await page.getByRole('textbox', { name: 'Search avatars', exact: true }).fill('Robot');
  await expect(page.locator('.avatar-card')).toContainText('Robot');
});

test('profile edit button keeps primary button contrast', async ({ page }, testInfo) => {
  await page.route('**/api/account/user?*', (r) => r.fulfill({ json: { id: user, displayName: 'Tester' } }));
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/user-groups?*', (r) => r.fulfill({ json: [] }));
  await page.goto('/#profile');
  const edit = page.getByRole('button', { name: 'Edit profile', exact: true });
  await expect(edit).toBeVisible();
  const colors = await edit.evaluate((button) => {
    const style = getComputedStyle(button);
    return { text: style.color, background: style.backgroundColor };
  });
  const luminance = (color: string) => {
    const rgb = color
      .match(/\d+/g)!
      .slice(0, 3)
      .map(Number)
      .map((v) => {
        v /= 255;
        return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
      });
    return rgb[0] * 0.2126 + rgb[1] * 0.7152 + rgb[2] * 0.0722;
  };
  const values = [luminance(colors.text), luminance(colors.background)].sort((a, b) => a - b);
  expect((values[1] + 0.05) / (values[0] + 0.05)).toBeGreaterThanOrEqual(4.5);
  await page.screenshot({ path: `/tmp/cove-profile-contrast-${testInfo.project.name}.png`, fullPage: true });
  await edit.click();
  await expect(page.getByRole('dialog', { name: 'Edit profile', exact: true })).toBeVisible();
});
