import { test, expect } from './fixtures';

test.describe('Actuators Page', () => {
  test('renders actuators list and supports Alpine-powered live filtering', async ({ claraPage: page }) => {
    await page.goto('/ui/actuators');

    await expect(page.locator('h1')).toContainText('Actuators');
    await expect(page.getByText('GitHub PR Reviewer')).toBeVisible();
    await expect(page.getByText('Slack Incident Responder')).toBeVisible();

    // Test Alpine search input
    const searchInput = page.getByPlaceholder('Search actuators...');
    await searchInput.fill('Slack');

    // Slack should be visible, GitHub should be hidden by Alpine x-show
    await expect(page.getByText('Slack Incident Responder')).toBeVisible();
    await expect(page.getByText('GitHub PR Reviewer')).toBeHidden();

    await searchInput.fill('GitHub');
    await expect(page.getByText('GitHub PR Reviewer')).toBeVisible();
    await expect(page.getByText('Slack Incident Responder')).toBeHidden();

    // Clear search
    await searchInput.fill('');
    await expect(page.getByText('GitHub PR Reviewer')).toBeVisible();
    await expect(page.getByText('Slack Incident Responder')).toBeVisible();
  });

  test('navigates to actuator detail view and triggers run via HTMX swap', async ({ claraPage: page }) => {
    await page.goto('/ui/actuators');

    // Click "View" on GitHub PR Reviewer
    await page.locator('tr:has-text("GitHub PR Reviewer") a:has-text("View")').click();

    await expect(page).toHaveURL(/\/ui\/actuators\/github-pr-reviewer/);
    await expect(page.locator('h1')).toContainText('GitHub PR Reviewer');
    await expect(page.getByText('Actuator Overview')).toBeVisible();
    await expect(page.getByText('Fast-path Rule ID', { exact: false })).toBeVisible();

    // Trigger HTMX run button and verify swap
    const triggerBtn = page.getByRole('button', { name: '▶ Trigger Run' });
    await expect(triggerBtn).toBeVisible();
    await triggerBtn.click();

    // Target #run-result should receive the response without full page reload or HTMX error
    await expect(page.locator('#run-result')).not.toBeEmpty();
  });
});
