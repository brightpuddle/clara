/**
 * Clara Browser Bridge — content script
 *
 * This script is injected into every page at document_idle. It currently
 * serves as a placeholder and utility layer for future page-level event
 * listening. All current automation actions are handled via
 * chrome.scripting.executeScript in the background service worker.
 *
 * DO NOT add communication back to the background script here unless
 * strictly necessary — executeScript with inline functions is sufficient
 * for synchronous DOM operations and avoids message-passing complexity.
 */

// Reserved for future use: page-level event observation, mutation observers,
// or long-running DOM watchers that require persistent injection.
