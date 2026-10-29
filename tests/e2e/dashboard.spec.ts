import { test, expect } from './fixtures';

test.describe('Dashboard Page', () => {
  test('renders dashboard metrics, navigation, and automations table without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/');

    // Page title and header
    await expect(page).toHaveTitle(/Clara/);
    await expect(page.locator('h1')).toContainText('Dashboard');

    // Stat cards within main content area
    const main = page.locator('main');
    await expect(main.getByText('Triggers', { exact: true })).toBeVisible();
    await expect(main.getByText('Workers', { exact: true })).toBeVisible();
    await expect(main.getByText('MCP Tools', { exact: true })).toBeVisible();
    await expect(main.getByText('Sensors / MCP', { exact: true })).toBeVisible();

    // Automations table
    await expect(page.getByText('github-pr-reviewer')).toBeVisible();
    await expect(page.getByText('slack-incident-responder')).toBeVisible();

    // Root redirect to /ui/
    await page.goto('/');
    expect(page.url()).toContain('/ui/');
  });
});
