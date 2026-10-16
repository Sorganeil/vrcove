import { test, expect } from '@playwright/test';

test('registration waits for Turnstile and sends its token', async ({ page }) => {
  await page.route('**/api/stats', (r) =>
    r.fulfill({
      json: {
        indexed: 1,
        loginConfigured: true,
        accountsConfigured: true,
        turnstileSiteKey: 'synthetic-site-key',
      },
    }),
  );
  await page.route('**/api/cove/me', (r) => r.fulfill({ status: 401, json: { error: 'Sign in.' } }));
  // A stand-in for Cloudflare's script: solving is triggered by the test.
  await page.route('https://challenges.cloudflare.com/**', (r) =>
    r.fulfill({
      contentType: 'text/javascript',
      body: `window.turnstile={render(el,o){window.__solve=()=>o.callback('synthetic-token');window.__opts=o;el.textContent='challenge';return 'w1';},reset(){window.__resets=(window.__resets||0)+1;},remove(){}};`,
    }),
  );
  let sent: any = null;
  await page.route('**/api/cove/signup', async (r) => {
    sent = r.request().postDataJSON();
    await r.fulfill({ status: 400, json: { error: 'That username is taken.' } });
  });
  await page.goto('/#account');
  await page.getByRole('button', { name: 'Create account', exact: true }).click();
  await expect(page.locator('.turnstile-box')).toHaveText('challenge');
  expect(
    await page.evaluate(() => (window as any).__opts.sitekey + '/' + (window as any).__opts.action),
  ).toBe('synthetic-site-key/signup');
  await page.getByLabel('VRCove username').fill('turnstile_user');
  await page.getByLabel('Password', { exact: true }).fill('synthetic passphrase');
  await page.getByLabel('Confirm password').fill('synthetic passphrase');
  const submit = page.locator('form').getByRole('button', { name: 'Create account', exact: true });
  await submit.click();
  await expect(page.getByRole('alert')).toHaveText('Complete the verification check and try again.');
  expect(sent).toBeNull();
  await page.evaluate(() => (window as any).__solve());
  await submit.click();
  await expect(page.getByRole('alert')).toHaveText('That username is taken.');
  expect(sent).toMatchObject({ username: 'turnstile_user', turnstile: 'synthetic-token' });
  expect(await page.evaluate(() => (window as any).__resets)).toBe(1);
});
