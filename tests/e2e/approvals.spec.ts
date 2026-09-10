import { test, expect } from './fixtures';

test.describe('Approvals Page', () => {
  test('renders pending approval requests and handles resolution submission', async ({ claraPage: page }) => {
    await page.goto('/ui/approvals');

    await expect(page.locator('h1')).toContainText('HITL Approvals');
    await expect(page.getByText('Pending Approval')).toBeVisible();
    await expect(page.getByText('write access to ~/.ssh/config')).toBeVisible();

    // Check resolution buttons
    const allowBtn = page.getByRole('button', { name: 'Allow once' });
    const denyBtn = page.getByRole('button', { name: 'Deny and block' });

    await expect(allowBtn).toBeVisible();
    await expect(denyBtn).toBeVisible();

    // Submit a decision
    await allowBtn.click();

    // Verify redirected back with flash or all clear
    await expect(page).toHaveURL(/\/ui\/approvals/);
    await expect(page.getByText('Decision recorded')).toBeVisible();
  });
});
