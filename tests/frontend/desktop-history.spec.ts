import { test, expect } from '@playwright/test';

test.skip(process.env.VRCOVE_TEST_MODE !== 'desktop', 'History and Feed belong to the desktop app.');

const onePixelPng = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==',
  'base64',
);

test('history shows world banners, instance badges and highlighted clickable links', async ({
  page,
}, testInfo) => {
  const worldId = 'wrld_4432ea9b-729c-46e3-8eaf-846aa0a37fdd';
  const user1 = 'usr_00000000-0000-0000-0000-000000000011';
  const user2 = 'usr_00000000-0000-0000-0000-000000000012';

  await page.route('**/api/image**', (r) => r.fulfill({ contentType: 'image/png', body: onePixelPng }));
  await page.route('**/api/account/world**', (r) =>
    r.fulfill({
      json: {
        id: worldId,
        name: 'Popcorn Palace',
        authorName: 'lubu',
        thumbnailImageUrl: 'https://api.vrchat.cloud/api/1/image/file_world/1/file',
        imageUrl: 'https://api.vrchat.cloud/api/1/image/file_world/1/file',
      },
    }),
  );
  await page.route('**/api/desktop/history**', (r) =>
    r.fulfill({
      json: {
        folder: '/home/user/.local/share/VRChat',
        visits: [
          {
            id: 1,
            location: `${worldId}:12345~region(us)`,
            worldId,
            worldName: 'Popcorn Palace',
            joined: Date.parse('2026-10-23T20:27:00Z'),
            left: Date.parse('2026-10-23T21:14:00Z'),
            people: [
              {
                userId: user1,
                displayName: 'metamartian',
                joined: Date.parse('2026-10-23T20:28:00Z'),
                avatar: 'Cunk MAYBE UNK',
              },
              {
                userId: user2,
                displayName: 'Petrichorazi',
                joined: Date.parse('2026-10-23T20:29:00Z'),
                avatar: 'Cunk Minecraft UNK',
              },
              {
                displayName: 'GuestOnly',
                joined: Date.parse('2026-10-23T20:30:00Z'),
              },
            ],
          },
        ],
      },
    }),
  );

  await page.goto('/history');
  await expect(page.getByRole('heading', { name: 'History', exact: true })).toBeVisible();
  const visit = page.locator('.history-visit').first();
  await expect(visit).toBeVisible();
  await expect(visit.locator('.history-world-banner.is-clickable img')).toBeVisible();
  await expect(visit.locator('.entity-link.world-link')).toContainText('Popcorn Palace');
  await expect(visit.locator('.history-badge')).toContainText(['47m', 'Public', 'US West', '3 people']);
  await expect(visit.locator('.history-person.is-clickable')).toHaveCount(2);
  await expect(visit.locator('.history-person:not(.is-clickable)')).toHaveCount(1);
  await expect(page.getByRole('button', { name: 'Load more' })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath('history.png'), fullPage: true });
});

test('feed resolves raw user IDs to usernames and highlights clickable elements', async ({
  page,
}, testInfo) => {
  const unresolvedId = 'usr_a9a88016-df1c-4cdb-b1f5-072a41e6b7ce';
  const friendId = 'usr_00000000-0000-0000-0000-000000000022';
  const worldId = 'wrld_4432ea9b-729c-46e3-8eaf-846aa0a37fdd';

  await page.route('**/api/image**', (r) => r.fulfill({ contentType: 'image/png', body: onePixelPng }));
  await page.route('**/api/account/friends**', (r) => r.fulfill({ json: [] }));
  await page.route('**/api/account/user**', (r) => {
    const url = new URL(r.request().url());
    const id = url.searchParams.get('id');
    if (id === unresolvedId) {
      return r.fulfill({
        json: {
          id: unresolvedId,
          displayName: 'NovaStar',
          currentAvatarThumbnailImageUrl: 'https://api.vrchat.cloud/api/1/image/file_user/1/file',
        },
      });
    }
    return r.fulfill({
      json: {
        id: friendId,
        displayName: 'Kyrillk',
        currentAvatarThumbnailImageUrl: 'https://api.vrchat.cloud/api/1/image/file_user/2/file',
      },
    });
  });
  await page.route('**/api/account/world**', (r) =>
    r.fulfill({
      json: {
        id: worldId,
        name: 'Popcorn Palace',
        thumbnailImageUrl: 'https://api.vrchat.cloud/api/1/image/file_world/1/file',
      },
    }),
  );
  await page.route('**/api/desktop/feed**', (r) =>
    r.fulfill({
      json: {
        entries: [
          {
            id: 1,
            at: Date.parse('2026-10-25T21:53:00Z'),
            userId: unresolvedId,
            displayName: unresolvedId,
            kind: 'offline',
          },
          {
            id: 2,
            at: Date.parse('2026-10-25T21:40:00Z'),
            userId: friendId,
            displayName: 'Kyrillk',
            kind: 'location',
            detail: `${worldId}:54321~hidden(usr_x)~region(eu)`,
            worldName: 'Popcorn Palace',
          },
          {
            id: 3,
            at: Date.parse('2026-10-25T21:30:00Z'),
            userId: friendId,
            displayName: 'Kyrillk',
            kind: 'status',
            detail: 'join me movie night!',
          },
        ],
      },
    }),
  );

  await page.goto('/feed');
  await expect(page.getByRole('heading', { name: 'Feed', exact: true })).toBeVisible();
  const list = page.locator('.feed-list');
  await expect(list).toContainText('NovaStar');
  await expect(list).not.toContainText(unresolvedId);
  await expect(list.locator('.entity-link.user-link').first()).toContainText('NovaStar');
  await expect(list.locator('.entity-link.world-link')).toContainText('Popcorn Palace');
  await expect(list.locator('.feed-status-pill')).toContainText('Join me');
  await expect(list.locator('.feed-status-pill')).toContainText('movie night!');
  await expect(page.getByRole('button', { name: 'Load more' })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath('feed.png'), fullPage: true });
});
