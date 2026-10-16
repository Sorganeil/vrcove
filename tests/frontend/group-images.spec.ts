import { test, expect } from '@playwright/test';

const user = 'usr_00000000-0000-0000-0000-000000000001';
const group = 'grp_00000000-0000-0000-0000-000000000002';
const icon = 'https://assets.vrchat.com/icon.png';
const goodIcon = 'https://assets.vrchat.com/good-icon.png';
const banner = 'https://assets.vrchat.com/banner.png';
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=',
  'base64',
);

test('group browser and discovery fall back from missing or broken icons to banners', async ({ page }) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: user }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: user, displayName: 'Tester' } }));
  const groups = [
    { groupId: group, name: 'Banner community', iconUrl: null, bannerUrl: banner },
    { groupId: group + 'a', name: 'Broken community', iconUrl: icon, bannerUrl: banner },
    { groupId: group + 'b', name: 'Icon community', iconUrl: goodIcon, bannerUrl: banner },
    { groupId: group + 'c', name: 'Unavailable community', iconUrl: icon, bannerUrl: icon },
  ];
  await page.route('**/api/account/groups', (r) => r.fulfill({ json: groups }));
  await page.route('**/api/account/search?*', (r) =>
    r.fulfill({ json: groups.map(({ groupId, ...g }) => ({ ...g, id: groupId })) }),
  );
  await page.route('**/api/image?*', (r) => {
    const url = new URL(r.request().url()).searchParams.get('url');
    return url === banner || url === goodIcon
      ? r.fulfill({ contentType: 'image/png', body: png })
      : r.fulfill({ status: 404 });
  });
  async function verifyImages() {
    for (const name of ['Banner community', 'Broken community', 'Icon community']) {
      const image = page.locator('.search-result').filter({ hasText: name }).locator('img');
      await image.scrollIntoViewIfNeeded();
      await expect(image).toHaveAttribute(
        'src',
        '/api/image?url=' + encodeURIComponent(name === 'Icon community' ? goodIcon : banner),
      );
      await expect(image).toHaveJSProperty('naturalWidth', 1);
    }
    const unavailable = page.locator('.search-result').filter({ hasText: 'Unavailable community' });
    await unavailable.scrollIntoViewIfNeeded();
    await expect(unavailable.locator('.initial-fallback')).toHaveText('U');
  }
  await page.goto('/#my-groups');
  await verifyImages();
  await page.goto('/#groups');
  const search = page.getByRole('searchbox', { name: 'Search groups', exact: true });
  await search.fill('community');
  await search.press('Enter');
  await verifyImages();
});

test('profile groups use banners when icons are missing or fail', async ({ page }) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'independent_handle', links: [{ vrchatId: user }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: user, displayName: 'Test user' } }));
  await page.route('**/api/account/user?*', (r) =>
    r.fulfill({ json: { id: user, displayName: 'Test user' } }),
  );
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/user-groups?*', (r) =>
    r.fulfill({
      json: [
        { id: group, name: 'Missing icon', iconUrl: null, bannerUrl: banner },
        { id: group + 'a', name: 'Broken icon', iconUrl: icon, bannerUrl: banner },
        { id: group + 'b', name: 'No artwork' },
        { id: group + 'c', name: 'Working icon', iconUrl: goodIcon, bannerUrl: banner },
        {
          id: group + 'd',
          name: 'Unavailable artwork',
          iconUrl: icon,
          bannerUrl: 'https://assets.vrchat.com/missing.png',
        },
      ],
    }),
  );
  const requested: string[] = [];
  await page.route('**/api/image?*', (r) => {
    const url = new URL(r.request().url()).searchParams.get('url')!;
    requested.push(url);
    return url === banner || url === goodIcon
      ? r.fulfill({ contentType: 'image/png', body: png })
      : r.fulfill({ status: 404 });
  });
  await page.goto('/#user/' + user);
  for (const name of ['Missing icon', 'Broken icon']) {
    const card = page.locator('.entity-card').filter({ hasText: name });
    await card.scrollIntoViewIfNeeded();
    const image = card.locator('img');
    await expect(image).toHaveAttribute('src', '/api/image?url=' + encodeURIComponent(banner));
    await expect(image).toHaveJSProperty('naturalWidth', 1);
  }
  await expect(
    page.locator('.entity-card').filter({ hasText: 'No artwork' }).locator('.initial-fallback'),
  ).toHaveText('N');
  const working = page.locator('.entity-card').filter({ hasText: 'Working icon' }).locator('img');
  await working.scrollIntoViewIfNeeded();
  await expect(working).toHaveAttribute('src', '/api/image?url=' + encodeURIComponent(goodIcon));
  await expect(working).toHaveJSProperty('naturalWidth', 1);
  const unavailable = page.locator('.entity-card').filter({ hasText: 'Unavailable artwork' });
  await unavailable.scrollIntoViewIfNeeded();
  await expect(unavailable.locator('.initial-fallback')).toHaveText('U');
  expect(requested).toContain(icon);
});
