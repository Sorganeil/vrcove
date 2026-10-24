import { test, expect, type Page } from '@playwright/test';
import { randomBytes, createCipheriv, createHmac, hkdfSync } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { tmpdir } from 'node:os';
import { DatabaseSync } from 'node:sqlite';

const origin = `http://localhost:${process.env.VRCOVE_TEST_FRONTEND_PORT || 15173}`;
const world = 'wrld_00000000-0000-0000-0000-000000000001';
const password = 'synthetic test passphrase';
const username = () => `test_${randomBytes(6).toString('hex')}`;

async function signup(page: Page) {
  // Give each disposable test account its own synthetic client address so
  // the full desktop/phone suite does not exhaust the real signup limiter.
  const groups = randomBytes(12)
    .toString('hex')
    .match(/.{1,4}/g)!;
  await page.setExtraHTTPHeaders({ 'CF-Connecting-IP': 'fd00:' + groups.join(':') + ':1' });
  const name = username();
  await page.goto('/#account');
  await page.getByRole('button', { name: 'Create account', exact: true }).click();
  await page.getByLabel('VRCove username').fill(name);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password').fill(password);
  await page.locator('form').getByRole('button', { name: 'Create account', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  return name;
}

// Provision a verified link only in the disposable test database. This keeps
// social tests on real SQLite without sending credentials to VRChat.
async function linkedSignup(page: Page) {
  const name = await signup(page);
  const fixture = JSON.parse(
    readFileSync(
      join(tmpdir(), `vrcove-ui-fixture-${process.env.VRCOVE_TEST_FRONTEND_PORT || '15173'}.json`),
      'utf8',
    ),
  );
  const db = new DatabaseSync(fixture.database);
  try {
    const account = db.prepare('SELECT id FROM accounts WHERE username = ?').get(name)!;
    const id = 'usr_' + randomBytes(16).toString('hex');
    const key = Buffer.from(fixture.accountKey, 'base64');
    const linkKey = Buffer.from(hkdfSync('sha256', key, Buffer.alloc(0), 'vrcove vrchat-link id', 32));
    const sealKey = Buffer.from(hkdfSync('sha256', key, Buffer.alloc(0), 'vrcove vrchat-link seal', 32));
    const iv = randomBytes(12),
      cipher = createCipheriv('aes-256-gcm', sealKey, iv);
    cipher.setAAD(Buffer.from('veil-link-v1'));
    const data = Buffer.concat([
      cipher.update(JSON.stringify({ vrchatId: id, displayName: 'Different VRChat Handle' })),
      cipher.final(),
      cipher.getAuthTag(),
    ]);
    const sealed =
      iv.toString('base64').replace(/=+$/, '') + '.' + data.toString('base64').replace(/=+$/, '');
    db.prepare('INSERT INTO vrchat_links (account_id,vrc_key,sealed,created) VALUES (?,?,?,?)').run(
      account.id as number,
      createHmac('sha256', linkKey).update(id).digest(),
      sealed,
      '2026-10-15',
    );
  } finally {
    db.close();
  }
  return name;
}

async function noOverflow(page: Page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

// All VRCove writes below use the real server and SQLite. Only upstream
// VRChat metadata is replaced, so no VRChat login or API action is performed.
async function mockWorld(page: Page) {
  await page.route('**/api/account/me', (r) =>
    r.fulfill({ json: { id: 'usr_test', displayName: 'Different VRChat Handle' } }),
  );
  await page.route('**/api/account/world?*', (r) =>
    r.fulfill({
      json: {
        id: world,
        name: 'Cozy Cabin',
        description: 'A synthetic test world',
        instances: [],
        platforms: [],
        tags: [],
      },
    }),
  );
  await page.route('**/api/account/user-worlds?*', (r) => r.fulfill({ json: [] }));
  await page.reload();
}

test('account signup, privacy, public profile and deletion', async ({ page, browser }, testInfo) => {
  const name = await signup(page);
  await expect(page.getByRole('checkbox', { name: 'Make my profile public' })).not.toBeChecked();
  await expect(page.getByText('Pick a name unrelated to your VRChat name.')).toBeVisible();
  const visitor = await browser.newContext();
  const hidden = await visitor.request.get(`${origin}/api/cove/profile?username=${name}`);
  expect(hidden.status()).toBe(404);
  await page.getByLabel('Bio', { exact: false }).fill('Independent world reviewer.');
  await page.getByRole('button', { name: 'Save profile' }).click();
  await expect(page.getByRole('button', { name: 'Save profile' })).toHaveCount(0);
  await page.getByRole('checkbox', { name: 'Make my profile public' }).check();
  await expect(page.getByRole('heading', { name: 'Profile is public' })).toBeVisible();
  await page.getByRole('link', { name: 'View my profile' }).click();
  await expect(page.locator('h1')).toHaveText(name);
  await expect(page.getByText('Independent world reviewer.')).toBeVisible();
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath('public-profile.png'), fullPage: true });
  const visible = await visitor.request.get(`${origin}/api/cove/profile?username=${name}`);
  expect(visible.status()).toBe(200);
  expect(await visible.json()).not.toHaveProperty('links');
  await page.goto('/#account');
  await page.getByRole('button', { name: 'Download my data' }).click();
  await page.getByLabel('Confirm with your password').fill(password);
  const download = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download', exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(new RegExp(`^vrcove-data-${name}-\\d{4}-\\d{2}-\\d{2}\\.json$`));
  const exported = JSON.parse(readFileSync(await file.path(), 'utf8'));
  expect(exported.account).toMatchObject({
    username: name,
    bio: 'Independent world reviewer.',
    publicProfile: true,
  });
  await page.getByRole('button', { name: 'Delete my account' }).click();
  await page.getByLabel('Confirm with your password').fill(password);
  await page.getByRole('button', { name: 'Delete permanently' }).click();
  await expect(page.getByRole('button', { name: 'Sign in', exact: true }).first()).toBeVisible();
  expect((await visitor.request.get(`${origin}/api/cove/profile?username=${name}`)).status()).toBe(404);
  await visitor.close();
});

test('changing the username renames the account and its public profile', async ({ page, browser }) => {
  const name = await signup(page);
  await page.getByRole('checkbox', { name: 'Make my profile public' }).check();
  await expect(page.getByRole('heading', { name: 'Profile is public' })).toBeVisible();
  const renamed = username();
  await page.getByRole('button', { name: 'Change username' }).click();
  await page.getByLabel('New username').fill(renamed);
  await page.getByLabel('Confirm with your password').fill('wrong passphrase');
  await page.locator('form').getByRole('button', { name: 'Change username' }).click();
  await expect(page.getByRole('alert')).toContainText('Your password is wrong.');
  await page.getByLabel('Confirm with your password').fill(password);
  await page.locator('form').getByRole('button', { name: 'Change username' }).click();
  await expect(page.getByRole('heading', { name: renamed, exact: true })).toBeVisible();
  await expect(page.getByLabel('New username')).toHaveCount(0);
  await noOverflow(page);
  const visitor = await browser.newContext();
  expect((await visitor.request.get(`${origin}/api/cove/profile?username=${renamed}`)).status()).toBe(200);
  expect((await visitor.request.get(`${origin}/api/cove/profile?username=${name}`)).status()).toBe(404);
  await visitor.close();
});

test('reviews keep the item name, survive edits, and link public authors', async ({ page, browser }) => {
  const name = await linkedSignup(page);
  await page.getByRole('checkbox', { name: 'Make my profile public' }).check();
  await expect(page.getByRole('heading', { name: 'Profile is public' })).toBeVisible();
  await mockWorld(page);
  await page.goto(`/#world/${world}`);
  await page.getByRole('radio', { name: '4 stars', exact: true }).click();
  await page.getByRole('textbox', { name: 'Comment', exact: true }).fill('Great lighting.');
  await page.getByRole('button', { name: 'Post review' }).click();
  await expect(page.getByText('Your review', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('radio', { name: '5 stars', exact: true }).click();
  await page.getByRole('button', { name: 'Save changes' }).click();
  await expect(page.locator('.review.mine').getByLabel('5.0 out of 5 stars')).toBeVisible();
  const visitor = await browser.newContext();
  const p = await visitor.newPage();
  await p.goto(`${origin}/#world/${world}`);
  await p.getByRole('link', { name, exact: false }).click();
  await expect(p.locator('h1')).toHaveText(name);
  await expect(p.getByRole('link', { name: 'Cozy Cabin', exact: true })).toHaveAttribute(
    'href',
    `/world/${world}`,
  );
  await expect(p.getByText('Great lighting.', { exact: true })).toBeVisible();
  await page.route('**/api/cove/review/delete', (r) =>
    r.fulfill({ status: 503, json: { error: 'Temporarily unavailable' } }),
  );
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('alert')).toHaveText('Temporarily unavailable');
  await page.unroute('**/api/cove/review/delete');
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Post review' })).toBeVisible();
  await visitor.close();
});

test('private collections, mobile dialog focus, sharing and removal', async ({ page, browser }, testInfo) => {
  await linkedSignup(page);
  await mockWorld(page);
  await page.goto(`/#world/${world}`);
  const trigger = page.getByRole('button', { name: 'Add to collection', exact: true });
  await trigger.click();
  const dialog = page.getByRole('dialog', { name: 'Add to collection' });
  await expect(dialog).toBeVisible();
  await dialog.getByRole('textbox', { name: 'New collection name' }).fill('Late night worlds');
  await dialog.getByRole('button', { name: 'Create & add' }).click();
  const choice = dialog.getByRole('checkbox', { name: /Late night worlds/ });
  await expect(choice).toBeChecked();
  await expect(choice).toContainText('1 items');
  await expect(dialog.getByRole('status')).toHaveText('Added to collection.');
  // Existing collections save directly; finishing never submits the empty
  // optional creation form or creates another collection.
  await dialog.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await trigger.click();
  await expect(choice).toBeChecked();
  await choice.click();
  await expect(choice).not.toBeChecked();
  await expect(dialog.getByRole('status')).toHaveText('Removed from collection.');
  let createRequests = 0;
  page.on('request', (r) => {
    if (new URL(r.url()).pathname === '/api/cove/collections/create') createRequests++;
  });
  await choice.click();
  await expect(choice).toBeChecked();
  await expect(dialog.getByRole('status')).toHaveText('Added to collection.');
  await dialog.getByRole('button', { name: 'Done', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(createRequests).toBe(0);
  await trigger.click();
  await expect(choice).toBeChecked();
  await expect(dialog.getByRole('textbox', { name: 'New collection name' })).toHaveValue('');
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath('collection-dialog.png'), fullPage: true });
  const box = await dialog.boundingBox();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  await dialog.getByRole('button', { name: 'Close dialog' }).focus();
  await page.keyboard.press('Shift+Tab');
  await expect(dialog.getByRole('button', { name: 'Done', exact: true })).toBeFocused();
  await page.keyboard.press('Tab');
  await expect(dialog.getByRole('button', { name: 'Close dialog' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(trigger).toBeFocused();
  await page.goto('/#collections');
  await page.getByRole('link', { name: /Late night worlds/ }).click();
  await expect(page.locator('h1')).toHaveText('Late night worlds');
  const id = page.url().split('/collection/')[1];
  const visitor = await browser.newContext();
  expect((await visitor.request.get(`${origin}/api/cove/collection?id=${id}`)).status()).toBe(404);
  await page.getByRole('button', { name: 'Edit', exact: true }).click();
  await page.getByRole('checkbox', { name: /Public: anyone/ }).check();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Copy link' })).toBeVisible();
  const shared = await visitor.request.get(`${origin}/api/cove/collection?id=${id}`);
  expect(shared.status()).toBe(200);
  const data = await shared.json();
  expect(data.owner).toBeNull();
  expect(data.items[0].id).toBe(world);
  await page.getByRole('button', { name: 'Remove Cozy Cabin' }).click();
  await expect(page.getByText('Nothing here yet')).toBeVisible();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByRole('button', { name: 'Delete for good' }).click();
  await expect(page.getByRole('heading', { name: 'My collections' })).toBeVisible();
  expect((await visitor.request.get(`${origin}/api/cove/collection?id=${id}`)).status()).toBe(404);
  await visitor.close();
});

test('profile pagination shows errors and retries without duplicating reviews', async ({ page }) => {
  let fail = true;
  const review = {
    kind: 'world',
    target: world,
    name: 'First world',
    body: 'Review one',
    rating: 4,
    date: '2026-10-15',
  };
  await page.route('**/api/cove/profile?*', (r) => {
    const offset = new URL(r.request().url()).searchParams.get('offset');
    if (offset && fail) {
      fail = false;
      return r.fulfill({ status: 503, json: { error: 'Try again shortly' } });
    }
    return r.fulfill({
      json: {
        username: 'public_reviewer',
        public: true,
        created: '2026-10-15',
        reviewCount: 2,
        averageGiven: 4,
        reviews: [offset ? { ...review, target: world.replace(/1$/, '2'), name: 'Second world' } : review],
        collections: [],
        pageSize: 1,
      },
    });
  });
  await page.goto('/#cove/public_reviewer');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('alert')).toHaveText('Try again shortly');
  await page.getByRole('button', { name: 'Load more' }).click();
  await expect(page.getByRole('link', { name: 'Second world', exact: true })).toBeVisible();
  await expect(page.locator('.review')).toHaveCount(2);
  await expect(page.getByRole('button', { name: 'Load more' })).toHaveCount(0);
});

test('guest actions require VRCove first and the header shows the VRCove identity', async ({ page }) => {
  await page.goto('/');
  await page.locator('.topbar').getByRole('button', { name: 'Sign in to VRCove', exact: true }).click();
  await expect(page.getByLabel('VRCove username')).toBeVisible();
  await expect(page.getByRole('dialog')).toHaveCount(0);
  const name = await signup(page);
  await expect(page.locator('.topbar .account-chip')).toHaveText(name);
  await expect(page.getByRole('button', { name: 'Link a VRChat account', exact: true })).toBeVisible();
  await page.reload();
  await expect(page.locator('.topbar .account-chip')).toHaveText(name);
  const denied = await page.request.post('/api/cove/collections/create', {
    headers: { Origin: origin },
    data: { kind: 'world', name: 'Unlinked' },
  });
  expect(denied.status()).toBe(403);
  // Phones sign out from the account page; the header keeps only the essentials.
  if (page.viewportSize()!.width < 650) {
    await page.goto('/#account');
    await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  } else await page.locator('.topbar').getByRole('button', { name: 'Sign out of VRCove' }).click();
  await expect(
    page.locator('.topbar').getByRole('button', { name: 'Sign in to VRCove', exact: true }),
  ).toBeVisible();
  await expect(page.locator('.account-chip')).toHaveCount(0);
});

test('VRChat connection links from the VRCove account and keeps its identity in the header', async ({
  page,
}) => {
  const name = await linkedSignup(page);
  const me = await (await page.request.get('/api/cove/me')).json();
  const vrc = { id: me.links[0].vrchatId, displayName: 'Different VRChat Handle' };
  let reconnected = false;
  await page.route('**/api/account/me', (r) =>
    reconnected
      ? r.fulfill({ json: vrc })
      : r.fulfill({ status: 401, json: { code: 'vrchat_session_required', error: 'Reconnect VRChat' } }),
  );
  await page.route('**/api/stats', (r) =>
    r.fulfill({ json: { accountsConfigured: true, loginConfigured: true, indexed: 0 } }),
  );
  await page.route('**/api/account/login', async (r) => {
    expect(r.request().postDataJSON()).toEqual({
      username: 'synthetic_vrc',
      password: 'synthetic password',
      expectedUserId: vrc.id,
    });
    reconnected = true;
    await r.fulfill({ json: { user: vrc } });
  });
  await page.reload();
  await page.locator('.cove-panel').getByRole('button', { name: 'Reconnect', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Connect VRChat', exact: true });
  await expect(dialog).toBeVisible();
  await dialog.getByLabel('VRChat username').fill('synthetic_vrc');
  await dialog.getByLabel('Password', { exact: true }).fill('synthetic password');
  await dialog.getByRole('button', { name: 'Connect to VRChat', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.locator('.topbar .account-chip')).toHaveText(name);
  await expect(
    page
      .locator('.cove-links li')
      .filter({ hasText: 'Different VRChat Handle' })
      .getByRole('button', { name: 'Active', exact: true }),
  ).toBeDisabled();
  await noOverflow(page);
});

test('VRCove profile pictures upload, persist, respect privacy and can be removed', async ({
  page,
  browser,
}, testInfo) => {
  const name = await signup(page);
  const input = page.getByLabel('Upload profile picture', { exact: true });
  const png = Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEklEQVR4nGMwXXvXdO1dBggFAC5SBv0CpzitAAAAAElFTkSuQmCC',
    'base64',
  );
  await input.setInputFiles({ name: 'picture.png', mimeType: 'image/png', buffer: png });
  await page.getByRole('button', { name: 'Use this picture', exact: true }).click();
  const header = page.locator('.topbar .account-chip img');
  await expect(header).toBeVisible();
  await expect(header).toHaveJSProperty('naturalWidth', 256);
  const chipBox = await page.locator('.topbar .account-chip').boundingBox();
  const imageBox = await header.boundingBox();
  expect(imageBox!.x).toBeGreaterThanOrEqual(chipBox!.x);
  expect(imageBox!.x + imageBox!.width).toBeLessThanOrEqual(chipBox!.x + chipBox!.width);
  await expect(page.getByRole('button', { name: 'Remove picture', exact: true })).toBeVisible();
  const picture = await header.getAttribute('src');
  await page.reload();
  await expect(header).toHaveAttribute('src', picture!);
  const visitor = await browser.newContext();
  expect((await visitor.request.get(origin + picture)).status()).toBe(404);
  await page.getByRole('checkbox', { name: 'Make my profile public' }).check();
  await expect(page.getByRole('heading', { name: 'Profile is public' })).toBeVisible();
  expect((await visitor.request.get(origin + picture)).status()).toBe(200);
  await page.getByRole('link', { name: 'View my profile' }).click();
  await expect(page.locator('.cove-profile-hero img')).toHaveAttribute('src', picture!);
  await expect(page.locator('h1')).toHaveText(name);
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath('profile-picture.png'), fullPage: true });
  await page.goto('/#account');
  await page
    .getByLabel('Upload profile picture', { exact: true })
    .setInputFiles({ name: 'invalid.png', mimeType: 'image/png', buffer: Buffer.from('invalid image') });
  await expect(page.getByRole('alert')).toContainText('This image could not be read.');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(header).toHaveAttribute('src', picture!);
  await page.getByRole('button', { name: 'Remove picture', exact: true }).click();
  await expect(header).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Remove picture', exact: true })).toHaveCount(0);
  expect((await visitor.request.get(origin + picture)).status()).toBe(404);
  await page.reload();
  await expect(header).toHaveCount(0);
  await visitor.close();
});

test('recovery codes reset a forgotten password and can only be used once', async ({ page }, testInfo) => {
  const name = await signup(page);
  await page.getByRole('button', { name: 'Generate recovery codes', exact: true }).click();
  await page.getByLabel('Confirm password for recovery codes').fill(password);
  await page.getByRole('button', { name: 'Generate recovery codes', exact: true }).click();
  const region = page.getByRole('region', { name: 'Your new recovery codes' });
  await expect(region.locator('code')).toHaveCount(10);
  const code = await region.locator('code').first().innerText();
  await expect(page.getByText('10 unused recovery codes remaining.', { exact: false })).toBeVisible();
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath('recovery-codes.png'), fullPage: true });
  const downloadPromise = page.waitForEvent('download');
  await page.getByRole('button', { name: 'Download codes', exact: true }).click();
  expect((await downloadPromise).suggestedFilename()).toBe('vrcove-recovery-codes.txt');
  await page.getByRole('button', { name: 'I saved my codes', exact: true }).click();
  await expect(region).toHaveCount(0);
  await page.getByRole('button', { name: 'Sign out', exact: true }).click();
  await page.getByRole('button', { name: 'Forgot password? Use a recovery code', exact: true }).click();
  await page.getByLabel('VRCove username').fill(name);
  await page.getByLabel('Recovery code', { exact: true }).fill(code);
  await page.getByLabel('New password', { exact: true }).fill('replacement test passphrase');
  await page.getByLabel('Confirm password', { exact: true }).fill('replacement test passphrase');
  await page.getByRole('button', { name: 'Reset password', exact: true }).click();
  await expect(
    page.getByRole('button', { name: 'Forgot password? Use a recovery code', exact: true }),
  ).toBeVisible();
  // Redeeming the same code cannot change the password a second time.
  const reused = await page.request.post('/api/cove/recover', {
    headers: { Origin: origin },
    data: { username: name, code, next: 'another test passphrase' },
  });
  expect(reused.status()).toBe(401);
  await page.getByLabel('Password', { exact: true }).fill('replacement test passphrase');
  await page.locator('form').getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  await expect(page.getByText('9 unused recovery codes remaining.', { exact: false })).toBeVisible();
  await expect(page.locator('.recovery-codes')).toHaveCount(0);
});

test('VRCove bio and status save independently of VRChat and respect profile visibility', async ({
  page,
  browser,
}, testInfo) => {
  const name = await signup(page);
  const bio = 'World explorer.\nAvatar collector.';
  const status = 'Looking for cozy worlds ☕';
  await page.getByLabel('Bio', { exact: false }).fill(bio);
  await page.getByLabel('VRCove status', { exact: false }).fill(status);
  // Changing visibility must not discard an unsaved bio or status.
  const published = page.waitForResponse(
    (r) => r.url().endsWith('/api/cove/profile') && r.request().method() === 'POST',
  );
  await page.getByRole('checkbox', { name: 'Make my profile public' }).check();
  await published;
  await expect(page.getByLabel('Bio', { exact: false })).toHaveValue(bio);
  await expect(page.getByLabel('VRCove status', { exact: false })).toHaveValue(status);
  let fail = true;
  await page.route('**/api/cove/profile', (r) => {
    if (fail) {
      fail = false;
      return r.fulfill({ status: 503, json: { error: 'Please try again' } });
    }
    return r.continue();
  });
  await page.getByRole('button', { name: 'Save profile', exact: true }).click();
  await expect(page.getByRole('alert')).toContainText('Please try again');
  await expect(page.getByLabel('Bio', { exact: false })).toHaveValue(bio);
  await expect(page.getByLabel('VRCove status', { exact: false })).toHaveValue(status);
  await page.getByRole('button', { name: 'Save profile', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save profile', exact: true })).toHaveCount(0);
  await page.reload();
  await expect(page.getByLabel('Bio', { exact: false })).toHaveValue(bio);
  await expect(page.getByLabel('VRCove status', { exact: false })).toHaveValue(status);
  await page.getByRole('link', { name: 'View my profile', exact: true }).click();
  await expect(page.locator('.cove-profile-status')).toHaveText(status);
  await expect(page.locator('.cove-bio')).toHaveText(bio);
  await expect(page.getByRole('link', { name: 'Edit VRCove profile', exact: true })).toBeVisible();
  await noOverflow(page);
  await page.screenshot({ path: testInfo.outputPath('vrcove-bio-status.png'), fullPage: true });
  const visitor = await browser.newContext();
  const publicProfile = await visitor.request.get(`${origin}/api/cove/profile?username=${name}`);
  expect(publicProfile.status()).toBe(200);
  expect(await publicProfile.json()).toMatchObject({ bio, status });
  await page.getByRole('link', { name: 'Edit VRCove profile', exact: true }).click();
  const hidden = page.waitForResponse(
    (r) => r.url().endsWith('/api/cove/profile') && r.request().method() === 'POST',
  );
  await page.getByRole('checkbox', { name: 'Make my profile public' }).uncheck();
  await hidden;
  expect((await visitor.request.get(`${origin}/api/cove/profile?username=${name}`)).status()).toBe(404);
  await page.getByLabel('Bio', { exact: false }).fill('');
  await page.getByLabel('VRCove status', { exact: false }).fill('');
  await page.getByRole('button', { name: 'Save profile', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Save profile', exact: true })).toHaveCount(0);
  await page.getByRole('link', { name: 'View my profile', exact: true }).click();
  await expect(page.locator('.cove-profile-status')).toHaveCount(0);
  await expect(page.locator('.cove-bio')).toHaveCount(0);
  await visitor.close();
});

test('profile pictures can be cropped before saving', async ({ page }, testInfo) => {
  await signup(page);
  // 200×100: red on the left half, blue on the right.
  const file = await page.evaluate(async () => {
    const canvas = document.createElement('canvas');
    canvas.width = 200;
    canvas.height = 100;
    const ctx = canvas.getContext('2d')!;
    ctx.fillStyle = '#f00';
    ctx.fillRect(0, 0, 100, 100);
    ctx.fillStyle = '#00f';
    ctx.fillRect(100, 0, 100, 100);
    return canvas.toDataURL('image/png').split(',')[1];
  });
  await page
    .getByLabel('Upload profile picture', { exact: true })
    .setInputFiles({ name: 'wide.png', mimeType: 'image/png', buffer: Buffer.from(file, 'base64') });
  const view = page.getByRole('application', { name: /Picture crop area/ });
  await expect(view.locator('img')).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
  await view.focus();
  for (let i = 0; i < 30; i++) await page.keyboard.press('ArrowRight');
  await page.getByLabel('Zoom', { exact: true }).fill('2');
  await page.screenshot({ path: testInfo.outputPath('picture-cropper.png') });
  await page.getByRole('button', { name: 'Use this picture', exact: true }).click();
  await expect(view).toHaveCount(0);
  const header = page.locator('.topbar .account-chip img');
  await expect(header).toHaveJSProperty('naturalWidth', 256);
  const pixel = await header.evaluate((img: HTMLImageElement) => {
    const canvas = document.createElement('canvas');
    canvas.width = canvas.height = 256;
    const ctx = canvas.getContext('2d')!;
    ctx.drawImage(img, 0, 0);
    return Array.from(ctx.getImageData(128, 128, 1, 1).data);
  });
  expect(pixel.slice(0, 3)).toEqual([0, 0, 255]);
});
