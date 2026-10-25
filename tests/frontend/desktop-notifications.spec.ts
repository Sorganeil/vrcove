import { test, expect } from '@playwright/test';

const user = 'usr_00000000-0000-0000-0000-000000000001';
const invite = {
  id: 'not_0123456789abcdef',
  type: 'invite',
  senderUsername: 'Mia',
  message: '',
  details: { worldName: 'Cozy room' },
};
const friendRequest = { id: 'not_fedcba9876543210', type: 'friendRequest', senderUsername: 'Zoe' };

test.beforeEach(async ({ page }) => {
  // A stand-in for the browser's notifications that records what would be shown.
  await page.addInitScript(() => {
    const shown: { title: string; body?: string }[] = [];
    (window as any).shown = shown;
    class FakeNotification {
      static permission = 'default';
      static requestPermission() {
        FakeNotification.permission = 'granted';
        return Promise.resolve('granted');
      }
      onclick: (() => void) | null = null;
      constructor(title: string, options: { body?: string } = {}) {
        shown.push({ title, body: options.body });
      }
      close() {}
    }
    (window as any).Notification = FakeNotification;
  });
  await page.route('**/api/cove/me', (r) =>
    r.fulfill({ json: { username: 'tester', links: [{ vrchatId: user }] } }),
  );
  await page.route('**/api/account/me', (r) => r.fulfill({ json: { id: user, displayName: 'Tester' } }));
  await page.route('**/api/account/notifications', (r) => r.fulfill({ json: [] }));
});

test('desktop notifications can be turned on and chosen by kind in Settings', async ({ page }) => {
  await page.route('**/api/account/live', (r) => r.fulfill({ status: 503, json: {} }));
  await page.goto('/settings');
  const panel = page.locator('.desktop-notifications');
  await expect(panel.getByRole('heading', { name: 'Desktop notifications' })).toBeVisible();
  await expect(panel.getByRole('button', { name: 'Send a test notification' })).toBeDisabled();
  await panel.getByLabel('Show VRChat notifications on this device').check();
  await expect(panel.getByLabel('Invites', { exact: true })).toBeChecked();
  await expect(panel.getByLabel('Other VRChat notifications')).not.toBeChecked();
  await panel.getByLabel('Friend requests').uncheck();
  await panel.getByRole('button', { name: 'Send a test notification' }).click();
  await expect.poll(() => page.evaluate(() => (window as any).shown)).toEqual([
    { title: 'Notifications are on', body: 'New VRChat notifications will show up like this.' },
  ]);
  const saved = await page.evaluate(() => JSON.parse(localStorage.getItem('vrcove-desktop-notifications')!));
  expect(saved).toEqual({ enabled: true, kinds: ['invite', 'requestInvite', 'response'] });
});

test('new notifications from the live feed show on the desktop, filtered by kind', async ({ page }) => {
  await page.addInitScript((id) => {
    (window as any).Notification.permission = 'granted';
    localStorage.setItem('vrcove-desktop-notifications', JSON.stringify({ enabled: true, kinds: ['invite'] }));
    localStorage.setItem('vrcove-notifications-seen:' + id, '[]');
  }, user);
  let live = 0;
  await page.route('**/api/account/live', (r) => {
    live++;
    return r.fulfill({
      headers: { 'Content-Type': 'text/event-stream' },
      body: `data: ${JSON.stringify(friendRequest)}\n\ndata: ${JSON.stringify(invite)}\n\ndata: ${JSON.stringify(invite)}\n\n`,
    });
  });
  await page.goto('/about');
  await expect.poll(() => page.evaluate(() => (window as any).shown)).toEqual([
    { title: 'Mia invited you', body: 'Cozy room' },
  ]);
  // The stream closing does not reconnect at once.
  await page.waitForTimeout(500);
  expect(live).toBe(1);
});
