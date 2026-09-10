import { test, expect } from './fixtures';

test.describe('Theme & Navigation Drawer', () => {
  test('switches themes and sets data-theme on html root without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/');

    // Click dark theme button
    const darkBtn = page.locator('button[data-theme-btn="dark"]');
    await darkBtn.click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');

    // Click light theme button
    const lightBtn = page.locator('button[data-theme-btn="light"]');
    await lightBtn.click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');

    // Click system/auto theme button
    const systemBtn = page.locator('button[data-theme-btn="system"]');
    await systemBtn.click();
    await expect(page.locator('html')).toHaveAttribute('data-pref', 'system');
  });

  test('toggles mobile drawer on smaller screens without errors', async ({ claraPage: page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/ui/');

    const drawerToggle = page.locator('#clara-drawer');
    await expect(drawerToggle).not.toBeChecked();

    const menuBtn = page.getByLabel('Open navigation menu');
    await menuBtn.click();
    await expect(drawerToggle).toBeChecked();
  });
});
