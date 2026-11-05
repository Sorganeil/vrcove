import { test, expect } from '@playwright/test';

test.skip(process.env.VRCOVE_TEST_MODE !== 'desktop', 'Tools belong to the desktop app.');

test('tools preview, chatbox controls and extraction results work without signing in', async ({
  page,
}, testInfo) => {
  let settings = { template: '', status: '', port: 9000, interval: 10, sound: false };
  let automatic = false;
  const actions: string[] = [];
  await page.route('**/api/desktop/tools/chatbox', async (r) => {
    if (r.request().method() === 'POST') {
      const body = r.request().postDataJSON();
      actions.push(body.action);
      if (body.action === 'stop') automatic = false;
      else {
        settings = body;
        if (body.action === 'start') automatic = true;
      }
    }
    await r.fulfill({ json: { settings, automatic, error: '' } });
  });
  await page.route('**/api/desktop/tools/extract', async (r) => {
    expect(r.request().postDataJSON()).toEqual({ metadata: true });
    await r.fulfill({ json: { files: 3, bytes: 1024, folder: '/tmp/My-Avatar-extracted-demo' } });
  });
  await page.goto('/tools');
  await expect(page.getByRole('heading', { name: 'Tools', exact: true })).toBeVisible();
  await expect(page.getByRole('heading', { name: 'Unity package extractor' })).toBeVisible();
  const send = page.getByRole('button', { name: 'Send once', exact: true });
  await expect(send).toBeDisabled();
  // Long text is shortened to fit, the same way the app shortens it.
  await page.getByLabel('Message', { exact: true }).fill('word '.repeat(40));
  await expect(page.getByLabel('Message preview')).toContainText('…');
  await expect(page.getByLabel('Message preview')).toContainText(/1[34]\d\/144/);
  await page.getByLabel('Message', { exact: true }).fill('Hello from VRCove');
  await expect(page.getByLabel('Message preview')).toContainText('Hello from VRCove');
  await send.click();
  await expect(page.locator('[aria-labelledby="chatbox-title"]').getByRole('status')).toContainText(
    'Sent to VRChat',
  );
  await page.getByRole('button', { name: 'Time', exact: true }).click();
  await expect(page.getByLabel('Message', { exact: true })).toHaveValue('{time}');
  await expect(page.getByLabel('Message preview')).toContainText(/\d{2}:\d{2}/);
  await page.getByRole('button', { name: 'Start automatic messages' }).click();
  await expect(page.getByRole('button', { name: 'Stop automatic messages' })).toBeVisible();
  await expect(page.getByLabel('Message', { exact: true })).toBeDisabled();
  await page.getByRole('button', { name: 'Stop automatic messages' }).click();
  await expect(page.getByLabel('Message', { exact: true })).toBeEnabled();
  expect(actions).toEqual(['send', 'start', 'stop']);
  await page.getByRole('button', { name: 'Choose package and extract' }).click();
  await expect(page.getByText('Extracted 3 files')).toBeVisible();
  await expect(page.getByText('/tmp/My-Avatar-extracted-demo')).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
  await page.screenshot({ path: testInfo.outputPath('tools.png'), fullPage: true });
});

test('the chatbox shows live values and hides lines with nothing to show', async ({ page }) => {
  const asked: string[] = [];
  await page.route('**/api/desktop/tools/osc', (r) =>
    r.fulfill({
      json: { listening: false, port: 0, defaultPort: 9001, afk: false, avatarId: '', parameters: [] },
    }),
  );
  await page.route('**/api/desktop/tools/chatbox**', async (r) => {
    asked.push(new URL(r.request().url()).search);
    await r.fulfill({
      json: {
        settings: {
          template: '',
          status: '',
          port: 9000,
          interval: 10,
          sound: false,
          rotation: [],
          phrases: [],
        },
        automatic: false,
        error: '',
        values: { cpu: 'CPU 12%', ram: 'RAM 40%', world: 'The Black Cat', players: '12', playtime: '1h 05m' },
      },
    });
  });
  await page.goto('/tools');
  const message = page.getByLabel('Message', { exact: true });
  const preview = page.getByLabel('Message preview');
  await message.fill('{cpu} | {gpu} | {ram}');
  await expect(preview).toContainText('CPU 12%', { timeout: 6000 });
  await expect(preview).toContainText('CPU 12% | RAM 40%');
  await expect.poll(() => asked).toContain('?want=cpu,gpu,ram');
  await page.getByRole('button', { name: 'Now playing', exact: true }).click();
  await expect(preview.locator('span')).toHaveText(/^\d{2}:\d{2}$/);
  await page.getByRole('button', { name: 'Current world', exact: true }).click();
  await expect(preview.locator('span')).toContainText('The Black Cat (12)');
  await expect(preview.locator('span')).toContainText('1h 05m');
});

test('quick chat sends, signals typing and keeps saved phrases', async ({ page }) => {
  let phrases: string[] = ['brb'];
  const bodies: Array<Record<string, unknown>> = [];
  await page.route('**/api/desktop/tools/osc', (r) =>
    r.fulfill({
      json: { listening: false, port: 0, defaultPort: 9001, afk: false, avatarId: '', parameters: [] },
    }),
  );
  await page.route('**/api/desktop/tools/chatbox', async (r) => {
    if (r.request().method() === 'POST') {
      const body = r.request().postDataJSON();
      bodies.push(body);
      if (body.action === 'phrases') phrases = body.phrases;
    }
    await r.fulfill({
      json: {
        settings: { template: '', status: '', port: 9000, interval: 10, sound: false, rotation: [], phrases },
        automatic: false,
        error: '',
      },
    });
  });
  await page.goto('/tools');
  const quick = page.getByLabel('Say something');
  await quick.fill('Hello');
  await expect.poll(() => bodies.some((b) => b.action === 'typing' && b.typing === true)).toBe(true);
  await page.getByRole('button', { name: 'Save as a phrase' }).click();
  await expect(page.getByRole('button', { name: 'Hello', exact: true })).toBeVisible();
  await quick.press('Enter');
  await expect.poll(() => bodies.some((b) => b.action === 'say' && b.text === 'Hello')).toBe(true);
  await expect(quick).toHaveValue('');
  await page.getByLabel('Open in the VRChat keyboard to edit before sending').check();
  await page.getByRole('button', { name: 'brb', exact: true }).click();
  await expect
    .poll(() => bodies.some((b) => b.action === 'say' && b.text === 'brb' && b.open === true))
    .toBe(true);
  await page.getByRole('button', { name: 'Remove brb' }).click();
  await expect.poll(() => phrases).toEqual(['Hello']);
});

test('avatar parameters connect, filter and send values', async ({ page }) => {
  const sets: Array<{ name: string; value: unknown }> = [];
  let listening = false;
  const parameters: Array<Record<string, unknown>> = [
    { name: 'Glasses', type: 'Bool', writable: true, value: false },
    { name: 'Outfit', type: 'Int', writable: true, value: 2 },
    { name: 'Hue', type: 'Float', writable: true, value: 0.5 },
    { name: 'Velocity', type: 'Float', writable: false, value: 0.25 },
  ];
  await page.route('**/api/desktop/tools/chatbox', (r) =>
    r.fulfill({
      json: {
        settings: {
          template: '',
          status: '',
          port: 9000,
          interval: 10,
          sound: false,
          rotation: [],
          phrases: [],
        },
        automatic: false,
        error: '',
      },
    }),
  );
  await page.route('**/api/desktop/tools/osc', async (r) => {
    if (r.request().method() === 'POST') {
      const body = r.request().postDataJSON();
      if (body.action === 'listen') listening = true;
      if (body.action === 'stop') listening = false;
      if (body.action === 'set') {
        sets.push({ name: body.name, value: body.value });
        parameters.find((p) => p.name === body.name)!.value = body.value;
      }
    }
    await r.fulfill({
      json: {
        listening,
        port: listening ? 9001 : 0,
        defaultPort: 9001,
        afk: false,
        avatarId: listening ? 'avtr_00000000-0000-0000-0000-000000000001' : '',
        avatarName: listening ? 'Test avatar' : '',
        parameters: listening ? parameters : [],
      },
    });
  });
  await page.goto('/tools');
  const panel = page.locator('[aria-labelledby="osc-title"]');
  await panel.getByRole('button', { name: 'Connect to VRChat' }).click();
  await expect(panel.getByText('Test avatar')).toBeVisible();
  await expect(panel.getByText('Connected', { exact: true })).toBeVisible();
  // Read-only parameters stay out of the way until asked for.
  await expect(panel.getByText('Velocity')).toBeHidden();
  await panel.getByLabel('Show read-only parameters').check();
  await expect(panel.getByText('Velocity')).toBeVisible();
  await panel.getByLabel('Glasses').check();
  await expect.poll(() => sets).toContainEqual({ name: 'Glasses', value: true });
  await panel.getByLabel('Outfit').fill('300');
  await panel.getByLabel('Outfit').press('Enter');
  await expect.poll(() => sets).toContainEqual({ name: 'Outfit', value: 255 });
  await panel.getByRole('searchbox').fill('hu');
  await expect(panel.getByLabel('Hue')).toBeVisible();
  await expect(panel.getByLabel('Glasses')).toBeHidden();
  await panel.getByRole('searchbox').fill('nothing');
  await expect(panel.getByText('No parameters match.')).toBeVisible();
  await panel.getByRole('button', { name: 'Disconnect' }).click();
  await expect(panel.getByRole('button', { name: 'Connect to VRChat' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(
    page.viewportSize()!.width,
  );
});
