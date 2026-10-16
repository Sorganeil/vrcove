import { test, expect, type Page, type Route } from '@playwright/test';

const alpha = {
  id: 'usr_00000000-0000-0000-0000-000000000001',
  displayName: 'Alpha VRChat',
  status: 'active',
  bio: 'Alpha bio',
};
const beta = {
  id: 'usr_00000000-0000-0000-0000-000000000002',
  displayName: 'Beta VRChat',
  status: 'active',
  bio: 'Beta bio',
};
async function setup(page: Page) {
  let active = alpha;
  const expires = Date.now() + 3_600_000;
  let betaExpires = expires;
  let alphaExpires = expires;
  const headers = () => ({ 'X-VRChat-Session-Expires': String(expires), 'X-VRChat-Session-User': active.id });
  const account = () => ({
    username: 'cove_identity',
    created: '2026-10-15',
    bio: '',
    public: false,
    picture: '',
    links: [alpha, beta].map((u, i) => ({
      id: i + 1,
      vrchatId: u.id,
      displayName: u.displayName,
      username: i === 0 ? 'alpha_login' : 'beta_login',
      linked: '2026-10-15',
      active: active.id === u.id,
      sessionExpires: i === 1 ? betaExpires : alphaExpires,
    })),
  });
  await page.route('**/api/stats', (r) =>
    r.fulfill({ json: { accountsConfigured: true, loginConfigured: true, indexed: 0 } }),
  );
  await page.route('**/api/cove/me', (r) => r.fulfill({ json: account() }));
  await page.route('**/api/account/me', (r) => r.fulfill({ headers: headers(), json: active }));
  await page.route('**/api/account/user?*', (r) => {
    const id = new URL(r.request().url()).searchParams.get('id');
    return r.fulfill({ json: id === active.id ? active : id === alpha.id ? alpha : beta });
  });
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/user-groups?*', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/cove/reviews?*', (r) => r.fulfill({ status: 503, json: { error: 'disabled' } }));
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars: [], total: 0 } }));
  await page.route('**/api/account/switch', (r) => {
    active = r.request().postDataJSON().id === 1 ? alpha : beta;
    return r.fulfill({ headers: headers(), json: { user: active } });
  });
  return {
    get active() {
      return active;
    },
    set active(next) {
      active = next;
    },
    set betaExpires(next: number) {
      betaExpires = next;
    },
    set alphaExpires(next: number) {
      alphaExpires = next;
    },
    headers,
  };
}
const selector = (page: Page) => page.getByLabel('Active VRChat account', { exact: true });

test('multiple linked accounts switch active profiles and subsequent actions use the selected account', async ({
  page,
}, testInfo) => {
  const state = await setup(page);
  await page.route('**/api/account/profile', async (r) => {
    expect(r.request().headers()['x-vrchat-active-user']).toBe(state.active.id);
    state.active = { ...state.active, bio: r.request().postDataJSON().bio };
    await r.fulfill({ headers: state.headers(), json: state.active });
  });
  await page.goto('/#profile');
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(alpha.displayName);
  await selector(page).selectOption(beta.id);
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(beta.displayName);
  await expect(page.locator('.my-vrchat-profile .description')).toHaveText(beta.bio);
  await expect(page.locator('.account-chip')).toHaveText('cove_identity');
  await page.getByRole('button', { name: 'Edit profile', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit profile' });
  await dialog.getByLabel('Bio', { exact: false }).fill('Only Beta changes');
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.my-vrchat-profile .description')).toHaveText('Only Beta changes');
  await page.evaluate(() => {
    window.location.hash = '#account';
  });
  const row = page.locator('.cove-links li').filter({ hasText: alpha.displayName });
  await row.getByRole('button', { name: 'Use account', exact: true }).click();
  await expect(selector(page)).toHaveValue(alpha.id);
  await expect(row.getByRole('button', { name: 'Active', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Link another VRChat account' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath('linked-accounts.png'), fullPage: true });
  await page.getByRole('button', { name: 'Link another VRChat account' }).click();
  await expect(
    page.getByRole('dialog', { name: 'Connect VRChat' }).getByLabel('VRChat username'),
  ).toHaveValue('');
});

test('reconnecting a chosen account supports two-factor verification and keeps the current account until verified', async ({
  page,
}) => {
  const state = await setup(page);
  state.betaExpires = 0;
  let phase = 'wrong';
  await page.route('**/api/account/login', (r) => {
    expect(r.request().postDataJSON().expectedUserId).toBe(beta.id);
    return phase === 'wrong'
      ? r.fulfill({
          status: 409,
          json: {
            error:
              'Sign in to the selected VRChat account to reconnect it. Your active account has not changed.',
          },
        })
      : r.fulfill({ json: { requiresTwoFactorAuth: ['totp'] } });
  });
  await page.route('**/api/account/2fa', (r) => {
    state.active = beta;
    state.betaExpires = Date.now() + 3_600_000;
    return r.fulfill({ headers: state.headers(), json: { user: beta } });
  });
  await page.goto('/#account');
  await expect(selector(page)).toHaveValue(alpha.id);
  await page
    .locator('.cove-links li')
    .filter({ hasText: beta.displayName })
    .getByRole('button', { name: 'Reconnect', exact: true })
    .click();
  const dialog = page.getByRole('dialog', { name: 'Connect VRChat' });
  await expect(dialog.getByLabel('VRChat username')).toHaveValue('beta_login');
  await expect(dialog.getByLabel('Password', { exact: true })).toHaveValue('');
  await dialog.getByLabel('VRChat username').fill('wrong');
  await dialog.getByLabel('Password', { exact: true }).fill('synthetic password');
  await dialog.getByRole('button', { name: 'Connect to VRChat', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('active account has not changed');
  await expect(selector(page)).toHaveValue(alpha.id);
  phase = 'verify';
  await dialog.getByLabel('VRChat username').fill('beta');
  await dialog.getByLabel('Password', { exact: true }).fill('synthetic password');
  await dialog.getByRole('button', { name: 'Connect to VRChat', exact: true }).click();
  await expect(dialog.getByLabel('Verification code')).toBeVisible();
  await expect(selector(page)).toHaveValue(alpha.id);
  await dialog.getByLabel('Verification code').fill('123456');
  await dialog.getByRole('button', { name: 'Verify & connect', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(selector(page)).toHaveValue(beta.id);
  await expect(page.locator('.account-chip')).toHaveText('cove_identity');
});

test('an expired or invalid target does not disconnect the active account', async ({ page }) => {
  const state = await setup(page);
  await page.route('**/api/account/switch', (r) => {
    state.betaExpires = 0;
    return r.fulfill({
      status: 401,
      json: {
        code: 'vrchat_switch_reconnect',
        error: 'Beta session is no longer valid. Reconnect it. Your active account has not changed.',
      },
    });
  });
  await page.goto('/#profile');
  await expect(selector(page)).toHaveValue(alpha.id);
  await selector(page).selectOption(beta.id);
  await expect(page.locator('.toast')).toContainText('Your active account has not changed');
  await expect(selector(page)).toHaveValue(alpha.id);
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(alpha.displayName);
  await expect(page.locator('.vrchat-session-notice')).toHaveCount(0);
});

test('late authentication failures from the previous account do not invalidate the new active account', async ({
  page,
}) => {
  await setup(page);
  let held: Route | undefined;
  await page.route('**/api/account/friends?*', (r) => {
    held = r;
  });
  await page.goto('/#friends');
  await expect.poll(() => Boolean(held)).toBe(true);
  await page.evaluate(() => {
    window.location.hash = '#profile';
  });
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(alpha.displayName);
  await selector(page).selectOption(beta.id);
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(beta.displayName);
  const response = page.waitForResponse((r) => r.url().includes('/api/account/friends?'));
  await held!.fulfill({
    status: 401,
    headers: { 'X-VRChat-Session-User': alpha.id },
    json: { code: 'vrchat_session_invalid', error: 'Old session is invalid' },
  });
  await response;
  await expect(page.locator('.vrchat-session-notice')).toHaveCount(0);
  await expect(selector(page)).toHaveValue(beta.id);
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(beta.displayName);
});

test('a stale tab refreshes the active identity instead of writing to a different account', async ({
  page,
}) => {
  const state = await setup(page);
  let writes = 0;
  await page.route('**/api/account/profile', (r) => {
    if (r.request().headers()['x-vrchat-active-user'] !== state.active.id)
      return r.fulfill({
        status: 409,
        json: {
          code: 'vrchat_account_changed',
          error: 'Active account changed in another tab. Refresh before trying again.',
        },
      });
    writes++;
    return r.fulfill({ headers: state.headers(), json: state.active });
  });
  await page.goto('/#profile');
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(alpha.displayName);
  state.active = beta;
  await page.getByRole('button', { name: 'Edit profile', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Edit profile' });
  await dialog.getByRole('button', { name: 'Save changes', exact: true }).click();
  await expect(page.locator('.my-vrchat-profile .entity-hero h1')).toHaveText(beta.displayName);
  await expect(dialog).toHaveCount(0);
  expect(writes).toBe(0);
  await expect(selector(page)).toHaveValue(beta.id);
  await page.getByRole('button', { name: 'Edit profile', exact: true }).click();
  await page
    .getByRole('dialog', { name: 'Edit profile' })
    .getByRole('button', { name: 'Save changes', exact: true })
    .click();
  await expect(page.getByRole('dialog', { name: 'Edit profile' })).toHaveCount(0);
  expect(writes).toBe(1);
});

test('verified active sessions override stale link metadata and revoked sessions require reconnect', async ({
  page,
}) => {
  await page.clock.install();
  const state = await setup(page);
  state.alphaExpires = 0;
  state.betaExpires = 0;
  await page.goto('/#account');
  const row = page.locator('.cove-links li').filter({ hasText: alpha.displayName });
  await expect(row.getByRole('button', { name: 'Active', exact: true })).toBeDisabled();
  await expect(row).toContainText('Active session in this browser');
  await expect(row).not.toContainText('Reconnect to use this account');
  await expect(selector(page).locator('option', { hasText: alpha.displayName })).not.toContainText(
    'Reconnect required',
  );
  await expect(selector(page).locator('option', { hasText: beta.displayName })).toContainText(
    'Reconnect required',
  );
  // Cached future metadata must not conceal a known revoked active session.
  state.alphaExpires = Date.now() + 3_600_000;
  await page.reload();
  await expect(row.getByRole('button', { name: 'Active', exact: true })).toBeDisabled();
  await page.route('**/api/account/me', (r) =>
    r.fulfill({
      status: 401,
      headers: { 'X-VRChat-Session-User': alpha.id },
      json: { code: 'vrchat_session_invalid', error: 'Session revoked' },
    }),
  );
  // Returning to the tab rechecks the session at most once every 30 seconds.
  await page.clock.fastForward(30_001);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  await expect(page.locator('.vrchat-session-notice')).toContainText('VRChat session is no longer valid');
  await expect(row.getByRole('button', { name: 'Reconnect', exact: true })).toBeVisible();
  await expect(row.getByRole('button', { name: 'Active', exact: true })).toHaveCount(0);
  await expect(row).toContainText('Reconnect to use this account');
  await expect(selector(page).locator('option', { hasText: alpha.displayName })).toContainText(
    'Reconnect required',
  );
});

test('the session notice prefills the account that expired among multiple links', async ({ page }) => {
  await page.clock.install();
  await setup(page);
  await page.goto('/#account');
  await expect(selector(page)).toHaveValue(alpha.id);
  await page.route('**/api/account/me', (r) =>
    r.fulfill({
      status: 401,
      headers: { 'X-VRChat-Session-User': alpha.id },
      json: { code: 'vrchat_session_invalid', error: 'Session revoked' },
    }),
  );
  // Returning to the tab rechecks the session at most once every 30 seconds.
  await page.clock.fastForward(30_001);
  await page.evaluate(() => window.dispatchEvent(new Event('focus')));
  const notice = page.locator('.vrchat-session-notice');
  await expect(notice).toContainText('VRChat session is no longer valid');
  await notice.getByRole('button', { name: 'Reconnect VRChat', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect VRChat' });
  await expect(dialog.getByLabel('VRChat username')).toHaveValue('alpha_login');
  await expect(dialog.getByLabel('Password', { exact: true })).toHaveValue('');
  await dialog.getByRole('button', { name: 'Close', exact: false }).click();
  await page.getByRole('button', { name: 'Link another VRChat account' }).click();
  await expect(
    page.getByRole('dialog', { name: 'Connect VRChat' }).getByLabel('VRChat username'),
  ).toHaveValue('');
});
