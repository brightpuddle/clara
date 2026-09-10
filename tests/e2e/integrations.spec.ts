import { test, expect } from './fixtures';

test.describe('Integrations Page', () => {
  test('renders sensor integration plugins list without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/integrations');

    await expect(page.locator('h1')).toContainText('Integrations');
    await expect(page.getByText('chrome')).toBeVisible();
    await expect(page.getByText('discord')).toBeVisible();
  });
});
