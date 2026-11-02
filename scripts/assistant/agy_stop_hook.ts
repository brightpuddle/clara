#!/usr/bin/env bun
/**
 * ~/.config/clara/scripts/assistant/agy_stop_hook.ts
 *
 * Antigravity (agy) `Stop` Lifecycle Hook.
 * Fires automatically whenever an agy session finishes a turn and is waiting for user input.
 *
 * If the session is running inside a tmux session that has a corresponding
 * `#eve-<session>` or `#sophia-<session>` Discord channel, this hook reads the
 * final assistant response from `transcriptPath` and posts it directly to that
 * Discord channel!
 */

import { Database } from "bun:sqlite";
import { existsSync, mkdirSync, readFileSync } from "node:fs";
import { homedir, hostname } from "node:os";
import { join } from "node:path";
import { callTool } from "../clara.ts";
import { getPrimaryAdapter } from "./platform/manager.ts";

const STATE_DIR = join(homedir(), ".local/share/clara");
mkdirSync(STATE_DIR, { recursive: true });
const stateDb = new Database(join(STATE_DIR, "assistant_state.db"));

stateDb.exec(`
  CREATE TABLE IF NOT EXISTS agy_hook_dedup (
    step_key TEXT PRIMARY KEY,
    sent_at INTEGER
  );
  CREATE TABLE IF NOT EXISTS pending_reactions (
    channel_id TEXT,
    message_id TEXT,
    session_name TEXT,
    created_at INTEGER,
    PRIMARY KEY(channel_id, message_id)
  );
`);

async function main() {
  let rawInput = "";
  try {
    rawInput = await Bun.stdin.text();
  } catch {}

  // Always output `{}` to stdout at the end so agy stops normally without blocking
  try {
    const payload = rawInput ? JSON.parse(rawInput) : {};
    const transcriptPath = String(payload.transcriptPath || "");
    const conversationId = String(payload.conversationId || "unknown");

    // Detect current tmux session name if running inside tmux
    let tmuxSession = "";
    if (process.env.TMUX) {
      const proc = Bun.spawn(["/opt/homebrew/bin/tmux", "display-message", "-p", "#S"], {
        stdout: "pipe",
        stderr: "pipe",
      });
      tmuxSession = (await new Response(proc.stdout).text()).trim();
    }

    if (!tmuxSession) {
      console.log("{}");
      return;
    }

    // Determine host prefix (eve or sophia)
    const host = hostname().toLowerCase().includes("sophia") ? "sophia" : "eve";
    const sanitize = (s: string) =>
      s
        .toLowerCase()
        .replace(/[^a-z0-9-_]/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-|-$/g, "");
    const expectedChannelName = `${host}-${sanitize(tmuxSession)}`;

    // Extract the latest assistant response from transcript.jsonl
    let lastAssistantMessage = "";
    let stepIndex = -1;
    if (transcriptPath && existsSync(transcriptPath)) {
      const lines = readFileSync(transcriptPath, "utf8")
        .trim()
        .split("\n")
        .filter(Boolean);
      for (let i = lines.length - 1; i >= 0; i--) {
        try {
          const step = JSON.parse(lines[i]);
          if (
            step.type === "PLANNER_RESPONSE" &&
            (!step.tool_calls || step.tool_calls.length === 0) &&
            step.content &&
            String(step.content).trim().length > 0
          ) {
            lastAssistantMessage = String(step.content).trim();
            stepIndex = Number(step.step_index ?? i);
            break;
          }
        } catch {}
      }
    }

    if (!lastAssistantMessage) {
      console.log("{}");
      return;
    }

    // Deduplicate by conversationId + stepIndex so the same turn is never posted twice
    const stepKey = `${conversationId}:${stepIndex}`;
    const alreadySent = stateDb
      .query("SELECT 1 FROM agy_hook_dedup WHERE step_key = ?")
      .get(stepKey);
    if (alreadySent) {
      console.log("{}");
      return;
    }
    stateDb
      .query("INSERT OR IGNORE INTO agy_hook_dedup (step_key, sent_at) VALUES (?, ?)")
      .run(stepKey, Date.now());

    // Look up the bound channel/space on the primary platform
    const primary = getPrimaryAdapter();
    const match = await primary.findChannelForSession(host as "eve" | "sophia", tmuxSession);

    if (match?.id) {
      // Resolve any pending message reactions in this channel
      try {
        const pending = stateDb
          .query("SELECT message_id FROM pending_reactions WHERE channel_id = ? AND session_name = ?")
          .all(match.id, tmuxSession) as { message_id: string }[];

        for (const p of pending) {
          await primary.removeReaction(match.id, p.message_id, "⏳");
          await primary.addReaction(match.id, p.message_id, "✅");
        }
        stateDb
          .query("DELETE FROM pending_reactions WHERE channel_id = ? AND session_name = ?")
          .run(match.id, tmuxSession);
      } catch {}

      // Post final response chunks via primary adapter
      await primary.sendMessage(match.id, lastAssistantMessage);
    }
  } catch {
    // Never fail or block agy stop hook
  }

  console.log("{}");
}

await main();
