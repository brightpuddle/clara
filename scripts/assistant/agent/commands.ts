/**
 * scripts/assistant/agent/commands.ts
 *
 * Inside an agent channel, everything starting with the Clara prefix `.` belongs
 * to Clara and must never reach the agent. `./name` is the explicit way to send
 * a slash command to the agent, and `..` or `...` are ordinary text.
 */

export interface DotCommand {
  name: string;
  args: string;
}

export function parseDotCommand(text: string): DotCommand | null {
  const m = text.trim().match(/^\.([A-Za-z][\w-]*)(?:\s+([\s\S]*))?$/);
  if (!m) return null;
  return { name: m[1].toLowerCase(), args: (m[2] || "").trim() };
}
