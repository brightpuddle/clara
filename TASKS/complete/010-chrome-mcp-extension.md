---
plan_recommended: true
---

# Chrome MCP Extension

## Planning Context

This is the critical blocker for all Facebook Marketplace automation. Before any
implementation begins, evaluate the existing `mcp-chrome` project
(https://github.com/hangwin/mcp-chrome) against the requirements below. If it
covers the needed capabilities, integrate it as a configured external MCP server
in Clara's `config.yaml`. If it has meaningful gaps, fork and extend it, keeping
the fork inside the Clara repo under `chrome/` or as a separate Go+JS tool under
`cmd/`. Design decisions to resolve during planning:

- Does mcp-chrome support the full set of required interactions (navigation,
  clicking, typing, file upload for photos, reading structured page content)?
- Can browser automation run non-blocking (in the background while Alex uses her
  browser for other things)? What isolation strategy achieves this (separate
  Chrome profile, headless, extension background service worker)?
- What is the anti-detection strategy (randomized delays, throttled request
  rates, human-like interaction patterns)? Should this be built into the MCP
  server itself or enforced in the Starlark intent layer?
- If a fork is needed: Go + stdio wrapper around a Chrome DevTools Protocol
  client, or TypeScript extension with an MCP server alongside it?

## Context

All Facebook Marketplace automation (new ad creation, renewal, and Messenger
replies) requires programmatic control of the Chrome browser. Facebook does not
provide a public API for personal accounts. The chosen approach is a Chrome MCP
extension / browser automation server.

### Required Capabilities

The MCP server must expose tools that collectively cover:

**Navigation and interaction:**

- Navigate to a URL
- Click an element (by selector, text, or aria label)
- Type text into a field
- Read text and structured content from the current page
- Scroll the page
- Wait for an element or network idle

**File operations:**

- Upload a local file (photo) through a file input element

**Facebook Marketplace specific:**

- Read the list of active marketplace listings (title, price, status, listing
  age, renew/relist availability)
- Read listing details (description, category, photos)
- Access Facebook Messenger conversation list and individual message threads

**Anti-detection requirements (CRITICAL):**

- All interactions must be throttled and randomized to appear human
- New ad creation: throttled to avoid triggering spam detection (no bulk
  posting)
- Renewals and re-lists: randomized delays between actions
- These constraints must not be bypassable from the intent layer; they should be
  enforced in the server

**Non-blocking requirement:**

- Browser automation should not prevent Alex from using her browser during
  automation runs
- Target: automation uses a separate Chrome profile, or runs against a Chrome
  instance she is not actively using, or runs headlessly

## Decisions Made

- **Approach:** Evaluate `mcp-chrome` first. If sufficient, use as external
  dependency configured in `config.yaml`. If gaps exist, fork and extend into
  the Clara repo.
- This MCP server will be used by all three Facebook automation intents (040,
  050, 060) and the Messenger reply intent (060).
- The Chrome MCP server is configured as a standard external MCP server in
  Clara's `config.yaml`, like any other tool provider.

## Acceptance Criteria

- A Chrome MCP server (existing or forked) is functional and configured in Clara
- The server exposes at minimum: navigate, click, type, read-page-content,
  upload-file, scroll
- All interactions include configurable randomized delays (min/max bounds per
  action type)
- The server can run against an isolated Chrome profile, not interfering with
  Alex's active browser session
- Integration test or manual verification against a non-Facebook target page
  (e.g., a local HTML fixture) confirms each tool works
