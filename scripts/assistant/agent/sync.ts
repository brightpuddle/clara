/**
 * scripts/assistant/agent/sync.ts
 *
 * Reconciles managed agent sessions with their chat channels: open sessions get
 * a channel, channels of closed or unknown sessions are removed.
 */

import { basename } from "node:path";

export interface SessionInfo {
  id: string;
  host: string;
  agent: string;
  dir: string;
  title?: string;
  state: string;
}

export interface AgentChannel {
  id: string;
  name: string;
  host: string;
  session: string;
}

export interface SyncPlan {
  create: SessionInfo[];
  remove: AgentChannel[];
  keep: AgentChannel[];
}

export function planSync(sessions: SessionInfo[], channels: AgentChannel[]): SyncPlan {
  const open = sessions.filter((s) => s.state !== "closed");
  const openKey = new Set(open.map((s) => `${s.host}:${s.id}`));
  const have = new Set(channels.map((c) => `${c.host}:${c.session}`));
  return {
    create: open.filter((s) => !have.has(`${s.host}:${s.id}`)),
    remove: channels.filter((c) => !openKey.has(`${c.host}:${c.session}`)),
    keep: channels.filter((c) => openKey.has(`${c.host}:${c.session}`)),
  };
}

/** A readable, mostly-unique channel name for a session. */
export function sessionLabel(s: SessionInfo): string {
  const base = (s.title?.trim() || basename(s.dir) || s.agent).slice(0, 40).trim();
  return `${base}-${s.id.slice(-4).toLowerCase()}`;
}
