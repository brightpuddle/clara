import { test, expect } from './fixtures';

test.describe('Observability Logs Page', () => {
  test('renders log stream console and level filter buttons without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/logs');

    await expect(page.locator('h1')).toContainText('Agent Observability');
    await expect(page.locator('#log-output')).toBeVisible();
    await expect(page.getByText('Console Output')).toBeVisible();

    // Test clicking a level filter button
    const infoFilterBtn = page.locator('a:has-text("Info")');
    if (await infoFilterBtn.isVisible()) {
      await infoFilterBtn.click();
      await expect(page).toHaveURL(/\/ui\/logs\?level=info/);
      await expect(page.locator('#log-output')).toBeVisible();
    }
  });
});
