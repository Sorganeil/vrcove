import { test, expect } from '@playwright/test';

test.skip(process.env.VRCOVE_TEST_MODE !== 'desktop', 'Saved device accounts belong to the desktop app.');

test('saved accounts switch, reconnect, add with 2FA and remove without a VRCove sign-in', async ({
  page,
}, testInfo) => {
  const alice = {
    id: 101,
    vrchatId: 'usr_alice',
    username: 'alice',
    displayName: 'Alice',
    linked: '',
    active: true,
    sessionExpires: Date.now() + 86400000,
  };
  const bob = { ...alice, id: 202, vrchatId: 'usr_bob', username: 'bob', displayName: 'Bob', active: false };
  let links = [alice, bob];
  let active = alice;
  let rejectBob = false;
  const switches: number[] = [];
  const user = () => ({
    id: active.vrchatId,
    username: active.username,
    displayName: active.displayName,
    friends: [],
  });
  await page.route('**/api/account/device', (r) =>
    r.fulfill({ json: { links: links.map((l) => ({ ...l, active: l.id === active?.id })) } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: user() }));
  await page.route('**/api/account/switch', async (r) => {
    const id = r.request().postDataJSON().id;
    switches.push(id);
    if (rejectBob && id === bob.id) {
      links = links.map((l) => (l.id === bob.id ? { ...l, sessionExpires: 0 } : l));
      await r.fulfill({
        status: 401,
        json: {
          code: 'vrchat_switch_reconnect',
          error: 'Reconnect this saved VRChat account before switching. Your active account has not changed.',
        },
      });
    } else {
      active = links.find((l) => l.id === id)!;
      await r.fulfill({ json: { user: user() } });
    }
  });
  await page.route('**/api/account/device/remove', async (r) => {
    links = links.filter((l) => l.id !== r.request().postDataJSON().id);
    await r.fulfill({ json: { ok: true } });
  });
  await page.route('**/api/account/login', async (r) => {
    const body = r.request().postDataJSON();
    expect(body.username).toBe('carol');
    expect(body.expectedUserId).toBeUndefined();
    await r.fulfill({ json: { requiresTwoFactorAuth: ['totp'] } });
  });
  await page.route('**/api/account/2fa', async (r) => {
    expect(r.request().postDataJSON().code).toBe('123456');
    active = { ...alice, id: 303, vrchatId: 'usr_carol', username: 'carol', displayName: 'Carol' };
    links = [...links, active];
    await r.fulfill({ json: { user: user() } });
  });
  await page.route('**/api/stats', (r) =>
    r.fulfill({ json: { indexed: 0, loginConfigured: true, accountsConfigured: true } }),
  );
  await page.route('**/api/desktop/tools/usernames', (r) =>
    r.fulfill({ json: { running: false, phase: 'idle', rows: [], error: '', nextAt: 0, waitReason: '' } }),
  );
  await page.goto('/tools');
  const picker = page.getByLabel('Active VRChat account', { exact: true });
  await expect(picker).toHaveValue(alice.vrchatId);
  await picker.selectOption(bob.vrchatId);
  await expect(picker).toHaveValue(bob.vrchatId);
  await picker.selectOption(alice.vrchatId);
  await expect(picker).toHaveValue(alice.vrchatId);
  expect(switches).toEqual([bob.id, alice.id]);
  rejectBob = true;
  await picker.selectOption(bob.vrchatId);
  const login = page.getByRole('dialog', { name: 'Connect VRChat', exact: true });
  await expect(login.getByLabel('VRChat username', { exact: true })).toHaveValue('bob');
  await expect(picker).toHaveValue(alice.vrchatId);
  await login.getByRole('button', { name: 'Close dialog' }).click();
  await page.getByRole('button', { name: 'Add account', exact: true }).click();
  await expect(login.getByLabel('VRChat username', { exact: true })).toHaveValue('');
  await login.getByLabel('VRChat username', { exact: true }).fill('carol');
  await login.getByLabel('Password', { exact: true }).fill('synthetic passphrase');
  await login.getByRole('button', { name: 'Connect to VRChat', exact: true }).click();
  await expect(login.getByRole('heading', { name: 'Two-factor verification' })).toBeVisible();
  await expect(picker).toHaveValue(alice.vrchatId);
  await login.getByLabel('Verification code', { exact: true }).fill('123456');
  await login.getByRole('button', { name: 'Verify & connect', exact: true }).click();
  await expect(login).not.toBeVisible();
  await expect(picker).toHaveValue('usr_carol');
  await page.getByRole('button', { name: 'Manage accounts', exact: true }).click();
  expect(new URL(page.url()).pathname).toBe('/settings');
  const manager = page.getByRole('region', { name: 'VRChat accounts on this device', exact: true });
  await expect(manager.getByRole('listitem')).toHaveCount(3);
  await expect(manager.getByText('Reconnect required', { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('desktop-accounts.png'), fullPage: true });
  await manager.getByRole('button', { name: 'Remove Bob from this device', exact: true }).click();
  await expect(manager.getByRole('listitem')).toHaveCount(2);
  await expect(picker).toHaveValue('usr_carol');
  await page.goto('/tools');
  await expect(
    page.getByText('Available is not a guarantee. Banned names or blacklisted words may still be unusable.'),
  ).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
});

test('desktop settings check for GitHub releases and install verified updates', async ({ page }) => {
  let state = {
    currentVersion: 'v0.2.0',
    latestVersion: 'v0.2.0',
    releaseUrl: 'https://github.com/Sorganeil/vrcove/releases/tag/v0.3.0',
    available: false,
    checking: false,
    installing: false,
    installed: false,
    checkedAt: 0,
  };
  await page.route('**/api/account/device', (r) => r.fulfill({ json: { links: [] } }));
  await page.route('**/api/desktop/notifications', (r) =>
    r.fulfill({ json: { enabled: false, kinds: ['friendRequest', 'invite', 'requestInvite', 'response'] } }),
  );
  await page.route('**/api/desktop/update', async (r) => {
    if (r.request().method() === 'POST') {
      const action = r.request().postDataJSON().action;
      if (action === 'check') {
        state = { ...state, latestVersion: 'v0.3.0', available: true, checkedAt: Date.now() };
      } else if (action === 'install') {
        state = { ...state, available: false, installed: true };
      }
    }
    await r.fulfill({ json: state });
  });
  await page.goto('/settings');
  const panel = page.locator('.desktop-updates');
  await expect(panel.getByRole('heading', { name: 'App updates', exact: true })).toBeVisible();
  await expect(panel.getByText('Current version: v0.2.0', { exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Check for updates', exact: true }).click();
  await expect(panel.getByText('Update v0.3.0 is available.', { exact: true })).toBeVisible();
  await panel.getByRole('button', { name: 'Install update', exact: true }).click();
  await expect(
    panel.getByText('Update v0.3.0 is installed. Restart VRCove to use the new version.', { exact: true }),
  ).toBeVisible();
});
