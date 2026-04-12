/**
 * Clara Browser Bridge — background service worker (Native Messaging)
 *
 * Reliability design:
 *  - chrome.alarms fires every 25 s to keep the service worker alive (MV3
 *    workers are killed after ~5 min of inactivity without this).
 *  - On every alarm tick we ensure the port is open; if not, we reconnect.
 *  - On connect we send a "hello" with our version so the server can push an
 *    update+reload if the extension is out of date.
 *  - The action icon is drawn via OffscreenCanvas: green circle = connected,
 *    grey circle = disconnected.
 *  - Only one reconnect timer can be pending at a time.
 */

const HOST_NAME = 'com.brightpuddle.clara';
const VERSION   = '0.3.0';

// Alarm name used for the service-worker keepalive heartbeat.
const KEEPALIVE_ALARM = 'clara-keepalive';

let port            = null;
let isConnected     = false;
let reconnectTimer  = null;  // tracks the pending reconnect setTimeout ID

console.log('[Clara] Service worker loading (Native Messaging)...');

// ── Icon ─────────────────────────────────────────────────────────────────────

async function updateIcon() {
  try {
    const size   = 19;
    const canvas = new OffscreenCanvas(size, size);
    const ctx    = canvas.getContext('2d');

    ctx.clearRect(0, 0, size, size);

    // Outer circle — green when connected, grey when not.
    ctx.beginPath();
    ctx.arc(size / 2, size / 2, size / 2 - 0.5, 0, 2 * Math.PI);
    ctx.fillStyle = isConnected ? '#4CAF50' : '#9E9E9E';
    ctx.fill();

    // 'C' glyph in white for Clara.
    ctx.fillStyle    = 'white';
    ctx.font         = `bold ${Math.round(size * 0.6)}px sans-serif`;
    ctx.textAlign    = 'center';
    ctx.textBaseline = 'middle';
    ctx.fillText('C', size / 2, size / 2 + 0.5);

    const imageData = ctx.getImageData(0, 0, size, size);
    await chrome.action.setIcon({ imageData });

    // Small badge dot so state is also legible at smaller sizes.
    await chrome.action.setBadgeText({ text: isConnected ? '●' : '' });
    await chrome.action.setBadgeBackgroundColor({ color: isConnected ? '#4CAF50' : '#9E9E9E' });
  } catch (e) {
    console.error('[Clara] Failed to update icon:', e);
  }
}

function updateStatus(connected) {
  isConnected = connected;
  updateIcon();
  chrome.runtime.sendMessage({ type: 'status', connected }).catch(() => {});
}

// ── Connection ────────────────────────────────────────────────────────────────

function scheduleReconnect(delayMs = 5000) {
  if (reconnectTimer !== null) return; // already scheduled — don't stack timers
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    connect();
  }, delayMs);
}

function connect() {
  // Tear down any existing port cleanly.
  if (port) {
    port.onMessage.removeListener(onMessage);
    port.onDisconnect.removeListener(onDisconnect);
    try { port.disconnect(); } catch (_) {}
    port = null;
  }

  console.log(`[Clara] Connecting to native host: ${HOST_NAME}...`);
  try {
    port = chrome.runtime.connectNative(HOST_NAME);
    port.onMessage.addListener(onMessage);
    port.onDisconnect.addListener(onDisconnect);

    // Native Messaging has no explicit "open" event; assume connected and let
    // the server-side heartbeat or onDisconnect correct us if wrong.
    updateStatus(true);

    // Send hello so the server can verify our version and push an update if
    // we're out of date.
    port.postMessage({ type: 'hello', version: VERSION });
  } catch (e) {
    console.error('[Clara] Failed to connect to native host:', e);
    updateStatus(false);
    scheduleReconnect();
  }
}

function onDisconnect() {
  const err = chrome.runtime.lastError;
  console.log('[Clara] Native host disconnected:', err ? err.message : 'unknown reason');
  port = null;
  updateStatus(false);
  scheduleReconnect();
}

// ── Message handler ───────────────────────────────────────────────────────────

async function onMessage(request) {
  const { id, tool, params, type } = request;

  if (type === 'ping') {
    if (port) port.postMessage({ type: 'pong' });
    return;
  }

  // Both "reload" and "update" cause the extension to reload from disk.
  // "update" is sent by the server after it writes fresh extension files.
  if (type === 'reload' || type === 'update') {
    console.log('[Clara] Reload requested by server (type=' + type + ')');
    chrome.runtime.reload();
    return;
  }

  if (!tool) return;

  try {
    const result = await executeTool(tool, params);
    if (port) port.postMessage({ id, result });
  } catch (error) {
    console.error(`[Clara] Error executing ${tool}:`, error);
    if (port) port.postMessage({ id, error: error.message });
  }
}

async function executeTool(tool, params) {
  switch (tool) {
    case 'get_tabs':          return handleGetTabs(params);
    case 'navigate':          return handleNavigate(params);
    case 'click':             return handleClick(params);
    case 'click_by_label':    return handleClickByLabel(params);
    case 'fill':              return handleFill(params);
    case 'fill_by_label':     return handleFillByLabel(params);
    case 'read_page':         return handleReadPage(params);
    case 'screenshot':        return handleScreenshot(params);
    case 'upload_file':       return handleUploadFile(params);
    case 'eval':              return handleEval(params);
    case 'close_tab':         return handleCloseTab(params);
    case 'cleanup_tabs':      return handleCleanupTabs(params);
    case 'wait_for_load':     return handleWaitForLoad(params);
    case 'wait_for_selector': return handleWaitForSelector(params);
    case 'type':              return handleType(params);
    case 'query_elements':    return handleQueryElements(params);
    case 'debugger_command':  return handleDebuggerCommand(params);
    case 'type_by_selector':  return handleTypeBySelector(params);
    default: throw new Error(`Unknown tool: ${tool}`);
  }
}

async function handleGetTabs({ url_filter } = {}) {
  const tabs = await chrome.tabs.query(url_filter ? { url: url_filter } : {});
  return tabs.map(t => ({ id: t.id, url: t.url, title: t.title }));
}

async function handleNavigate({ url, tab_id, background = true }) {
  if (!url) throw new Error('url is required');
  await maybeApplyHumanDelay(arguments[0]);
  if (tab_id != null) {
    const tab = await chrome.tabs.update(tab_id, { url });
    return { tab_id: tab.id, url: tab.url };
  }
  const tab = await chrome.tabs.create({ url, active: !background });
  const { ownedTabIds = [] } = await chrome.storage.local.get('ownedTabIds');
  ownedTabIds.push(tab.id);
  await chrome.storage.local.set({ ownedTabIds });
  return { tab_id: tab.id, url: tab.pendingUrl || tab.url };
}

async function handleClick({ tab_id, selector, wait_after_ms = 500 }) {
  await maybeApplyHumanDelay(arguments[0]);
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (sel) => {
      const el = document.querySelector(sel);
      if (!el) return { error: `Not found: ${sel}` };
      el.scrollIntoView({ behavior: 'instant', block: 'center' });
      el.click();
      return { ok: true };
    },
    args: [selector]
  });
  if (result?.error) throw new Error(result.error);
  if (wait_after_ms) await sleep(wait_after_ms);
  return result;
}

async function handleFill({ tab_id, selector, value, clear_first = true }) {
  await maybeApplyHumanDelay(arguments[0]);
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (sel, val, clear) => {
      const el = document.querySelector(sel);
      if (!el) return { error: `Not found: ${sel}` };
      if (clear && el.select) el.select();
      const setter = Object.getOwnPropertyDescriptor(el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')?.set;
      if (setter) setter.call(el, val); else el.value = val;
      el.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, data: val }));
      el.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true }));
      el.dispatchEvent(new KeyboardEvent('keypress', { bubbles: true }));
      el.dispatchEvent(new InputEvent('input', { bubbles: true, data: val }));
      el.dispatchEvent(new KeyboardEvent('keyup', { bubbles: true }));
      el.dispatchEvent(new Event('change', { bubbles: true }));
      return { ok: true };
    },
    args: [selector, String(value), clear_first]
  });
  if (result?.error) throw new Error(result.error);
  return result;
}

async function handleFillByLabel({ tab_id, label, value, tag = 'input' }) {
  await maybeApplyHumanDelay(arguments[0]);
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (labelText, val, tagName) => {
      const search = labelText.toLowerCase();
      const elements = Array.from(document.querySelectorAll('span, label, div, p, [aria-label]'));
      let labelEl = elements.find(el => {
        const text = ((el.innerText || '') + ' ' + (el.getAttribute('aria-label') || '')).trim().toLowerCase();
        return text === search || text.includes(search);
      });
      if (!labelEl) return { error: `Label not found: ${labelText}` };
      let current = labelEl;
      let input = null;
      for (let i = 0; i < 5; i++) {
        input = current.tagName === tagName.toUpperCase() ? current : current.querySelector(tagName);
        if (!input) {
          let sib = current.nextElementSibling;
          while (sib) {
            input = sib.tagName === tagName.toUpperCase() ? sib : sib.querySelector(tagName);
            if (input) break;
            sib = sib.nextElementSibling;
          }
        }
        if (input) break;
        current = current.parentElement;
        if (!current) break;
      }
      if (!input) return { error: `Input not found for: ${labelText}` };
      input.scrollIntoView({ behavior: 'instant', block: 'center' });
      input.focus();
      const setter = Object.getOwnPropertyDescriptor(input instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype, 'value')?.set;
      if (setter) setter.call(input, val); else input.value = val;
      input.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, data: val }));
      input.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true }));
      input.dispatchEvent(new KeyboardEvent('keypress', { bubbles: true }));
      input.dispatchEvent(new InputEvent('input', { bubbles: true, data: val }));
      input.dispatchEvent(new KeyboardEvent('keyup', { bubbles: true }));
      input.dispatchEvent(new Event('change', { bubbles: true }));
      return { ok: true };
    },
    args: [label, String(value), tag]
  });
  if (result?.error) throw new Error(result.error);
  return result;
}

async function handleClickByLabel({ tab_id, label }) {
  await maybeApplyHumanDelay(arguments[0]);
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (labelText) => {
      const search = labelText.toLowerCase();
      const candidates = Array.from(document.querySelectorAll('div, span, button, a, [role="button"], [role="option"], li'));
      const found = candidates.find(el => {
        const text = ((el.innerText || '') + ' ' + (el.getAttribute('aria-label') || '')).trim().toLowerCase();
        return text === search || (text.length > 0 && text.includes(search));
      });
      if (!found) return { error: `Not found: ${labelText}` };
      found.scrollIntoView({ behavior: 'instant', block: 'center' });
      found.click();
      return { ok: true };
    },
    args: [label]
  });
  if (result?.error) throw new Error(result.error);
  return result;
}

async function handleReadPage({ tab_id }) {
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: () => ({ title: document.title, url: window.location.href, text: document.body?.innerText || '' })
  });
  return result;
}

async function handleScreenshot({ tab_id } = {}) {
  let windowId;
  if (tab_id != null) { const tab = await chrome.tabs.get(tab_id); windowId = tab.windowId; await chrome.tabs.update(tab_id, { active: true }); }
  const dataUrl = await chrome.tabs.captureVisibleTab(windowId ?? null, { format: 'png' });
  return { data_url: dataUrl };
}

async function handleUploadFile({ tab_id, selector, file_path, file_paths }) {
  const files = Array.isArray(file_paths) ? file_paths : (file_path ? [file_path] : []);
  if (!files.length) throw new Error('files required');
  await maybeApplyHumanDelay(arguments[0]);
  const target = { tabId: tab_id };
  await debuggerAttach(target, '1.3');
  try {
    await sendDebugCmd(target, 'DOM.enable', {});
    const { root } = await sendDebugCmd(target, 'DOM.getDocument', { depth: -1, pierce: true });
    const { nodeId } = await sendDebugCmd(target, 'DOM.querySelector', { nodeId: root.nodeId, selector });
    if (!nodeId) throw new Error(`Not found: ${selector}`);
    await sendDebugCmd(target, 'DOM.setFileInputFiles', { nodeId, files });
  } finally { await debuggerDetach(target); }
  return { ok: true };
}

async function handleEval({ tab_id, script, args = {} }) {
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    world: 'MAIN',
    func: async (src, a) => {
      try {
        const fn = new Function('args', `return (async () => { ${src}\n })();`);
        return { ok: true, value: await fn(a) };
      } catch (e) { return { error: e.message }; }
    },
    args: [script, args]
  });
  if (result?.error) throw new Error(result.error);
  return result?.value;
}

async function handleCleanupTabs() {
  const { ownedTabIds = [] } = await chrome.storage.local.get('ownedTabIds');
  for (const id of ownedTabIds) { try { await chrome.tabs.remove(id); } catch(e) {} }
  await chrome.storage.local.set({ ownedTabIds: [] });
  return { ok: true };
}

async function handleCloseTab({ tab_id }) {
  await chrome.tabs.remove(tab_id);
  return { ok: true };
}

async function handleWaitForLoad({ tab_id, timeout_seconds = 30 }) {
  const deadline = Date.now() + (timeout_seconds * 1000);
  while (Date.now() < deadline) {
    const tab = await chrome.tabs.get(tab_id);
    if (tab.status === 'complete') return { status: 'complete' };
    await sleep(250);
  }
  return { status: 'timeout' };
}

async function handleWaitForSelector({ tab_id, selector, timeout_seconds = 30 }) {
  const deadline = Date.now() + (timeout_seconds * 1000);
  while (Date.now() < deadline) {
    const [{ result }] = await chrome.scripting.executeScript({
      target: { tabId: tab_id },
      func: (s) => !!document.querySelector(s),
      args: [selector]
    });
    if (result) return { status: 'found' };
    await sleep(500);
  }
  throw new Error(`Timeout: ${selector}`);
}

async function handleType({ tab_id, text, delay_between_keys_ms = 10 }) {
  if (tab_id == null) throw new Error('tab_id required');
  if (text == null) throw new Error('text required');
  const str = String(text);
  const target = { tabId: tab_id };
  await debuggerAttach(target, '1.3');
  try {
    for (const char of str) {
      await dispatchChar(target, char);
      if (delay_between_keys_ms > 0) await sleep(delay_between_keys_ms);
    }
  } finally {
    await debuggerDetach(target);
  }
  return { ok: true };
}

async function handleQueryElements({ tab_id, selector }) {
  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId: tab_id },
    func: (sel) => {
      const elements = Array.from(document.querySelectorAll(sel));
      return elements.map(el => {
        const attributes = {};
        for (const attr of el.attributes) {
          attributes[attr.name] = attr.value;
        }
        const details = {
          tag: el.tagName,
          id: el.id,
          className: el.className,
          innerText: el.innerText,
          value: el.value,
          ariaLabel: el.getAttribute('aria-label'),
          placeholder: el.placeholder,
          type: el.type,
          role: el.getAttribute('role'),
          attributes,
          parent: null
        };
        if (el.parentElement) {
          details.parent = {
            tag: el.parentElement.tagName,
            id: el.parentElement.id,
            className: el.parentElement.className
          };
        }
        return details;
      });
    },
    args: [selector]
  });
  return result;
}

async function handleDebuggerCommand({ tab_id, method, params = {} }) {
  const target = { tabId: tab_id };
  await debuggerAttach(target, '1.3');
  try {
    return await sendDebugCmd(target, method, params);
  } finally {
    await debuggerDetach(target);
  }
}

async function handleTypeBySelector({ tab_id, selector, text, delay_between_keys_ms = 10 }) {
  if (tab_id == null) throw new Error('tab_id required');
  if (text == null) throw new Error('text required');
  const str = String(text);
  const target = { tabId: tab_id };
  await debuggerAttach(target, '1.3');
  try {
    await sendDebugCmd(target, 'DOM.enable', {});
    const { root } = await sendDebugCmd(target, 'DOM.getDocument', {});
    const { nodeId } = await sendDebugCmd(target, 'DOM.querySelector', { nodeId: root.nodeId, selector });
    if (!nodeId) throw new Error(`Node not found for selector: ${selector}`);
    await sendDebugCmd(target, 'DOM.focus', { nodeId });
    for (const char of str) {
      await dispatchChar(target, char);
      if (delay_between_keys_ms > 0) await sleep(delay_between_keys_ms);
    }
  } finally {
    await debuggerDetach(target);
  }
  return { ok: true };
}

async function dispatchChar(target, char) {
  if (char === '\n' || char === '\r') {
    await sendDebugCmd(target, 'Input.dispatchKeyEvent', {
      type: 'rawKeyDown',
      windowsVirtualKeyCode: 13,
      nativeVirtualKeyCode: 13,
      unmodifiedText: '\r',
      text: '\r',
    });
    await sendDebugCmd(target, 'Input.dispatchKeyEvent', {
      type: 'char',
      windowsVirtualKeyCode: 13,
      nativeVirtualKeyCode: 13,
      unmodifiedText: '\r',
      text: '\r',
    });
    await sendDebugCmd(target, 'Input.dispatchKeyEvent', {
      type: 'keyUp',
      windowsVirtualKeyCode: 13,
      nativeVirtualKeyCode: 13,
      unmodifiedText: '\r',
      text: '\r',
    });
  } else {
    await sendDebugCmd(target, 'Input.dispatchKeyEvent', {
      type: 'char',
      text: char,
      unmodifiedText: char,
    });
  }
}

function debuggerAttach(t, v) { return new Promise((res, rej) => chrome.debugger.attach(t, v, () => chrome.runtime.lastError ? rej(new Error(chrome.runtime.lastError.message)) : res())); }
function debuggerDetach(t) { return new Promise(res => chrome.debugger.detach(t, res)); }
function sendDebugCmd(t, m, p) { return new Promise((res, rej) => chrome.debugger.sendCommand(t, m, p, (r) => chrome.runtime.lastError ? rej(new Error(chrome.runtime.lastError.message)) : res(r))); }
function sleep(ms) { return new Promise(res => setTimeout(res, ms)); }

async function maybeApplyHumanDelay(params = {}) {
  const MIN_HUMAN_DELAY_MS = 5000;
  const MAX_HUMAN_DELAY_MS = 10000;
  if (params.delay_before_ms != null) { await sleep(Number(params.delay_before_ms)); return; }
  if (params.human_delay === false) return;
  const min = params.human_delay_min_ms ?? MIN_HUMAN_DELAY_MS;
  const max = params.human_delay_max_ms ?? MAX_HUMAN_DELAY_MS;
  await sleep(min + Math.floor(Math.random() * (max - min + 1)));
}

// ── Lifecycle & keepalive ─────────────────────────────────────────────────────

// Keep the service worker alive by firing an alarm every 25 seconds.
// Without this, Chrome terminates MV3 service workers after ~5 minutes of
// inactivity, dropping the Native Messaging connection.
chrome.alarms.create(KEEPALIVE_ALARM, { periodInMinutes: 25 / 60 });

chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name !== KEEPALIVE_ALARM) return;
  // Reconnect if the port went away while the SW was dormant.
  if (!port) {
    console.log('[Clara] Alarm fired — reconnecting...');
    connect();
  }
});

// Reconnect after Chrome starts or the extension is updated/installed.
chrome.runtime.onStartup.addListener(() => {
  console.log('[Clara] Chrome startup — connecting...');
  connect();
});

chrome.runtime.onInstalled.addListener(() => {
  console.log('[Clara] Extension installed/updated — connecting...');
  connect();
});

// Popup / inter-page messaging.
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg.type === 'get_status') {
    sendResponse({ connected: isConnected });
  } else if (msg.type === 'reconnect') {
    connect();
    sendResponse({ ok: true });
  } else if (msg.type === 'reload') {
    chrome.runtime.reload();
  }
});

// Initial connection when the service worker first loads.
connect();
