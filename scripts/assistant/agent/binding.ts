/**
 * scripts/assistant/agent/binding.ts
 *
 * Maps chat channels/spaces to managed agent sessions.
 *
 * Discord topic:  `agent:<host>:<session> — <hint>`
 * Webex title:    `[Clara:<host>] <name> agent:<session>`
 *
 * A channel whose topic starts with `tmux:` stays on the legacy tmux path, so
 * changing the topic is the per-channel fallback switch.
 */

export interface AgentBinding {
  host: string;
  session: string;
}

export interface Origin {
  platform: "discord" | "webex";
  channelId: string;
  messageId: string;
}

export function parseAgentTopic(topic: string): AgentBinding | null {
  const m = String(topic || "").match(/^agent:([^:\s]+):(\S+)/i);
  if (!m) return null;
  return { host: m[1].toLowerCase(), session: m[2] };
}

export function parseAgentWebexTitle(title: string): AgentBinding | null {
  const m = String(title || "").match(/^\[Clara:([^\]]+)\]\s*.*?\bagent:(\S+)\s*$/i);
  if (!m) return null;
  return { host: m[1].toLowerCase(), session: m[2] };
}

export function agentTopic(host: string, session: string): string {
  return (
    `agent:${host}:${session} — Messages are queued as prompts for the managed agent. ` +
    `/stop interrupts the running turn, /kill closes the session.`
  );
}

export function webexAgentTitle(host: string, name: string, session: string): string {
  return `[Clara:${host}] ${name} agent:${session}`;
}

export function formatOrigin(o: Origin): string {
  return `${o.platform}:${o.channelId}:${o.messageId}`;
}

/** Parses an origin string produced by formatOrigin; returns null for anything else. */
export function parseOrigin(raw: string | undefined): Origin | null {
  const m = String(raw || "").match(/^(discord|webex):([^:]+):(.+)$/);
  if (!m) return null;
  if (m[3].startsWith("cli-")) return null;
  return { platform: m[1] as Origin["platform"], channelId: m[2], messageId: m[3] };
}
