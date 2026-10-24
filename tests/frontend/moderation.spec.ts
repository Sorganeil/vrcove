import { test, expect, type Page } from '@playwright/test';
import { randomBytes, randomUUID, createCipheriv, createHmac, hkdfSync } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { tmpdir } from 'node:os';
import { DatabaseSync } from 'node:sqlite';

const origin = `http://localhost:${process.env.VRCOVE_TEST_FRONTEND_PORT || 15173}`;
const password = 'synthetic test passphrase';
const fixture = () =>
  JSON.parse(
    readFileSync(
      join(tmpdir(), `vrcove-ui-fixture-${process.env.VRCOVE_TEST_FRONTEND_PORT || '15173'}.json`),
      'utf8',
    ),
  );

function withDatabase<T>(path: string, fn: (db: DatabaseSync) => T): T {
  const db = new DatabaseSync(path);
  try {
    return fn(db);
  } finally {
    db.close();
  }
}

async function signup(page: Page) {
  const groups = randomBytes(12)
    .toString('hex')
    .match(/.{1,4}/g)!;
  await page.setExtraHTTPHeaders({ 'CF-Connecting-IP': 'fd00:' + groups.join(':') + ':1' });
  const name = `test_${randomBytes(6).toString('hex')}`;
  await page.goto('/account');
  await page.getByRole('button', { name: 'Create account', exact: true }).click();
  await page.getByLabel('VRCove username').fill(name);
  await page.getByLabel('Password', { exact: true }).fill(password);
  await page.getByLabel('Confirm password').fill(password);
  await page.locator('form').getByRole('button', { name: 'Create account', exact: true }).click();
  await expect(page.getByRole('heading', { name, exact: true })).toBeVisible();
  return name;
}

// A verified VRChat link exists only in the disposable test database.
function link(name: string) {
  const { database, accountKey } = fixture();
  withDatabase(database, (db) => {
    const account = db.prepare('SELECT id FROM accounts WHERE username = ?').get(name)!;
    const id = 'usr_' + randomUUID();
    const key = Buffer.from(accountKey, 'base64');
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
      '2026-10-17',
    );
  });
}

const setRole = (name: string, role: string) =>
  withDatabase(fixture().database, (db) =>
    db.prepare('UPDATE accounts SET role = ? WHERE username = ?').run(role, name),
  );

const send = (page: Page, path: string, data: object) =>
  page.request.post(origin + '/api' + path, { data, headers: { Origin: origin } });

async function openMenu(page: Page, isMobile: boolean) {
  if (isMobile) await page.getByRole('button', { name: 'Open navigation' }).click();
}

test('reports reach the hidden admin page, which can delete reviews and ban accounts', async ({
  page,
  browser,
  isMobile,
}) => {
  const world = 'wrld_' + randomUUID();
  const author = await signup(page);
  link(author);
  expect(
    (await send(page, '/cove/profile', { public: true, bio: 'Buy followers at example.test' })).status(),
  ).toBe(200);
  expect(
    (
      await send(page, '/cove/review', {
        kind: 'world',
        id: world,
        rating: 1,
        body: 'Insulting comment',
        name: 'Cabin',
      })
    ).status(),
  ).toBe(200);

  // Another signed-in account reports the review and the profile.
  const reporterContext = await browser.newContext();
  const reporter = await reporterContext.newPage();
  await signup(reporter);
  await reporter.route('**/api/account/world?*', (r) =>
    r.fulfill({
      json: { id: world, name: 'Cabin', description: '', instances: [], platforms: [], tags: [] },
    }),
  );
  await reporter.goto(`/world/${world}`);
  await reporter
    .locator('.review')
    .filter({ hasText: 'Insulting comment' })
    .getByRole('button', { name: 'Report review' })
    .click();
  const dialog = reporter.getByRole('dialog', { name: 'Report review' });
  await dialog.getByRole('button', { name: 'Send report' }).click();
  await expect(dialog.getByRole('alert')).toHaveText('Choose a reason.');
  await dialog.getByRole('radio', { name: 'Harassment or bullying' }).check();
  await dialog.getByRole('textbox').fill('Targets the world creator.');
  await dialog.getByRole('button', { name: 'Send report' }).click();
  await expect(dialog.getByText('Thanks. Moderators will review your report.')).toBeVisible();
  await dialog.getByRole('button', { name: 'Close', exact: true }).click();
  await reporter.goto(`/cove/${author}`);
  await reporter.getByRole('button', { name: 'Report profile' }).click();
  await reporter.getByRole('dialog').getByRole('radio', { name: 'Spam or advertising' }).check();
  await reporter.getByRole('dialog').getByRole('button', { name: 'Send report' }).click();
  await expect(reporter.getByRole('dialog').getByText('Thanks.', { exact: false })).toBeVisible();

  // Regular accounts never see the panel.
  await reporter.goto('/admin');
  await expect(reporter.getByText('This page doesn’t exist.')).toBeVisible();
  await openMenu(reporter, isMobile);
  await expect(reporter.getByRole('link', { name: 'Moderation' })).toHaveCount(0);
  await reporterContext.close();

  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  const adminName = await signup(admin);
  setRole(adminName, 'owner');
  await admin.reload();
  await openMenu(admin, isMobile);
  await admin.getByRole('link', { name: 'Moderation' }).click();
  await expect(admin).toHaveURL(/\/admin$/);
  await expect(admin.getByRole('tab', { name: /Reports/ })).toBeVisible();
  const reviewCard = admin.locator('.admin-card').filter({ hasText: 'Insulting comment' });
  await expect(reviewCard).toContainText('Harassment or bullying');
  await expect(reviewCard).toContainText('Targets the world creator.');
  await reviewCard.getByRole('button', { name: 'Delete review' }).click();
  await expect(reviewCard).toHaveCount(0);
  const profileCard = admin.locator('.admin-card').filter({ hasText: 'Buy followers' });
  await profileCard.getByRole('button', { name: 'Dismiss' }).click();
  await expect(admin.getByText('No open reports. Nice.')).toBeVisible();
  expect(await admin.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
  const reviews = await (await admin.request.get(`${origin}/api/cove/reviews?kind=world&id=${world}`)).json();
  expect(reviews.count).toBe(0);

  await admin.getByRole('tab', { name: /Users/ }).click();
  await admin.getByLabel('Search usernames').fill(author);
  await admin.locator('.admin-search').getByRole('button', { name: 'Search' }).click();
  const user = admin.locator('.admin-user').filter({ hasText: author });
  admin.once('dialog', (d) => d.accept());
  await user.getByRole('button', { name: 'Ban' }).click();
  await expect(user.getByText('Banned', { exact: true })).toBeVisible();
  expect((await admin.request.get(`${origin}/api/cove/profile?username=${author}`)).status()).toBe(404);
  // The banned account is signed out everywhere and can't sign back in.
  expect((await page.request.get(`${origin}/api/cove/me`)).status()).toBe(401);
  const retry = await send(page, '/cove/login', { username: author, password });
  expect(retry.status()).toBe(403);
  expect((await retry.json()).error).toBe('This account has been banned.');
  await admin.getByRole('tab', { name: /Activity/ }).click();
  await expect(admin.locator('.admin-log')).toContainText('ban');
  await adminContext.close();
});

test('avatar creators can request removal and admins see it in the backlog', async ({ page, browser }) => {
  const avatar = 'avtr_' + randomUUID();
  const catalog = join(dirname(fixture().database), 'catalog.sqlite');
  withDatabase(catalog, (db) =>
    db
      .prepare('INSERT INTO catalog (id,name,author,updated,pc,quest,rating,data) VALUES (?,?,?,?,?,?,?,?)')
      .run(
        avatar,
        'Removal Test Avatar',
        'Creator',
        '2026-10-01T00:00:00Z',
        1,
        0,
        'Good',
        JSON.stringify({
          id: avatar,
          name: 'Removal Test Avatar',
          author: 'Creator',
          authorId: 'usr_' + randomUUID(),
          description: '',
          image: '',
          thumbnail: '',
          pc: true,
          quest: false,
          rating: 'Good',
          tags: [],
          updated: '2026-10-01T00:00:00Z',
          created: '2026-10-01T00:00:00Z',
        }),
      ),
  );
  await signup(page);
  await page.goto(`/avatar/${avatar}`);
  await page.getByRole('button', { name: 'Your avatar? Request removal' }).click();
  const dialog = page.getByRole('dialog', { name: 'Request removal' });
  await dialog.getByLabel('Proof of ownership').fill('short');
  await dialog.getByRole('button', { name: 'Send request' }).click();
  await expect(dialog.getByRole('alert')).toContainText('Explain how you can prove');
  await dialog
    .getByLabel('Proof of ownership')
    .fill('I sell this avatar on my store page at example.test/shop.');
  await dialog.getByRole('button', { name: 'Send request' }).click();
  await expect(dialog.getByText('Request sent. Moderators will check your proof.')).toBeVisible();

  const adminContext = await browser.newContext();
  const admin = await adminContext.newPage();
  const adminName = await signup(admin);
  setRole(adminName, 'admin');
  await admin.goto('/admin');
  await admin.getByRole('tab', { name: /Removal requests/ }).click();
  const card = admin.locator('.admin-card').filter({ hasText: avatar });
  await expect(card).toContainText('Unverified');
  await expect(card).toContainText('example.test/shop');
  await card.getByLabel('Note (optional)').fill('No proof on the store page.');
  await card.getByRole('button', { name: 'Reject' }).click();
  await expect(card).toHaveCount(0);
  await admin.getByRole('button', { name: 'Handled' }).click();
  await expect(admin.locator('.admin-card').filter({ hasText: avatar })).toContainText('rejected');
  // Admins can't promote: only owners see role controls.
  await admin.getByRole('tab', { name: /Users/ }).click();
  await expect(admin.getByRole('button', { name: 'Make admin' })).toHaveCount(0);
  await adminContext.close();
});

test('staff can remove an avatar directly from the removal tab', async ({ page }) => {
  const adminName = await signup(page);
  setRole(adminName, 'admin');
  await page.goto('/admin');
  await page.getByRole('tab', { name: /Removal requests/ }).click();
  const form = page.getByRole('form', { name: 'Remove an avatar' });
  const button = form.getByRole('button', { name: 'Remove avatar' });
  await form.getByLabel('Avatar ID or link').fill('not an avatar');
  await expect(form.getByText('That isn’t an avatar ID or link.')).toBeVisible();
  await expect(button).toBeDisabled();
  await form.getByLabel('Avatar ID or link').fill(`${origin}/avatar/avtr_${randomUUID()}`);
  await form.getByLabel('Reason').fill('Advertises a website');
  await expect(button).toBeEnabled();
  page.once('dialog', (d) => d.accept());
  await button.click();
  // The test server has no catalog writer, so the request reaches the server and is refused there.
  await expect(page.getByText('Catalog changes are not enabled on this deployment.')).toBeVisible();
});
