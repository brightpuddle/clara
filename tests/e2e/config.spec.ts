import { test, expect } from './fixtures';

test.describe('Config Page', () => {
  test('switches between Structured and Raw YAML tabs using Alpine without errors', async ({ claraPage: page }) => {
    await page.goto('/ui/config');

    await expect(page.locator('h1')).toContainText('Configuration');

    const structuredTab = page.getByRole('button', { name: 'Structured Settings' });
    const rawTab = page.getByRole('button', { name: 'Raw YAML' });

    await expect(structuredTab).toBeVisible();
    await expect(rawTab).toBeVisible();

    // Default tab is structured
    const structuredForm = page.locator('#structuredForm');
    await expect(structuredForm).toBeVisible();

    // Switch to Raw YAML tab
    await rawTab.click();
    await expect(page.locator('textarea[name="yaml"]')).toBeVisible();
    await expect(structuredForm).toBeHidden();

    // Switch back to Structured Settings tab
    await structuredTab.click();
    await expect(structuredForm).toBeVisible();
  });

  test('interacts with Alpine form controls and saves structured config', async ({ claraPage: page }) => {
    await page.goto('/ui/config');

    // Add a new task directory via Alpine list
    const addTaskDirBtn = page.getByRole('button', { name: '+ Add Directory' });
    if (await addTaskDirBtn.isVisible()) {
      await addTaskDirBtn.click();
    }

    // Submit structured settings
    const saveBtn = page.locator('#structuredForm button[type="submit"]');
    await saveBtn.click();

    await expect(page.getByText('Configuration saved')).toBeVisible();
  });
});
