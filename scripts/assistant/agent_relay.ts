#!/usr/bin/env bun
/**
 * ~/.config/clara/scripts/assistant/agent_relay.ts
 *
 * Supervised worker that mirrors managed-agent events from every host, read from
 * the daemon's aggregated hub log (agent.hub.read), into the Discord/Webex channel bound to each session: assistant replies,
 * status reactions on the user's message, failures and questions.
 *
 * The cursor (global hub seq) is persisted, so a restart of this worker or of the
 * daemon resumes exactly where it stopped. A fresh install starts at the
 * current head and does not replay history.
 */

import { Database } from "bun:sqlite";
import { homedir } from "node:os";
import { join } from "node:path";
import { mkdirSync } from "node:fs";
import { callTool } from "../clara.ts";
import { getPlatformAdapter, getPrimaryAdapter } from "./platform/manager.ts";
import type { PlatformType } from "./platform/types.ts";
import type { Origin } from "./agent/binding.ts";
import { sessionLabel } from "./agent/sync.ts";
import { reconcileAll } from "./agent/reconciler.ts";
import {
  Relay,
  type ChannelRef,
  type Chat,
  type HostEvent,
  type PromptRecord,
  type Store,
} from "./agent/relay_core.ts";

const STATE_DIR = join(homedir(), ".local/share/clara");
mkdirSync(STATE_DIR, { recursive: true });
const db = new Database(join(STATE_DIR, "assistant_state.db"));
db.exec(`
  CREATE TABLE IF NOT EXISTS agent_relay_hub_cursor (id INTEGER PRIMARY KEY CHECK (id = 1), seq INTEGER NOT NULL);
  CREATE TABLE IF NOT EXISTS agent_relay_prompts (prompt_id TEXT PRIMARY KEY, rec TEXT NOT NULL, created_at INTEGER NOT NULL);
  CREATE TABLE IF NOT EXISTS agent_relay_turns (turn_id TEXT PRIMARY KEY, prompt_id TEXT NOT NULL);
  CREATE TABLE IF NOT EXISTS agent_relay_running (session TEXT PRIMARY KEY);
`);

const POLL_MS = Number(process.env.CLARA_AGENT_RELAY_POLL_MS || 1500);
const PAGE = 200;
const MAX_EVENT_FAILURES = 5;
const CHANNEL_REFRESH_MS = 10_000;
const SYNC_WAIT_MS = 60_000;
const AUTO_RECONCILE_INTERVAL_MS = Number(process.env.CLARA_RECONCILE_INTERVAL_MS || 45_000);

const sqlStore: Store = {
  getPrompt(id) {
    const row = db.query("SELECT rec FROM agent_relay_prompts WHERE prompt_id = ?").get(id) as any;
    return row ? (JSON.parse(row.rec) as PromptRecord) : undefined;
  },
  putPrompt(id, rec) {
    db.query(
      "INSERT OR REPLACE INTO agent_relay_prompts (prompt_id, rec, created_at) VALUES (?, ?, ?)"
    ).run(id, JSON.stringify(rec), Date.now());
  },
  getTurnPrompt(turnId) {
    const row = db.query("SELECT prompt_id FROM agent_relay_turns WHERE turn_id = ?").get(turnId) as any;
    return row?.prompt_id;
  },
  setTurnPrompt(turnId, promptId) {
    db.query("INSERT OR REPLACE INTO agent_relay_turns (turn_id, prompt_id) VALUES (?, ?)").run(
      turnId,
      promptId
    );
  },
  isRunning(session) {
    return Boolean(db.query("SELECT 1 FROM agent_relay_running WHERE session = ?").get(session));
  },
  setRunning(session, running) {
    if (running) db.query("INSERT OR IGNORE INTO agent_relay_running (session) VALUES (?)").run(session);
    else db.query("DELETE FROM agent_relay_running WHERE session = ?").run(session);
  },
};

class ChannelCache {
  private map = new Map<string, ChannelRef>();
  private refreshedAt = 0;

  async find(host: string, session: string): Promise<ChannelRef | null> {
    const key = `${host}:${session}`;
    let hit = this.map.get(key);
    if (!hit && Date.now() - this.refreshedAt > CHANNEL_REFRESH_MS) {
      await this.refresh();
      hit = this.map.get(key);
    }
    return hit ?? null;
  }

  set(host: string, session: string, ref: ChannelRef) {
    this.map.set(`${host}:${session}`, ref);
  }

  private async refresh() {
    this.refreshedAt = Date.now();
    for (const platform of ["discord", "webex"] as PlatformType[]) {
      try {
        for (const c of await getPlatformAdapter(platform).listAgentChannels()) {
          this.map.set(`${c.host}:${c.session}`, { platform, channelId: c.id });
        }
      } catch (err: any) {
        console.warn(`[agent_relay] listing ${platform} channels failed: ${err.message}`);
      }
    }
  }
}

const channels = new ChannelCache();

const chat: Chat = {
  channelFor: (host, session) => channels.find(host, session),
  async ensureChannel(host, session, payload) {
    const existing = await channels.find(host, session);
    if (existing) return existing;
    try {
      const primary = getPrimaryAdapter();
      const label = sessionLabel({
        id: session,
        host,
        agent: payload?.agent || "agent",
        dir: payload?.dir || "",
        title: payload?.title,
        state: "starting",
      });
      const ch = await primary.ensureAgentChannel(host, session, label, payload?.dir);
      const ref: ChannelRef = { platform: primary.platform, channelId: ch.id };
      channels.set(host, session, ref);
      return ref;
    } catch (err: any) {
      console.warn(`[agent_relay] ensureChannel failed for ${host}:${session}: ${err.message}`);
      return null;
    }
  },
  async post(ch, text) {
    await getPlatformAdapter(ch.platform).sendMessage(ch.channelId, text);
  },
  async setStatus(origin: Origin, prev, next) {
    const a = getPlatformAdapter(origin.platform);
    if (prev.emoji) await a.removeReaction(origin.channelId, prev.indicator || origin.messageId, prev.emoji);
    if (!next) return undefined;
    const id = await a.addReaction(origin.channelId, origin.messageId, next);
    return typeof id === "string" && id ? id : undefined;
  },
};

function loadCursor(): number | null {
  const row = db.query("SELECT seq FROM agent_relay_hub_cursor WHERE id = 1").get() as any;
  return row ? Number(row.seq) : null;
}

function saveCursor(seq: number) {
  db.query("INSERT OR REPLACE INTO agent_relay_hub_cursor (id, seq) VALUES (1, ?)").run(seq);
}

async function readPage(
  after: number
): Promise<{ events: HostEvent[]; next: number; synced: boolean }> {
  const res = (await callTool("agent.hub.read" as any, { after, limit: PAGE })) as any;
  const events = ((res?.events || []) as any[]).map((e) => ({
    seq: Number(e.seq),
    host_seq: Number(e.host_seq),
    host: String(e.host),
    session: String(e.session || ""),
    type: String(e.type),
    payload: typeof e.payload === "string" ? JSON.parse(e.payload) : e.payload,
  }));
  return { events, next: Number(res?.next ?? after), synced: Boolean(res?.synced) };
}

// Waits for the hub to finish its first pull from every host, so a still-running
// backfill of old history is never mistaken for new activity.
async function seekHead(): Promise<number> {
  const deadline = Date.now() + SYNC_WAIT_MS;
  let after = 0;
  for (;;) {
    const page = await readPage(after);
    if (page.events.length > 0) {
      after = page.next;
      continue;
    }
    if (page.synced || Date.now() > deadline) return after;
    await new Promise((r) => setTimeout(r, 1000));
  }
}

async function main() {
  const relay = new Relay(sqlStore, chat);
  let cursor = loadCursor();
  if (cursor === null) {
    cursor = await seekHead();
    saveCursor(cursor);
    console.log(`[agent_relay] fresh start at hub seq=${cursor}`);
  }

  let failures = 0;
  let stopping = false;
  let lastReconcile = Date.now();
  for (const sig of ["SIGINT", "SIGTERM"] as const) process.on(sig, () => (stopping = true));

  // Run an initial reconciliation pass on startup
  reconcileAll().catch((err: any) =>
    console.warn(`[agent_relay] Initial auto-reconcile error: ${err.message}`)
  );

  while (!stopping) {
    try {
      // Periodic automatic session reconciliation (tmux & agents across all hosts)
      if (Date.now() - lastReconcile > AUTO_RECONCILE_INTERVAL_MS) {
        lastReconcile = Date.now();
        reconcileAll().catch((err: any) =>
          console.warn(`[agent_relay] Periodic auto-reconcile error: ${err.message}`)
        );
      }

      const page = await readPage(cursor);
      for (const ev of page.events) {
        try {
          await relay.handle(ev);
          failures = 0;
        } catch (err: any) {
          console.error(`[agent_relay] event ${ev.type}#${ev.seq} failed: ${err.message}`);
          // Retry on the next poll so a transient chat outage loses nothing,
          // but never let one poison event block the stream forever.
          if (++failures < MAX_EVENT_FAILURES) break;
          failures = 0;
        }
        cursor = ev.seq ?? cursor;
        saveCursor(cursor);
      }
      if (page.events.length === PAGE && failures === 0) continue;
    } catch (err: any) {
      console.error(`[agent_relay] poll failed: ${err.message}`);
    }
    await new Promise((r) => setTimeout(r, POLL_MS));
  }
}

await main();
