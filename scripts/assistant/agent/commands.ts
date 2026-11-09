/**
 * scripts/assistant/agent/commands.ts
 *
 * Inside an agent channel, `/name` for a name in CLARA_AGENT_COMMANDS belongs to
 * Clara and never reaches the agent. Any other `/name` is forwarded to the agent
 * unchanged, so agent-native slash commands (`/plan`, `/compact`, ...) keep working.
 */

export interface SlashCommand {
  name: string;
  args: string;
}

export const CLARA_AGENT_COMMANDS = new Set([
  "help",
  "new",
  "clear",
  "model",
  "stop",
  "interrupt",
  "kill",
]);

export function parseSlashCommand(
  text: string,
  known: ReadonlySet<string> = CLARA_AGENT_COMMANDS
): SlashCommand | null {
  const m = text.trim().match(/^\/([A-Za-z][\w-]*)(?:\s+([\s\S]*))?$/);
  if (!m) return null;
  const name = m[1].toLowerCase();
  if (!known.has(name)) return null;
  return { name, args: (m[2] || "").trim() };
}
