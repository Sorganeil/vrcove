import { test, expect } from '@playwright/test';

test.skip(process.env.VRCOVE_TEST_MODE !== 'desktop', 'Tools belong to the desktop app.');

test('single, pasted and file input enforce 100 names and show results and cooldowns', async ({
  page,
}, testInfo) => {
  await page.route('**/api/account/me', (r) =>
    r.fulfill({ json: { id: 'usr_test', username: 'tester', displayName: 'Tester', friends: [] } }),
  );
  await page.route('**/api/account/device', (r) =>
    r.fulfill({
      json: {
        links: [
          {
            id: 1,
            vrchatId: 'usr_test',
            username: 'tester',
            displayName: 'Tester',
            active: true,
            sessionExpires: Date.now() + 86400000,
          },
        ],
      },
    }),
  );
  await page.route('**/api/desktop/tools/chatbox', (r) =>
    r.fulfill({
      json: {
        settings: { template: '', status: '', port: 9000, interval: 10, sound: false },
        automatic: false,
        error: '',
      },
    }),
  );
  let snapshot = {
    running: false,
    phase: 'idle',
    rows: [] as { username: string; status: string }[],
    nextAt: 0,
    waitReason: '',
    error: '',
  };
  const batches: string[][] = [];
  await page.route('**/api/desktop/tools/usernames', async (r) => {
    if (r.request().method() === 'POST') {
      const body = r.request().postDataJSON();
      if (body.action === 'stop')
        snapshot = { ...snapshot, running: false, phase: 'stopped', nextAt: 0, waitReason: '' };
      else {
        batches.push(body.usernames);
        snapshot = {
          running: true,
          phase: 'running',
          rows: body.usernames.map((username: string) => ({ username, status: 'queued' })),
          nextAt: Date.now() + 60000,
          waitReason: 'rate-limit',
          error: '',
        };
      }
    }
    await r.fulfill({ json: snapshot });
  });
  await page.goto('/tools');
  const tool = page.locator('[aria-labelledby="username-check-title"]');
  const input = tool.getByLabel('Usernames', { exact: true });
  const start = tool.getByRole('button', { name: 'Check usernames', exact: true });
  await expect(start).toBeDisabled();
  await input.fill('single');
  await expect(start).toBeEnabled();
  await start.click();
  expect(batches).toEqual([['single']]);
  await expect(input).toBeDisabled();
  await expect(tool.getByText(/VRChat rate limit. Retrying in/)).toBeVisible();
  await tool.getByRole('button', { name: 'Stop checking', exact: true }).click();
  await expect(tool.getByText('Check stopped', { exact: true })).toBeVisible();
  await input.fill(Array.from({ length: 101 }, (_, i) => 'name' + i).join('\n'));
  await expect(start).toBeDisabled();
  await expect(tool.getByRole('alert')).toContainText('Enter between 1 and 100 usernames.');
  await input.fill('available_name, taken_name\ninvalid name\nunknown_name\navailable_name');
  await expect(tool.getByText('4/100 usernames')).toBeVisible();
  await start.click();
  expect(batches[1]).toEqual(['available_name', 'taken_name', 'invalid name', 'unknown_name']);
  snapshot = {
    ...snapshot,
    running: false,
    phase: 'complete',
    nextAt: 0,
    waitReason: '',
    rows: snapshot.rows.map((row, i) => ({
      ...row,
      status: ['available', 'taken', 'invalid', 'unconfirmed'][i],
    })),
  };
  await expect(tool.getByText('Check complete', { exact: true })).toBeVisible();
  for (const label of ['Available', 'Taken', 'Invalid username', 'Unconfirmed'])
    await expect(tool.getByText(label, { exact: true })).toBeVisible();
  await expect(tool.getByText(/Unconfirmed means/)).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath('username-checker.png'), fullPage: true });
  await tool.getByLabel('Username list file').setInputFiles({
    name: 'names.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from('\uFEFFfile_one\r\nfile_two\r\nfile_one'),
  });
  await expect(tool.getByText('2/100 usernames')).toBeVisible();
  await start.click();
  expect(batches[2]).toEqual(['file_one', 'file_two']);
  await tool.getByRole('button', { name: 'Stop checking', exact: true }).click();
  await tool.getByLabel('Username list file').setInputFiles({
    name: 'too-many.txt',
    mimeType: 'text/plain',
    buffer: Buffer.from(Array.from({ length: 101 }, (_, i) => 'name' + i).join('\n')),
  });
  await expect(start).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
});

test('checker requires VRChat sign-in and rejects oversized files locally', async ({ page }) => {
  let starts = 0;
  await page.route('**/api/desktop/tools/usernames', async (r) => {
    if (r.request().method() === 'POST') starts++;
    await r.fulfill({
      json: { running: false, phase: 'idle', rows: [], error: '', waitReason: '', nextAt: 0 },
    });
  });
  await page.goto('/tools');
  const tool = page.locator('[aria-labelledby="username-check-title"]');
  await tool.getByLabel('Usernames', { exact: true }).fill('tester');
  await expect(tool.getByRole('button', { name: 'Check usernames', exact: true })).toBeDisabled();
  await expect(tool.getByRole('button', { name: 'Sign in to VRChat', exact: true })).toBeVisible();
  await tool
    .getByLabel('Username list file')
    .setInputFiles({ name: 'large.txt', mimeType: 'text/plain', buffer: Buffer.alloc(65537, 'a') });
  await expect(tool.getByRole('alert')).toContainText('Choose a text file smaller than 64 KB.');
  expect(starts).toBe(0);
});
