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

    // Capture console.error calls (including Alpine runtime warnings/errors)
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        consoleErrors.push(msg.text());
      }
    });

    // Inject HTMX error listeners before each navigation
    await page.addInitScript(() => {
      (window as any).__htmxErrors = [];
      const recordHtmxError = (evtType: string) => (e: any) => {
        (window as any).__htmxErrors.push({
          event: evtType,
          detail: e.detail ? JSON.stringify(e.detail) : undefined,
        });
      };

      document.addEventListener('htmx:error', recordHtmxError('htmx:error'));
      document.addEventListener('htmx:targetError', recordHtmxError('htmx:targetError'));
      document.addEventListener('htmx:responseError', recordHtmxError('htmx:responseError'));
      document.addEventListener('htmx:sendError', recordHtmxError('htmx:sendError'));
      document.addEventListener('htmx:sseError', recordHtmxError('htmx:sseError'));
    });

    await use(page);

    // Retrieve HTMX errors from page context if available
    let htmxErrors: any[] = [];
    try {
      htmxErrors = await page.evaluate(() => (window as any).__htmxErrors || []);
    } catch {
      // Ignore if page already closed
    }

    // Assert that no HTMX runtime errors were emitted
    expect(
      htmxErrors,
      `HTMX runtime error(s) detected: ${JSON.stringify(htmxErrors, null, 2)}`
    ).toEqual([]);

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
