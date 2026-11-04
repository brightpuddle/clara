/**
 * scripts/assistant/agent/history.ts
 *
 * Formats the tail of a session's logged conversation so it can be re-posted
 * into a freshly created channel (for example after switching platforms).
 */

export interface LoggedEvent {
  type: string;
  payload: any;
}

const MAX_CHARS = 3500;

export function formatHistory(events: LoggedEvent[], maxChars = MAX_CHARS): string {
  const lines: string[] = [];
  for (const ev of events) {
    const text = String(ev.payload?.text || "").trim();
    if (!text) continue;
    if (ev.type === "message.user") lines.push(`**You:** ${text}`);
    else if (ev.type === "message.assistant") lines.push(`**Agent:** ${text}`);
  }
  const kept: string[] = [];
  let used = 0;
  for (let i = lines.length - 1; i >= 0; i--) {
    if (used + lines[i].length > maxChars && kept.length > 0) break;
    kept.unshift(lines[i].length > maxChars ? lines[i].slice(-maxChars) : lines[i]);
    used += lines[i].length;
  }
  if (kept.length === 0) return "";
  const omitted = lines.length - kept.length;
  const header = `📜 **Session history${omitted > 0 ? ` (last ${kept.length} of ${lines.length} messages)` : ""}**`;
  return `${header}\n\n${kept.join("\n\n")}`;
}
