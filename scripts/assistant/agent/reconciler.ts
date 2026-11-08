/**
 * scripts/assistant/agent/reconciler.ts
 *
 * Background reconciliation engine for Clara.
 * Automatically keeps chat platform channels (Discord / Webex) in sync
 * with active tmux and managed-agent sessions across all hosts (Eve & Sophia)
 * without requiring the user to run manual '.sync' commands.
 */

import { callTool } from "../../clara.ts";
import {
  getPrimaryAdapter,
  getPlatformAdapter,
  syncSessionChannels,
} from "../platform/manager.ts";
import type { PlatformType } from "../platform/types.ts";
import { planSync, sessionLabel, type SessionInfo } from "./sync.ts";
import { REMOTE_HOSTS, agentToolFor } from "./hosts.ts";
import { formatHistory } from "./history.ts";

export interface ReconcileResult {
  tmuxChanges: boolean;
  agentChanges: boolean;
  createdAgents: number;
  removedAgents: number;
}

export async function listAllAgentSessions(): Promise<SessionInfo[]> {
  const hosts = ["eve", ...Object.keys(REMOTE_HOSTS)];
  const sessions: SessionInfo[] = [];

  await Promise.all(
    hosts.map(async (h) => {
      try {
        const tool = agentToolFor(h, "agent.session.list");
        const res = (await callTool(tool as any, {})) as any;
        // The tool returns {sessions: [...]}; tolerate a bare array for safety.
        const list = Array.isArray(res) ? res : res?.sessions ?? [];
        for (const s of list) {
          sessions.push({ ...s, host: h });
        }
      } catch {
        // Satellite or host may be temporarily unreachable / sleeping
      }
    })
  );

  return sessions;
}

export async function readHostSessionEvents(
  host: string | undefined,
  session: string
): Promise<any[]> {
  try {
    const tool = agentToolFor(host, "agent.events.read");
    const res = (await callTool(tool as any, { session, limit: 100 })) as any;
    return (res?.events || []) as any[];
  } catch {
    return [];
  }
}

export async function backfillAgentChannelHistory(
  channelId: string,
  host: string | undefined,
  session: string
): Promise<void> {
  try {
    const events = await readHostSessionEvents(host, session);
    const text = formatHistory(events);
    if (text) {
      await getPrimaryAdapter().sendMessage(channelId, text);
    }
  } catch (err: any) {
    console.warn(`[reconciler] history backfill failed for ${host}:${session}: ${err.message}`);
  }
}

/**
 * Reconcile managed-agent sessions with Discord/Webex channels.
 */
export async function reconcileAgentSessions(): Promise<{ created: number; removed: number }> {
  const primary = getPrimaryAdapter();
  const other = getPlatformAdapter(primary.platform === "webex" ? "discord" : "webex");
  const sessions = await listAllAgentSessions();
  const existingChannels = await primary.listAgentChannels();
  const plan = planSync(sessions, existingChannels);

  let createdCount = 0;
  let removedCount = 0;

  for (const sess of plan.create) {
    try {
      const ch = await primary.ensureAgentChannel(sess.host, sess.id, sessionLabel(sess));
      await backfillAgentChannelHistory(ch.id, sess.host, sess.id);
      createdCount++;
      console.log(`[reconciler] Auto-created channel ${ch.name} for ${sess.host}:${sess.id}`);
    } catch (err: any) {
      console.warn(`[reconciler] Failed creating channel for ${sess.host}:${sess.id}: ${err.message}`);
    }
  }

  for (const ch of plan.remove) {
    try {
      await primary.deleteChannel(ch.id);
      removedCount++;
      console.log(`[reconciler] Auto-removed stale channel ${ch.name} (${ch.host}:${ch.session})`);
    } catch (err: any) {
      console.warn(`[reconciler] Failed removing stale channel ${ch.name}: ${err.message}`);
    }
  }

  // Purge lingering channels on inactive platform
  try {
    for (const ch of await other.listAgentChannels()) {
      try {
        await other.deleteChannel(ch.id);
      } catch {}
    }
  } catch {}

  return { created: createdCount, removed: removedCount };
}

/**
 * Run a full reconciliation pass across tmux sessions and managed-agent sessions.
 */
export async function reconcileAll(): Promise<ReconcileResult> {
  let agentRes = { created: 0, removed: 0 };
  let tmuxChanged = false;

  try {
    // 1. Reconcile tmux session channels
    await syncSessionChannels(undefined, async (channelId, host, sessionName) => {
      // Backfill hook if needed
    });
  } catch (err: any) {
    console.warn(`[reconciler] tmux sync pass failed: ${err.message}`);
  }

  try {
    // 2. Reconcile agent sessions
    agentRes = await reconcileAgentSessions();
  } catch (err: any) {
    console.warn(`[reconciler] agent sync pass failed: ${err.message}`);
  }

  return {
    tmuxChanges: tmuxChanged,
    agentChanges: agentRes.created > 0 || agentRes.removed > 0,
    createdAgents: agentRes.created,
    removedAgents: agentRes.removed,
  };
}
