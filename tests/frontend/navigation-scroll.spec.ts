import { test, expect } from '@playwright/test';

test('opening a section from the home page starts at the top', async ({ page }) => {
  const avatars = Array.from({ length: 36 }, (_, i) => ({
    id: `avtr_00000000-0000-0000-0000-${String(i).padStart(12, '0')}`,
    name: `Avatar ${i}`,
    author: 'Creator',
    description: '',
    image: '',
    thumbnail: '',
    pc: true,
    quest: false,
    rating: 'Good',
    tags: [],
    updated: '2026-01-01T00:00:00Z',
    created: '2026-01-01T00:00:00Z',
  }));
  await page.route('**/api/catalog?*', (r) => r.fulfill({ json: { avatars, total: 36, page: 0 } }));
  await page.goto('/#avatars');
  await expect(page.getByText('Avatar 35')).toBeAttached();
  await page.goto('/');
  const browse = page
    .getByRole('button', { name: 'Browse all' })
    .or(page.getByRole('button', { name: 'Browse avatars' }))
    .first();
  await expect(browse).toBeVisible();
  await page.evaluate(() => window.scrollTo(0, document.documentElement.scrollHeight));
  await browse.click();
  await expect(page).toHaveURL(/\/avatars$/);
  await expect(page.getByText('Avatar 35')).toBeAttached();
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
});
