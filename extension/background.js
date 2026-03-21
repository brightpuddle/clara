/**
 * Clara Browser Bridge — background service worker
 *
 * Connects to the clara mcp chrome WebSocket bridge on localhost and executes
 * browser automation commands dispatched from Clara intents.
 *
 * Protocol:
 *   Clara → extension:  { id: string, tool: string, params: object }
 *   Extension → Clara:  { id: string, result?: any, error?: string }
 *
 * The WebSocket connection keeps this service worker alive for as long as
 * Clara is running. On disconnect, exponential backoff reconnection is used.
 */

const BRIDGE_URL = 'ws://localhost:48765';
const MIN_RECONNECT_MS = 1000;
const MAX_RECONNECT_MS = 30000;

let ws = null;
let reconnectDelay = MIN_RECONNECT_MS;
let reconnectTimer = null;

// ── Connection management ────────────────────────────────────────────────────

function connect() {
  if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
    return;
  }

  console.log('[clara] Connecting to', BRIDGE_URL);
  ws = new WebSocket(BRIDGE_URL);

  ws.onopen = () => {
    console.log('[clara] Connected to Clara bridge');
    reconnectDelay = MIN_RECONNECT_MS;
    if (reconnectTimer !== null) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
  };

  ws.onmessage = async (event) => {
    let cmd;
    try {
      cmd = JSON.parse(event.data);
    } catch (e) {
      console.error('[clara] Failed to parse message:', e);
      return;
    }
    if (!cmd.id || !cmd.tool) {
      return; // malformed or keep-alive ping
    }

    const response = { id: cmd.id };
    try {
      response.result = await dispatch(cmd.tool, cmd.params || {});
    } catch (e) {
      response.error = e.message || String(e);
    }

    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify(response));
    }
  };

  ws.onclose = () => {
    console.log(`[clara] Disconnected. Reconnecting in ${reconnectDelay}ms`);
    scheduleReconnect();
  };

  ws.onerror = () => {
    // onerror is always followed by onclose; handle reconnect there.
  };
}

function scheduleReconnect() {
  if (reconnectTimer !== null) return;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    reconnectDelay = Math.min(reconnectDelay * 2, MAX_RECONNECT_MS);
    connect();
  }, reconnectDelay);
}

// ── Command dispatcher ───────────────────────────────────────────────────────

async function dispatch(tool, params) {
  switch (tool) {
    case 'navigate':      return handleNavigate(params);
    case 'click':         return handleClick(params);
    case 'fill':          return handleFill(params);
    case 'read_page':     return handleReadPage(params);
    case 'screenshot':    return handleScreenshot(params);
    case 'upload_file':   return handleUploadFile(params);
    case 'get_tabs':      return handleGetTabs(params);
    case 'close_tab':     return handleCloseTab(params);
    case 'wait_for_load': return handleWaitForLoad(params);
    default:
      throw new Error(`Unknown tool: ${tool}`);
  }
}

// ── Tool handlers ────────────────────────────────────────────────────────────

/**
 * navigate — open or update a tab
 * params: { url, tab_id?, background? }
 */
async function handleNavigate({ url, tab_id, background = true }) {
  if (!url) throw new Error('url is required');

  if (tab_id != null) {
    const tab = await chrome.tabs.update(tab_id, { url });
    return { tab_id: tab.id, url: tab.pendingUrl || tab.url };
  }

  const tab = await chrome.tabs.create({ url, active: !background });
  return { tab_id: tab.id, url: tab.pendingUrl || tab.url };
}

/**
 * click — click an element by CSS selector
 * params: { tab_id, selector, wait_after_ms? }
 */
async function handleClick({ tab_id, selector, wait_after_ms = 500 }) {
  requireArgs({ tab_id, selector });

  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (sel) => {
      const el = document.querySelector(sel);
      if (!el) return { ok: false, error: `Element not found: ${sel}` };
      el.scrollIntoView({ behavior: 'instant', block: 'center' });
      el.focus();
      el.dispatchEvent(new MouseEvent('mouseover', { bubbles: true, cancelable: true }));
      el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }));
      el.dispatchEvent(new MouseEvent('mouseup',   { bubbles: true, cancelable: true }));
      el.click();
      return { ok: true };
    },
    args: [selector],
  });

  if (result?.error) throw new Error(result.error);

  if (wait_after_ms > 0) {
    await sleep(wait_after_ms);
  }

  return { ok: true, selector };
}

/**
 * fill — set a text input value using the React-compatible native setter
 * params: { tab_id, selector, value, clear_first? }
 */
async function handleFill({ tab_id, selector, value, clear_first = true }) {
  requireArgs({ tab_id, selector });
  if (value === undefined || value === null) throw new Error('value is required');

  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (sel, val, clearFirst) => {
      const el = document.querySelector(sel);
      if (!el) return { ok: false, error: `Element not found: ${sel}` };

      el.focus();

      if (clearFirst && el.select) {
        el.select();
      }

      // Use the native value setter so React's synthetic events fire correctly.
      const proto = el instanceof HTMLTextAreaElement
        ? window.HTMLTextAreaElement.prototype
        : window.HTMLInputElement.prototype;
      const nativeSetter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;

      if (nativeSetter) {
        nativeSetter.call(el, val);
      } else {
        el.value = val;
      }

      el.dispatchEvent(new InputEvent('input',  { bubbles: true, cancelable: true }));
      el.dispatchEvent(new Event('change',       { bubbles: true, cancelable: true }));

      return { ok: true };
    },
    args: [selector, String(value), clear_first],
  });

  if (result?.error) throw new Error(result.error);
  return { ok: true, selector, value };
}

/**
 * read_page — return visible text, title, and URL from a tab
 * params: { tab_id }
 */
async function handleReadPage({ tab_id }) {
  if (!tab_id) throw new Error('tab_id is required');

  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: () => ({
      title: document.title,
      url:   window.location.href,
      text:  document.body?.innerText ?? '',
    }),
  });

  return result;
}

/**
 * screenshot — capture visible tab area as a PNG data URL
 * params: { tab_id? }
 */
async function handleScreenshot({ tab_id } = {}) {
  let windowId;

  if (tab_id != null) {
    const tab = await chrome.tabs.get(tab_id);
    windowId = tab.windowId;
    // captureVisibleTab only works on the active tab in a window.
    // Temporarily activate the target tab, capture, then optionally restore.
    await chrome.tabs.update(tab_id, { active: true });
  }

  const dataUrl = await chrome.tabs.captureVisibleTab(windowId ?? null, { format: 'png' });
  return { data_url: dataUrl };
}

/**
 * upload_file — set a local file on an <input type="file"> via CDP
 * params: { tab_id, selector, file_path }
 *
 * Uses chrome.debugger + DOM.setFileInputFiles — the same approach used by
 * Playwright and Puppeteer. Requires the "debugger" manifest permission.
 * Chrome will show a "Clara Browser Bridge is debugging this browser" banner
 * while the debugger is attached; it is detached immediately after the upload.
 */
async function handleUploadFile({ tab_id, selector, file_path }) {
  requireArgs({ tab_id, selector, file_path });

  const target = { tabId: tab_id };

  await debuggerAttach(target, '1.3');
  try {
    await sendDebugCmd(target, 'DOM.enable', {});
    await sendDebugCmd(target, 'Runtime.enable', {});

    const { root } = await sendDebugCmd(target, 'DOM.getDocument', {
      depth: -1,
      pierce: true,
    });

    const { nodeId } = await sendDebugCmd(target, 'DOM.querySelector', {
      nodeId: root.nodeId,
      selector,
    });

    if (!nodeId) throw new Error(`Element not found: ${selector}`);

    await sendDebugCmd(target, 'DOM.setFileInputFiles', {
      nodeId,
      files: [file_path],
    });

    // Trigger change event so the page reacts to the new file selection.
    await sendDebugCmd(target, 'Runtime.evaluate', {
      expression: `(function(){
        const el = document.querySelector(${JSON.stringify(selector)});
        if (el) el.dispatchEvent(new Event('change', { bubbles: true }));
      })()`,
    });
  } finally {
    await debuggerDetach(target);
  }

  return { ok: true, selector, file_path };
}

/**
 * get_tabs — list open tabs, optionally filtered by URL pattern
 * params: { url_filter? }
 */
async function handleGetTabs({ url_filter } = {}) {
  const query = url_filter ? { url: url_filter } : {};
  const tabs = await chrome.tabs.query(query);
  return tabs.map(t => ({
    id:        t.id,
    url:       t.url,
    title:     t.title,
    active:    t.active,
    window_id: t.windowId,
    status:    t.status,
  }));
}

/**
 * close_tab — close a tab by ID
 * params: { tab_id }
 */
async function handleCloseTab({ tab_id }) {
  if (!tab_id) throw new Error('tab_id is required');
  await chrome.tabs.remove(tab_id);
  return { ok: true, tab_id };
}

/**
 * wait_for_load — poll until tab status is 'complete'
 * params: { tab_id, timeout_seconds? }
 */
async function handleWaitForLoad({ tab_id, timeout_seconds = 30 }) {
  if (!tab_id) throw new Error('tab_id is required');

  const deadline = Date.now() + timeout_seconds * 1000;

  while (Date.now() < deadline) {
    const tab = await chrome.tabs.get(tab_id);
    if (tab.status === 'complete') {
      return { status: 'complete', tab_id, url: tab.url, title: tab.title };
    }
    await sleep(250);
  }

  const tab = await chrome.tabs.get(tab_id);
  return { status: 'timeout', tab_id, url: tab.url };
}

// ── CDP helpers ──────────────────────────────────────────────────────────────

function debuggerAttach(target, version) {
  return new Promise((resolve, reject) => {
    chrome.debugger.attach(target, version, () => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
      } else {
        resolve();
      }
    });
  });
}

function debuggerDetach(target) {
  return new Promise((resolve) => {
    chrome.debugger.detach(target, () => { resolve(); });
  });
}

function sendDebugCmd(target, method, params) {
  return new Promise((resolve, reject) => {
    chrome.debugger.sendCommand(target, method, params, (result) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
      } else {
        resolve(result);
      }
    });
  });
}

// ── Utility helpers ──────────────────────────────────────────────────────────

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function requireArgs(obj) {
  for (const [k, v] of Object.entries(obj)) {
    if (v == null) throw new Error(`${k} is required`);
  }
}

// ── Startup ──────────────────────────────────────────────────────────────────

connect();
