import { test, expect } from './fixtures';

test.describe('Dashboard Page', () => {
  test('renders dashboard metrics, navigation, and automations table without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/');

    // Page title and header
    await expect(page).toHaveTitle(/Clara/);
    await expect(page.locator('h1')).toContainText('Dashboard');

    // Stat cards within main content area
    const main = page.locator('main');
    await expect(main.getByText('Actuators', { exact: true })).toBeVisible();
    await expect(main.getByText('Pending Approvals', { exact: true })).toBeVisible();
    await expect(main.getByText('Tools', { exact: true })).toBeVisible();
    await expect(main.getByText('Integrations', { exact: true })).toBeVisible();

    // Automations table
    await expect(page.getByText('GitHub PR Reviewer')).toBeVisible();
    await expect(page.getByText('Slack Incident Responder')).toBeVisible();

    // Root redirect to /ui/
    await page.goto('/');
    expect(page.url()).toContain('/ui/');
  });
});
