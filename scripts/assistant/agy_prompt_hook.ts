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

    // Detect tmux session name
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

    // Determine target Discord channel
    const host = hostname().toLowerCase().includes("sophia") ? "sophia" : "eve";
    const sanitize = (s: string) =>
      s
        .toLowerCase()
        .replace(/[^a-z0-9-_]/g, "-")
        .replace(/-+/g, "-")
        .replace(/^-|-$/g, "");
    const expectedChannelName = `${host}-${sanitize(tmuxSession)}`;

    const channels = ((await callTool("discord.channels.list" as any, {})) || []) as any[];
    const match = channels.find(
      (c) =>
        c.type === "text" &&
        (String(c.name || "").toLowerCase() === expectedChannelName ||
          String(c.topic || "").toLowerCase().startsWith(`tmux:${host}:${tmuxSession.toLowerCase()}`) ||
          String(c.topic || "").toLowerCase().startsWith(`tmux:${tmuxSession.toLowerCase()}`))
    );

    if (match?.id) {
      // Mirror CLI prompt into Discord channel
      const content = `🖥️ **Nathan (terminal):**\n${prompt}`;
      await callTool("discord.message.send" as any, {
        channel_id: String(match.id),
        content: content.slice(0, 1900),
      });
    }
  } catch {
    // Non-blocking
  }

  console.log("{}");
}

await main();
