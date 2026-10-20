import { test, expect } from '@playwright/test';

const me = 'usr_00000000-0000-0000-0000-000000000001';
const world = 'wrld_00000000-0000-0000-0000-0000000000aa';
const instance = '12345~region(eu)~private(usr_00000000-0000-0000-0000-000000000003)~nonce(abc-123)';

test('launch links keep the instance location readable by VRChat', async ({ page }) => {
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: me }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: me, displayName: 'Tester' } }));
  await page.route('**/api/account/world?*', (r) =>
    r.fulfill({
      json: {
        id: world,
        name: 'Launch Test World',
        description: '',
        capacity: 16,
        platforms: [],
        tags: [],
        instances: [{ id: instance, users: 3 }],
      },
    }),
  );
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/cove/reviews?*', (r) => r.fulfill({ status: 503, json: {} }));
  await page.goto(`/world/${world}`);
  // VRChat does not decode the id parameter, so ":" must not become "%3A".
  await expect(page.getByRole('link', { name: /^Launch .* in VRChat$/ })).toHaveAttribute(
    'href',
    `vrchat://launch?ref=vrcove&id=${world}:${instance}`,
  );
});
