#!/usr/bin/env bun
/**
 * ~/.config/clara/scripts/assistant/agy_prompt_hook.ts
 *
 * Antigravity (agy) `UserPromptSubmit` Lifecycle Hook.
 * Fires automatically whenever a user prompt is submitted to an agy session
 * (either from the terminal CLI, IDE, or automated pipe).
 *
 * If the session is inside tmux and has an active `#eve-<name>` or `#sophia-<name>`
 * channel, this hook mirrors terminal prompts into Discord so that conversation
 * history in Discord stays complete regardless of where the prompt originated!
 */

import { Database } from "bun:sqlite";
import { existsSync, mkdirSync } from "node:fs";
import { homedir, hostname } from "node:os";
import { join } from "node:path";
import { callTool } from "../clara.ts";
import { getPrimaryAdapter } from "./platform/manager.ts";

const STATE_DIR = join(homedir(), ".local/share/clara");
mkdirSync(STATE_DIR, { recursive: true });
const stateDb = new Database(join(STATE_DIR, "assistant_state.db"));

stateDb.exec(`
  CREATE TABLE IF NOT EXISTS agy_prompt_mirror (
    prompt_hash TEXT PRIMARY KEY,
    session_name TEXT,
    created_at INTEGER
  );
`);

function quickHash(s: string): string {
  let h = 0;
  for (let i = 0; i < s.length; i++) {
    h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
  }
  return h.toString(36);
}

async function main() {
  // Managed (ACP) sessions are relayed by agent_relay.ts; never double post.
  if (process.env.CLARA_MANAGED_AGENT) {
    console.log("{}");
    return;
  }
  let rawInput = "";
  try {
    rawInput = await Bun.stdin.text();
  } catch {}

  // Output empty JSON to keep hook execution non-blocking
  try {
    const payload = rawInput ? JSON.parse(rawInput) : {};
    const prompt = String(payload.prompt || payload.userPrompt || payload.content || "").trim();

    if (!prompt) {
      console.log("{}");
      return;
    }

    // Detect current tmux session name and pane index
    let tmuxSession = "";
    if (process.env.TMUX) {
      try {
        const proc = Bun.spawn(
          ["/opt/homebrew/bin/tmux", "display-message", "-p", "#{session_name}|#{pane_index}"],
          { stdout: "pipe", stderr: "pipe" }
        );
        const out = (await new Response(proc.stdout).text()).trim();
        const [sName, pIdx] = out.split("|");

        if (sName) {
          const procList = Bun.spawn(
            [
              "/opt/homebrew/bin/tmux",
              "list-panes",
              "-s",
              "-t",
              `${sName}:`,
              "-F",
              "#{pane_id}|#{pane_index}|#{pane_current_command}",
            ],
            { stdout: "pipe", stderr: "pipe" }
          );
          const listText = (await new Response(procList.stdout).text()).trim();
          const agentPanes: string[] = [];
          for (const line of listText.split("\n")) {
            if (!line.trim()) continue;
            const [_pid, idx, cmd] = line.split("|");
            const c = (cmd || "").toLowerCase();
            if (c.includes("agy") || c.includes("antigravity") || c.includes("opencode")) {
              agentPanes.push(idx);
            }
          }
          if (agentPanes.length > 1 && pIdx !== undefined) {
            tmuxSession = `${sName}:${pIdx}`;
          } else {
            tmuxSession = sName;
          }
        }
      } catch {}
    }

    if (!tmuxSession) {
      console.log("{}");
      return;
    }

    // Check if this prompt was dispatched by chat_router.ts (marked in stateDb)
    const promptHash = `${tmuxSession}:${quickHash(prompt)}`;
    const alreadyFromDiscord = stateDb
      .query("SELECT 1 FROM agy_prompt_mirror WHERE prompt_hash = ?")
      .get(promptHash);

    if (alreadyFromDiscord) {
      // Prompt originated from Discord, don't echo it back
      console.log("{}");
      return;
    }

    // Look up target channel/space on primary platform
    const host = hostname().toLowerCase().includes("sophia") ? "sophia" : "eve";
    const primary = getPrimaryAdapter();
    const match = await primary.findChannelForSession(host, tmuxSession);

    if (match?.id) {
      // Mirror CLI prompt into primary channel
      const content = `🖥️ **Nathan (terminal):**\n${prompt}`;
      await primary.sendMessage(match.id, content);
    }
  } catch {
    // Non-blocking
  }

  console.log("{}");
}

await main();
