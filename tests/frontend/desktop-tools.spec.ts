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
  await page.getByLabel('Message', { exact: true }).fill('A'.repeat(145));
  await expect(send).toBeDisabled();
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
