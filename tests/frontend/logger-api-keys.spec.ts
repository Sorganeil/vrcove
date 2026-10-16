import { test, expect, type Page } from '@playwright/test';

async function setup(page: Page, connected = true) {
  const user = {
    id: 'usr_00000000-0000-0000-0000-000000000001',
    displayName: 'Example VRChat account',
    status: 'active',
  };
  const expires = Date.now() + 3600000;
  let keys: { id: number; vrchatId: string; expires: number; browser: string; device: string }[] = [];
  await page.route('**/api/stats', (r) =>
    r.fulfill({ json: { accountsConfigured: true, loginConfigured: true, indexed: 0 } }),
  );
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({
      json: {
        username: 'companion_owner',
        created: '2026-10-15',
        bio: '',
        public: false,
        picture: '',
        links: [
          {
            id: 1,
            vrchatId: user.id,
            displayName: user.displayName,
            linked: '2026-10-15',
            sessionExpires: connected ? expires : undefined,
          },
        ],
      },
    }),
  );
  await page.route('**/api/account/me', (r) =>
    connected
      ? r.fulfill({
          headers: { 'X-VRChat-Session-Expires': String(expires), 'X-VRChat-Session-User': user.id },
          json: user,
        })
      : r.fulfill({ status: 401, json: { code: 'vrchat_session_required', error: 'Reconnect VRChat.' } }),
  );
  await page.route('**/api/cove/logger-keys', (r) => {
    if (r.request().method() === 'GET') return r.fulfill({ json: { enabled: true, keys } });
    expect(r.request().postDataJSON()).toEqual({ vrchatId: user.id });
    keys = [{ id: 7, vrchatId: user.id, expires, browser: 'Firefox', device: 'computer' }];
    return r.fulfill({ json: { key: 'vrcove_logger_' + 'x'.repeat(43), id: 7, expires, vrchatId: user.id } });
  });
  await page.route('**/api/cove/logger-keys/revoke', (r) => {
    expect(r.request().postDataJSON()).toEqual({ id: 7 });
    keys = [];
    return r.fulfill({ json: { ok: true } });
  });
  await page.goto('/account');
  return page.getByRole('region', { name: 'Companion API keys' });
}

test('key generation discloses encrypted session storage and lifetime, then supports revocation', async ({
  page,
}, info) => {
  const panel = await setup(page);
  await expect(panel).toContainText('existing server-side session record');
  await expect(panel).toContainText('while your browser is closed');
  await expect(panel).toContainText('expires when the VRChat session expires or ends');
  await expect(panel).not.toContainText('operator can decrypt');
  await panel.getByRole('button', { name: 'Generate API key', exact: true }).click();
  await expect(panel.getByLabel('Copy this API key now. It is shown only once.')).toHaveValue(
    'vrcove_logger_' + 'x'.repeat(43),
  );
  await expect(panel).toContainText('Expires with your VRChat session:');
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await panel.screenshot({ path: info.outputPath('companion-api-key.png') });
  await panel.getByRole('button', { name: 'Hide API key', exact: true }).click();
  await expect(panel.locator('textarea')).toHaveCount(0);
  await panel.getByRole('button', { name: 'Revoke API key', exact: true }).click();
  await expect(panel.getByRole('button', { name: 'Revoke API key', exact: true })).toHaveCount(0);
});

test('a disconnected VRChat session cannot generate an API key', async ({ page }) => {
  const panel = await setup(page, false);
  await expect(panel.getByRole('button', { name: 'Generate API key', exact: true })).toBeDisabled();
  await expect(panel).toContainText('Connect a linked VRChat account');
});
