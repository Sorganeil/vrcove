import { test, expect, type Page } from '@playwright/test';

const link = {
  id: 1,
  vrchatId: 'usr_00000000-0000-0000-0000-000000000001',
  displayName: 'VRChat Handle',
  username: 'synthetic',
  linked: '2026-10-15',
};
const me = (links: any[]) => ({
  username: 'gate_identity',
  created: '2026-10-15',
  bio: '',
  public: false,
  picture: '',
  links,
});

async function visitFriends(page: Page, coveMe: any) {
  await page.route('**/api/stats', (r) =>
    r.fulfill({ json: { accountsConfigured: true, loginConfigured: true, indexed: 0 } }),
  );
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars: [], total: 0 } }));
  await page.route('**/api/cove/me', (r) =>
    coveMe
      ? r.fulfill({ json: coveMe })
      : r.fulfill({ status: 401, json: { error: 'Sign in to your VRCove account.' } }),
  );
  await page.route('**/api/account/me', (r) =>
    r.fulfill({ status: 401, json: { code: 'vrchat_session_required', error: 'Connect VRChat' } }),
  );
  await page.goto('/friends');
  return page.locator('.empty-state h3');
}

test('the VRChat prompt explains the next step for each account state', async ({ page }) => {
  await expect(await visitFriends(page, null)).toHaveText('Sign in to VRCove and link VRChat');
});
test('a VRCove account without a link is asked to link VRChat', async ({ page }) => {
  await expect(await visitFriends(page, me([]))).toHaveText('Link your VRChat account');
});
test('a linked account in a new browser is asked to connect on this device', async ({ page }) => {
  await expect(await visitFriends(page, me([link]))).toHaveText('Connect VRChat on this device');
});
