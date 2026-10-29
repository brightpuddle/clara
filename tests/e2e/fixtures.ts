import { test as base, expect, Page } from '@playwright/test';

interface ClaraFixtures {
  claraPage: Page;
}

export const test = base.extend<ClaraFixtures>({
  claraPage: async ({ page }, use) => {
    const pageErrors: Error[] = [];
    const consoleErrors: string[] = [];

    // Capture uncaught JS exceptions
    page.on('pageerror', (err) => {
      pageErrors.push(err);
    });

    // Capture console.error calls
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text());
      }
    });

    await use(page);

    // Assert that no unhandled page runtime errors occurred
    expect(
      pageErrors.map((e) => e.message || String(e)),
      `Page runtime error(s) detected: ${pageErrors.map((e) => e.stack || e.message).join('\n')}`
    ).toEqual([]);

    // Assert that no console.error calls occurred
    expect(
      consoleErrors,
      `Console error(s) detected: ${consoleErrors.join('\n')}`
    ).toEqual([]);
  },
});

export { expect };
