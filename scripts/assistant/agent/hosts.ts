/**
 * scripts/assistant/agent/hosts.ts
 *
 * Routes agent.* tool calls to the host that owns a session. Remote hosts are
 * mounted by the daemon under a tool namespace prefix (sophia -> "laptop.").
 * Override with CLARA_AGENT_HOSTS="sophia=laptop.,other=other." .
 */

const DEFAULT_HOSTS: Record<string, string> = { sophia: "laptop." };

export function parseHostPrefixes(raw: string | undefined): Record<string, string> {
  if (!raw) return { ...DEFAULT_HOSTS };
  const out: Record<string, string> = {};
  for (const part of raw.split(",")) {
    const [name, prefix] = part.split("=").map((s) => s.trim());
    if (name && prefix !== undefined) out[name.toLowerCase()] = prefix;
  }
  return out;
}

export const REMOTE_HOSTS = parseHostPrefixes(process.env.CLARA_AGENT_HOSTS);

/** Tool name for `name` on `host`; local hosts (not in the table) are unprefixed. */
export function agentToolFor(
  host: string | undefined,
  name: string,
  remotes: Record<string, string> = REMOTE_HOSTS
): string {
  const prefix = host ? remotes[host.toLowerCase()] : undefined;
  return `${prefix ?? ""}${name}`;
}

/** Splits an optional `host:` prefix off a spawn name. */
export function splitHost(
  raw: string,
  remotes: Record<string, string> = REMOTE_HOSTS
): { host: string | undefined; name: string } {
  const m = raw.match(/^([a-z0-9_-]+):(.*)$/i);
  if (!m) return { host: undefined, name: raw };
  const h = m[1].toLowerCase();
  const host = h === "laptop" ? "sophia" : h;
  if (host === "eve" || host in remotes) return { host, name: m[2] || "dev" };
  return { host: undefined, name: raw };
}
