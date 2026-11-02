#!/usr/bin/env bun
/**
 * ~/.config/clara/scripts/assistant/chat_router.ts
 *
 * Unified Webex (1:1 Bot DM) + Discord (Multiplexed Agent Channels + Voice Notes)
 * Control Plane & Conversational Router for Clara.
 *
 * Features:
 * 1. Dual-Platform Support (Webex 1:1 + Discord Server):
 *    - Replies natively to whichever platform & channel/room you messaged from.
 *
 * 2. Discord Per-Session Channel Multiplexing (#session-<name>):
 *    - `sync` / `/sync`: Auto-creates `#session-<name>` Discord channels under
 *      "Active Agent Sessions" for every running tmux session, and cleans up dead ones.
 *    - Inside `#session-<name>`:
 *      • Any normal message/voice note is sent straight to that tmux session (`pane.send_keys`)
 *        and replies with the updated terminal output!
 *      • `? <question>` captures the tmux pane and asks the AI to explain/summarize it.
 *      • `capture` shows the latest 35 lines of the pane.
 *      • `/kill` terminates the tmux session and deletes the Discord channel.
 *
 * 3. Mobile Voice Message Dictation (Local Whisper STT):
 *    - Automatically transcribes Discord/Webex voice messages via `ffmpeg` + `whisper-cli`
 *      using a custom technical vocabulary prompt.
 *
 * 4. Conversational Tool-Using Assistant (`gpt-6-luna` default):
 *    - Full access to Calendar, Reminders, Mail, Webex, Discord, Obsidian ZK,
 *      Minimized Chrome, Tmux, Taskwarrior, SQLite, Shell, and headless `opencode`.
 */

import { Database } from "bun:sqlite";
import { parseArgs } from "node:util";
import { homedir, tmpdir } from "node:os";
import { join } from "node:path";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "node:fs";
import { callTool, readEvent, runID, triggerID } from "../clara.ts";

const HOME = homedir();
const STATE_DIR = join(HOME, ".local/share/clara");
const MODELS_DIR = join(STATE_DIR, "models");
mkdirSync(STATE_DIR, { recursive: true });
mkdirSync(MODELS_DIR, { recursive: true });

const ASSISTANT_DB_PATH = join(STATE_DIR, "assistant_state.db");
const CLARA_DB_PATH = join(STATE_DIR, "clara.db");
const DEFAULT_WEBEX_ROOM_ID =
  "Y2lzY29zcGFyazovL3VzL1JPT00vYWY2MTJmYTAtNDhmNy0xMWYxLThiOGItZmRjMDNjNDQzZGYz";
// Default LLM configuration: Local-First on Apple Silicon via MLX (Qwen 2.5-Coder 7B)
// Fallback to Copilot API gateway (gpt-6-luna / claude-sonnet) if CLARA_USE_CLOUD_LLM=true or MLX is unavailable.
const LOCAL_MLX_URL = process.env.CLARA_LOCAL_LLM_URL || "http://127.0.0.1:8080/v1";
const LOCAL_MLX_MODEL =
  process.env.CLARA_LOCAL_LLM_MODEL || "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit";
const CLOUD_LLM_URL = process.env.CLARA_CLOUD_LLM_URL || "http://localhost:4141/v1";
const CLOUD_LLM_MODEL = process.env.CLARA_CLOUD_CHAT_MODEL || "gpt-6-luna";

const PREFER_CLOUD = process.env.CLARA_USE_CLOUD_LLM === "true";
const LLM_BASE_URL = PREFER_CLOUD ? CLOUD_LLM_URL : LOCAL_MLX_URL;
const LLM_MODEL = PREFER_CLOUD ? CLOUD_LLM_MODEL : LOCAL_MLX_MODEL;
const WHISPER_MODEL_PATH = join(MODELS_DIR, "ggml-base.en.bin");

const { values } = parseArgs({
  args: Bun.argv.slice(2),
  options: {
    prompt: { type: "string" },
    "dry-run": { type: "boolean", default: false },
    room: { type: "string" },
    platform: { type: "string" }, // "webex" | "discord"
  },
  allowPositionals: true,
});

type Platform = "webex" | "discord";

// ==========================================
// SQLite State Store (Deduplication + Memory)
// ==========================================
const stateDb = new Database(ASSISTANT_DB_PATH);
stateDb.exec(`
  CREATE TABLE IF NOT EXISTS processed_messages (
    message_id TEXT PRIMARY KEY,
    room_id TEXT,
    person_email TEXT,
    text TEXT,
    processed_at INTEGER
  );

  CREATE TABLE IF NOT EXISTS chat_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    room_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at INTEGER NOT NULL
  );

  CREATE TABLE IF NOT EXISTS work_items (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    sender TEXT,
    title TEXT NOT NULL,
    details TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    reminder_id TEXT,
    prep_note_path TEXT,
    missing_tools TEXT,
    estimated_minutes INTEGER DEFAULT 15,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
  );

  CREATE TABLE IF NOT EXISTS agy_prompt_mirror (
    prompt_hash TEXT PRIMARY KEY,
    session_name TEXT,
    created_at INTEGER
  );

  CREATE TABLE IF NOT EXISTS pending_reactions (
    channel_id TEXT,
    message_id TEXT,
    session_name TEXT,
    created_at INTEGER,
    PRIMARY KEY(channel_id, message_id)
  );

  CREATE TABLE IF NOT EXISTS channel_backfill_done (
    channel_id TEXT PRIMARY KEY,
    backfilled_at INTEGER
  );
`);

function isMessageProcessed(msgId: string): boolean {
  if (!msgId || msgId === "unknown" || msgId.startsWith("cli-")) return false;
  const row = stateDb
    .query("SELECT 1 FROM processed_messages WHERE message_id = ?")
    .get(msgId);
  return Boolean(row);
}

function markMessageProcessed(
  msgId: string,
  roomId: string,
  sender: string,
  text: string
) {
  if (!msgId || msgId === "unknown") return;
  stateDb
    .query(
      "INSERT OR IGNORE INTO processed_messages (message_id, room_id, person_email, text, processed_at) VALUES (?, ?, ?, ?, ?)"
    )
    .run(msgId, roomId, sender, text, Date.now());
}

function getRecentChatHistory(
  roomId: string,
  limit = 14
): Array<{ role: string; content: string }> {
  const rows = stateDb
    .query(
      `SELECT role, content FROM (
         SELECT id, role, content FROM chat_history
         WHERE room_id = ?
         ORDER BY id DESC LIMIT ?
       ) ORDER BY id ASC`
    )
    .all(roomId, limit) as Array<{ role: string; content: string }>;
  return rows || [];
}

function appendChatHistory(roomId: string, role: string, content: string) {
  stateDb
    .query(
      "INSERT INTO chat_history (room_id, role, content, created_at) VALUES (?, ?, ?, ?)"
    )
    .run(roomId, role, content, Date.now());
}

function clearChatHistory(roomId: string) {
  stateDb.query("DELETE FROM chat_history WHERE room_id = ?").run(roomId);
}

// ==========================================
// Unified Reply Helper (Webex & Discord)
// ==========================================
function splitDiscordChunks(text: string, maxLen = 1900): string[] {
  if (text.length <= maxLen) return [text];
  const chunks: string[] = [];
  let remaining = text;
  while (remaining.length > maxLen) {
    let splitIdx = remaining.lastIndexOf("\n", maxLen);
    if (splitIdx < 400) splitIdx = maxLen;
    chunks.push(remaining.slice(0, splitIdx));
    remaining = remaining.slice(splitIdx).trimStart();
  }
  if (remaining.length > 0) chunks.push(remaining);
  return chunks;
}

async function sendReply(
  platform: Platform,
  targetId: string,
  markdown: string,
  isDryRun = false
) {
  console.log(`\n--- Reply [${platform}:${targetId}] ---\n${markdown}\n-----------------------\n`);
  if (isDryRun) return;

  if (platform === "discord") {
    const chunks = splitDiscordChunks(markdown);
    for (const chunk of chunks) {
      await callTool("discord.message.send" as any, {
        channel_id: targetId,
        content: chunk,
      });
    }
    return;
  }

  try {
    await callTool("webex.notification.send" as any, {
      room_id: targetId,
      markdown,
    });
  } catch {
    await callTool("notify.send" as any, {
      message: markdown,
    });
  }
}

// ==========================================
// Local Whisper Voice Message Transcription
// ==========================================
async function ensureWhisperModel(): Promise<string> {
  if (existsSync(WHISPER_MODEL_PATH)) return WHISPER_MODEL_PATH;
  console.log("[chat_router] Downloading ggml-base.en.bin for local voice transcription...");
  const url =
    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin";
  const resp = await fetch(url);
  if (!resp.ok) {
    throw new Error(`Failed to download Whisper model: ${resp.status}`);
  }
  const buf = new Uint8Array(await resp.arrayBuffer());
  writeFileSync(WHISPER_MODEL_PATH, buf);
  return WHISPER_MODEL_PATH;
}

async function transcribeVoiceAttachment(attachmentUrl: string): Promise<string> {
  const modelPath = await ensureWhisperModel();
  const id = `${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
  const rawPath = join(tmpdir(), `clara-voice-${id}.audio`);
  const wavPath = join(tmpdir(), `clara-voice-${id}.wav`);

  try {
    const resp = await fetch(attachmentUrl);
    if (!resp.ok) throw new Error(`Download audio failed: ${resp.status}`);
    writeFileSync(rawPath, new Uint8Array(await resp.arrayBuffer()));

    // Convert to 16kHz mono PCM WAV via ffmpeg
    const ff = Bun.spawn(
      [
        "/opt/homebrew/bin/ffmpeg",
        "-y",
        "-i",
        rawPath,
        "-ar",
        "16000",
        "-ac",
        "1",
        "-c:a",
        "pcm_s16le",
        wavPath,
      ],
      { stdout: "pipe", stderr: "pipe" }
    );
    await ff.exited;

    const whisper = Bun.spawn(
      [
        "/opt/homebrew/bin/whisper-cli",
        "-m",
        modelPath,
        "-f",
        wavPath,
        "--no-timestamps",
        "-nt",
        "--prompt",
        "Clara, opencode, agy, tmux, Webex, Discord, Obsidian, Cisco, CML, Mac mini, MyPerformance, timesheet",
      ],
      { stdout: "pipe", stderr: "pipe" }
    );
    const out = await new Response(whisper.stdout).text();
    await whisper.exited;

    return out
      .split("\n")
      .map((l) => l.trim())
      .filter((l) => l && !l.startsWith("["))
      .join(" ")
      .trim();
  } finally {
    try {
      rmSync(rawPath, { force: true });
      rmSync(wavPath, { force: true });
    } catch {}
  }
}

// ==========================================
// Discord <-> Tmux Session Channel Multiplexer
// ==========================================
const LOCAL_HOST_PREFIX = "eve";
const LAPTOP_HOST_PREFIX = "sophia";

interface BoundSessionInfo {
  host: "eve" | "sophia";
  sessionName: string;
}

function sanitizeChannelSuffix(sessionName: string): string {
  return sessionName
    .toLowerCase()
    .replace(/[^a-z0-9-_]/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

async function syncDiscordSessionChannels(): Promise<string> {
  const eveSessions = ((await callTool("tmux.session.list" as any, {})) || []) as any[];
  let sophiaSessions: any[] = [];
  try {
    const res = await callTool("laptop.tmux.session.list" as any, {});
    if (Array.isArray(res)) sophiaSessions = res;
  } catch {}

  const channels = ((await callTool("discord.channels.list" as any, {})) || []) as any[];

  const activeMap = new Map<string, BoundSessionInfo>(); // channelName -> { host, sessionName }
  for (const s of eveSessions) {
    const chName = `${LOCAL_HOST_PREFIX}-${sanitizeChannelSuffix(s.name)}`;
    activeMap.set(chName, { host: "eve", sessionName: s.name });
  }
  for (const s of sophiaSessions) {
    const chName = `${LAPTOP_HOST_PREFIX}-${sanitizeChannelSuffix(s.name)}`;
    activeMap.set(chName, { host: "sophia", sessionName: s.name });
  }

  const created: string[] = [];
  const existing: string[] = [];
  const removed: string[] = [];

  // 1. Ensure a channel exists for every active tmux session on eve (and sophia when connected)
  for (const [chName, info] of activeMap.entries()) {
    let targetChannelId = "";
    const found = channels.find(
      (c) => c.type === "text" && String(c.name).toLowerCase() === chName
    );
    if (found) {
      targetChannelId = String(found.id);
      existing.push(`#${chName} (\`${info.host}:${info.sessionName}\`)`);
    } else {
      const createdCh = (await callTool("discord.channel.create" as any, {
        name: chName,
        topic: `tmux:${info.host}:${info.sessionName} — Type directly to send keys to ${info.host} tmux session '${info.sessionName}', or prefix with '? ' to ask AI`,
        category: "Active Agent Sessions",
      })) as any;
      if (createdCh?.id) targetChannelId = String(createdCh.id);
      created.push(`#${chName} (\`${info.host}:${info.sessionName}\`)`);
    }

    // Backfill history idempotently (only once per channel, checking existing discord messages)
    if (targetChannelId && info.host === "eve") {
      await backfillChannelHistoryIfNew(targetChannelId, info.sessionName);
    }
  }

  // 2. Remove stale #eve-*, #sophia-*, or legacy #session-* channels whose tmux session no longer exists
  for (const c of channels) {
    const name = String(c.name || "").toLowerCase();
    const isManagedPrefix =
      name.startsWith(`${LOCAL_HOST_PREFIX}-`) ||
      name.startsWith(`${LAPTOP_HOST_PREFIX}-`) ||
      name.startsWith("session-");
    if (c.type === "text" && isManagedPrefix && !activeMap.has(name)) {
      try {
        await callTool("discord.channel.delete" as any, { channel_id: c.id });
        removed.push(`#${name}`);
      } catch {}
    }
  }

  return [
    `### Discord ↔ Tmux Session Multiplexer Synced`,
    `- **Active Session Channels (${activeMap.size}):** ${[...created, ...existing].join(", ") || "None"}`,
    created.length > 0 ? `- **Created:** ${created.join(", ")}` : "",
    removed.length > 0 ? `- **Cleaned Up Ended Sessions:** ${removed.join(", ")}` : "",
    `\n*Tip: Jump into any \`#eve-*\` or \`#sophia-*\` channel on Discord to talk directly to that session, or prefix with \`?\` to ask AI about its output.*`,
  ]
    .filter(Boolean)
    .join("\n");
}

async function backfillChannelHistoryIfNew(
  channelId: string,
  sessionName: string
): Promise<void> {
  try {
    // Check local DB flag
    const alreadyDone = stateDb
      .query("SELECT 1 FROM channel_backfill_done WHERE channel_id = ?")
      .get(channelId);
    if (alreadyDone) return;

    // Check if Discord channel already has messages
    try {
      const msgs = (await callTool("discord.messages.list" as any, {
        channel_id: channelId,
        limit: 10,
      })) as any[];
      if (Array.isArray(msgs) && msgs.length > 0) {
        stateDb
          .query("INSERT OR IGNORE INTO channel_backfill_done (channel_id, backfilled_at) VALUES (?, ?)")
          .run(channelId, Date.now());
        return;
      }
    } catch {}

    // Find latest transcript.jsonl
    const brainDir = join(HOME, ".gemini/antigravity-cli/brain");
    if (!existsSync(brainDir)) return;

    const entries = Bun.spawnSync(["ls", "-1td", `${brainDir}/*`]);
    const dirs = entries.stdout.toString().trim().split("\n").filter(Boolean);
    let targetTranscript = "";
    for (const d of dirs.slice(0, 10)) {
      const p = join(d, ".system_generated/logs/transcript.jsonl");
      if (existsSync(p)) {
        targetTranscript = p;
        break;
      }
    }

    if (!targetTranscript) return;

    const lines = (await Bun.file(targetTranscript).text())
      .trim()
      .split("\n")
      .filter(Boolean);

    const turns: string[] = [];
    for (let i = lines.length - 1; i >= 0 && turns.length < 4; i--) {
      try {
        const step = JSON.parse(lines[i]);
        if (step.type === "USER_INPUT" && step.content) {
          turns.unshift(`👤 **User:** ${String(step.content).trim().slice(0, 400)}`);
        } else if (
          step.type === "PLANNER_RESPONSE" &&
          (!step.tool_calls || step.tool_calls.length === 0) &&
          step.content
        ) {
          turns.unshift(`🤖 **Assistant:** ${String(step.content).trim().slice(0, 800)}`);
        }
      } catch {}
    }

    if (turns.length > 0) {
      const header = `📜 **--- Attached to Active Session (\`${sessionName}\`) Recent Context ---**\n\n`;
      const combined = header + turns.join("\n\n");
      await callTool("discord.message.send" as any, {
        channel_id: channelId,
        content: combined.slice(0, 1900),
      });
    }

    stateDb
      .query("INSERT OR IGNORE INTO channel_backfill_done (channel_id, backfilled_at) VALUES (?, ?)")
      .run(channelId, Date.now());
  } catch (err: any) {
    console.error(`[chat_router] Error backfilling channel ${channelId}:`, err);
  }
}

function resolveBoundTmuxSession(
  channelName: string,
  channelTopic: string
): BoundSessionInfo | null {
  const topicHostMatch = String(channelTopic || "").match(/^tmux:(eve|sophia):([^\s—-]+)/i);
  if (topicHostMatch?.[1] && topicHostMatch?.[2]) {
    return {
      host: topicHostMatch[1].toLowerCase() as "eve" | "sophia",
      sessionName: topicHostMatch[2].trim(),
    };
  }
  const legacyTopicMatch = String(channelTopic || "").match(/^tmux:([^\s:—-]+)/i);
  if (legacyTopicMatch?.[1]) {
    return { host: "eve", sessionName: legacyTopicMatch[1].trim() };
  }
  const name = String(channelName || "").toLowerCase();
  if (name.startsWith(`${LOCAL_HOST_PREFIX}-`)) {
    return { host: "eve", sessionName: name.slice(`${LOCAL_HOST_PREFIX}-`.length) };
  }
  if (name.startsWith(`${LAPTOP_HOST_PREFIX}-`)) {
    return { host: "sophia", sessionName: name.slice(`${LAPTOP_HOST_PREFIX}-`.length) };
  }
  if (name.startsWith("session-")) {
    return { host: "eve", sessionName: name.slice("session-".length) };
  }
  return null;
}

// ==========================================
// Fast-Path Built-in Commands
// ==========================================
async function handleStatusCommand(): Promise<string> {
  let triggerRuns24h = 0;
  let failedRuns24h = 0;
  try {
    const db = new Database(CLARA_DB_PATH, { readonly: true });
    const since = Date.now() - 24 * 60 * 60 * 1000;
    const row = db
      .query(
        `SELECT count(*) as total,
                sum(case when status != 'success' then 1 else 0 end) as failed
         FROM trigger_runs WHERE started_at >= ?`
      )
      .get(since) as any;
    triggerRuns24h = Number(row?.total || 0);
    failedRuns24h = Number(row?.failed || 0);
    db.close();
  } catch {}

  let triggers: any[] = [];
  try {
    const proc = Bun.spawn(["clara", "trigger", "list", "--output", "json"], {
      stdout: "pipe",
      stderr: "pipe",
    });
    const out = await new Response(proc.stdout).text();
    const parsed = JSON.parse(out);
    if (Array.isArray(parsed)) triggers = parsed;
  } catch {}

  let tmuxSessions: any[] = [];
  try {
    const res = await callTool("tmux.session.list" as any, {});
    if (Array.isArray(res)) tmuxSessions = res;
  } catch {}

  let laptopStatus = "Offline (queued mode)";
  try {
    const proc = Bun.spawn(["clara", "mcp", "list", "--output", "json"], {
      stdout: "pipe",
      stderr: "pipe",
    });
    const out = await new Response(proc.stdout).text();
    const parsed = JSON.parse(out);
    const managed = parsed?.managed || [];
    const laptop = managed.find((m: any) => m.name === "laptop");
    if (laptop) {
      laptopStatus =
        laptop.status === "RUNNING" ? "Connected (VPN Ready)" : `${laptop.status}`;
    }
  } catch {}

  let mlxStatus = "Offline";
  try {
    const res = await fetch(`${LOCAL_MLX_URL}/chat/completions`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model: LOCAL_MLX_MODEL,
        messages: [{ role: "user", content: "ping" }],
        max_tokens: 1,
      }),
    });
    if (res.ok) mlxStatus = `Online (\`${LOCAL_MLX_MODEL.split("/")[1] || LOCAL_MLX_MODEL}\`)`;
  } catch {}

  const enabledCount = triggers.filter((t) => t.enabled !== false).length;
  const sessionNames =
    tmuxSessions.length > 0
      ? tmuxSessions.map((s) => `\`${s.name}\``).join(", ")
      : "None";

  return [
    `### Clara System Status`,
    `- **Coordinator Node:** Mac mini (64GB) — Online`,
    `- **Active Assistant Engine:** ${PREFER_CLOUD ? `Cloud (${CLOUD_LLM_MODEL})` : `Local MLX (${LOCAL_MLX_MODEL.split("/")[1] || LOCAL_MLX_MODEL})`}`,
    `- **Local MLX Apple Silicon Model:** ${mlxStatus}`,
    `- **Laptop VPN Satellite:** ${laptopStatus}`,
    `- **Active Triggers:** ${enabledCount} enabled (${triggers.length} total)`,
    `- **24h Automation Runs:** ${triggerRuns24h} total (${failedRuns24h} failed)`,
    `- **Active Tmux Sessions:** ${sessionNames}`,
  ].join("\n");
}

async function handleRunsCommand(): Promise<string> {
  try {
    const db = new Database(CLARA_DB_PATH, { readonly: true });
    const rows = db
      .query(
        `SELECT id, trigger_id, status, duration_ms, started_at, stderr
         FROM trigger_runs
         ORDER BY started_at DESC
         LIMIT 8`
      )
      .all() as any[];
    db.close();

    if (!rows || rows.length === 0) {
      return "No recent trigger runs recorded.";
    }

    let md = `### Recent Execution Runs\n`;
    for (const r of rows) {
      const icon = r.status === "success" ? "✅" : "❌";
      const timeStr = new Date(Number(r.started_at)).toLocaleTimeString("en-US", {
        hour: "numeric",
        minute: "2-digit",
      });
      const dur = r.duration_ms ? `${(Number(r.duration_ms) / 1000).toFixed(1)}s` : "-";
      md += `- ${icon} **${r.trigger_id}** (${dur} at ${timeStr}) — \`${r.id.slice(0, 8)}\`\n`;
      if (r.status !== "success" && r.stderr) {
        const snippet = String(r.stderr).trim().split("\n").slice(-1)[0]?.slice(0, 100);
        if (snippet) md += `  - *Error:* \`${snippet}\`\n`;
      }
    }
    return md;
  } catch (err: any) {
    return `Failed to query runs: ${err.message}`;
  }
}

async function handleTriggersCommand(): Promise<string> {
  try {
    const proc = Bun.spawn(["clara", "trigger", "list", "--output", "json"], {
      stdout: "pipe",
      stderr: "pipe",
    });
    const out = await new Response(proc.stdout).text();
    const triggers = JSON.parse(out) as any[];
    if (!Array.isArray(triggers) || triggers.length === 0) {
      return "No triggers registered.";
    }
    let md = `### Configured Triggers (${triggers.length})\n`;
    for (const t of triggers) {
      const state = t.enabled !== false ? "🟢" : "⚪";
      const info = t.schedule ? `\`${t.schedule}\`` : t.type;
      md += `- ${state} **${t.id}** (${info}) — ${t.name || t.description || ""}\n`;
    }
    return md;
  } catch (err: any) {
    return `Failed to list triggers: ${err.message}`;
  }
}

async function handleSessionsCommand(): Promise<string> {
  try {
    const sessions = (await callTool("tmux.session.list" as any, {})) as any[];
    if (!Array.isArray(sessions) || sessions.length === 0) {
      return "No active `tmux` sessions on `eve` (Mac mini). Use `spawn <name> [opencode|agy|zsh]` to start one.";
    }
    let md = `### Active Tmux Sessions on \`eve\` (${sessions.length})\n`;
    for (const s of sessions) {
      md += `- **\`${s.name}\`** (${s.attached ? "attached" : "detached"}, ${s.windows} win) → Discord: \`#${LOCAL_HOST_PREFIX}-${sanitizeChannelSuffix(s.name)}\`\n`;
      try {
        const cap = (await callTool("tmux.pane.capture" as any, {
          name: s.name,
          limit: 6,
        })) as any;
        const lines = String(cap?.content || "")
          .trim()
          .split("\n")
          .filter(Boolean)
          .slice(-4);
        if (lines.length > 0) {
          md += "  ```text\n  " + lines.join("\n  ") + "\n  ```\n";
        }
      } catch {}
    }
    md += `\n*Run \`sync\` to create/update dedicated \`#eve-*\` / \`#sophia-*\` Discord channels for each session.*`;
    return md;
  } catch (err: any) {
    return `Failed to list tmux sessions: ${err.message}`;
  }
}

function getHelpMarkdown(): string {
  return [
    `### Clara Conversational & Session Control Plane`,
    `**Fast Commands (Prefix with \`.\` to avoid Discord slash-command conflicts):**`,
    `- \`.status\` (or \`status\`) — System health, triggers, and Laptop (\`sophia\`) VPN status`,
    `- \`.sync\` (or \`sync\`) — Sync active \`tmux\` sessions into dedicated \`#eve-<name>\` / \`#sophia-<name>\` Discord channels`,
    `- \`.sessions\` (or \`sessions\`) — List active \`tmux\` sessions & recent pane output`,
    `- \`.spawn <name> [cmd]\` — Launch a \`tmux\` session on \`eve\` + create its \`#eve-<name>\` Discord channel`,
    `- \`.send <name> <text>\` — Send keystrokes/prompt to a \`tmux\` session & view output`,
    `- \`.capture <name>\` — Capture last 35 lines of a \`tmux\` session`,
    `- \`.runs\` / \`.triggers\` / \`.run <id>\` — Inspect or trigger Clara workflows`,
    `- \`.code <prompt>\` — Run headless \`opencode\` task in \`~/.config/clara\``,
    `- \`.new\` / \`.reset\` — Reset chat memory for this channel`,
    ``,
    `**Inside an \`#eve-<name>\` or \`#sophia-<name>\` Discord Channel:**`,
    `- Type or record a **Voice Message** to send input directly to that \`tmux\` session`,
    `- Send \`./<command>\` (e.g. \`./clear\`, \`./plan\`) to pass a slash command cleanly to the agent`,
    `- Prefix with \`? <question>\` to ask AI about that session's terminal output`,
    `- Type \`.kill\` or \`/kill\` to terminate the session and delete the channel`,
  ].join("\n");
}

// ==========================================
// Tool-Calling Agent Loop
// ==========================================
const AGENT_TOOLS = [
  {
    type: "function",
    function: {
      name: "clara_call_tool",
      description:
        "Invoke any Clara MCP tool by its fully-qualified name. Common tools:\n" +
        "- macos.events_list: { start_date: ISO, end_date: ISO }\n" +
        "- macos.reminders_list: { completed?: boolean, list_name?: string }\n" +
        "- macos.reminders_create: { title: string, list_name?: string, due_date?: ISO, priority?: number, notes?: string }\n" +
        "- macos.reminders_complete: { identifier: string }\n" +
        "- macos.mail_list_messages: { account?: string, mailbox?: string, unread_only?: boolean, limit?: number }\n" +
        "- macos.mail_get_message: { message_id: string }\n" +
        "- macos.mail_create_draft: { to: string[], subject: string, body: string }\n" +
        "- webex.rooms.list: { max?: number }\n" +
        "- webex.messages.list: { room_id: string, max?: number }\n" +
        "- webex.message.get: { message_id: string }\n" +
        "- webex.message.reply: { room_id: string, markdown: string }\n" +
        "- webex.interactive.request: { room_id: string, title: string, description: string, options?: string[], allow_text?: boolean }\n" +
        "- discord.channels.list: {}\n" +
        "- discord.channel.create: { name: string, topic?: string, category?: string }\n" +
        "- discord.channel.delete: { channel_id?: string, name?: string }\n" +
        "- discord.message.send: { channel_id: string, content: string }\n" +
        "- discord.interactive.request: { channel_id: string, title: string, description: string, options?: string[], allow_text?: boolean }\n" +
        "- zk.note_list: { folder?: string, tags?: string[] }\n" +
        "- zk.note_get: { note: string }\n" +
        "- zk.note_create: { title: string, folder?: string, content: string }\n" +
        "- zk.note_search: { query: string }\n" +
        "- chrome_nathan.navigate: { url: string, background?: boolean, foreground?: boolean }\n" +
        "- chrome_nathan.wait_for_load: { tab_id: number }\n" +
        "- chrome_nathan.read_page: { tab_id: number }\n" +
        "- chrome_nathan.cleanup_tabs: {}\n" +
        "- tmux.session.list: {}\n" +
        "- tmux.session.create: { name: string, command: string }\n" +
        "- tmux.pane.send_keys: { name: string, text?: string, enter?: boolean, raw_key?: string }\n" +
        "- tmux.pane.capture: { name: string, limit?: number }\n" +
        "- task.pending.list: { project?: string }\n" +
        "- task.create: { description: string, project?: string, priority?: string, due?: string }\n" +
        "- fs.read_file: { path: string }\n" +
        "- fs.write_file: { path: string, content: string }\n" +
        "- fs.list_directory: { path: string }\n" +
        "- shell.run: { command: string }",
      parameters: {
        type: "object",
        properties: {
          tool_name: {
            type: "string",
            description: "The Clara MCP tool name, e.g. 'macos.reminders_list' or 'discord.channels.list'",
          },
          arguments: {
            type: "object",
            description: "Arguments object for the tool",
          },
        },
        required: ["tool_name", "arguments"],
      },
    },
  },
  {
    type: "function",
    function: {
      name: "delegate_to_opencode",
      description:
        "Delegate a multi-file coding, workflow creation, or deep sysadmin task to headless `opencode run` with full repository and MCP access.",
      parameters: {
        type: "object",
        properties: {
          prompt: {
            type: "string",
            description: "Detailed task instructions for opencode",
          },
          workdir: {
            type: "string",
            description:
              "Working directory (defaults to ~/.config/clara; use /Users/nathan/src/clara/main for Clara core Go changes)",
          },
        },
        required: ["prompt"],
      },
    },
  },
];

async function runOpenCodeHeadless(prompt: string, workdir?: string): Promise<string> {
  const dir = workdir || join(HOME, ".config/clara");
  const proc = Bun.spawn(
    [
      "/opt/homebrew/bin/opencode",
      "run",
      "--dir",
      dir,
      "--dangerously-skip-permissions",
      prompt,
    ],
    {
      cwd: dir,
      stdout: "pipe",
      stderr: "pipe",
      env: {
        ...process.env,
        PATH: `/opt/homebrew/bin:/usr/local/bin:${HOME}/.go/bin:${process.env.PATH || ""}`,
      },
    }
  );

  const timeoutMs = 150_000;
  const timer = setTimeout(() => {
    try {
      proc.kill();
    } catch {}
  }, timeoutMs);

  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  clearTimeout(timer);

  const cleanOut = stdout.replace(/\x1b\[[0-9;]*m/g, "").trim();
  const cleanErr = stderr.replace(/\x1b\[[0-9;]*m/g, "").trim();
  return cleanOut || cleanErr || "opencode completed with no output.";
}

async function runConversationalAgent(
  platform: Platform,
  targetId: string,
  userMessage: string,
  extraContext = "",
  channelName = ""
): Promise<string> {
  const historyKey = `${platform}:${targetId}`;
  const history = getRecentChatHistory(historyKey, 12);
  const nowStr = new Date().toLocaleString("en-US", {
    weekday: "long",
    year: "numeric",
    month: "long",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    timeZoneName: "short",
  });

  let channelSpecialty = "";
  if (platform === "discord") {
    const chLower = channelName.toLowerCase();
    if (chLower === "agents") {
      channelSpecialty =
        "Channel Role (#agents): Primary Clara command & orchestration hub for managing eve-* and sophia-* tmux coding sessions, triggers, runs, and delegating tasks.";
    } else if (chLower === "qa") {
      channelSpecialty =
        "Channel Role (#qa): Work & Cisco engineering research, Q&A, bug scrubs, meeting transcript action items, and MyPerformance/Timesheet rituals.";
    } else if (chLower === "home") {
      channelSpecialty =
        "Channel Role (#home): Personal life, family reminders, personal calendar, home automation/server tasks, and personal Obsidian notes.";
    } else if (chLower === "notify") {
      channelSpecialty =
        "Channel Role (#notify): System notifications, daily digests, and email/Webex triage stream.";
    }
  }

  const systemPrompt = `You are Clara, Nathan's autonomous personal AI assistant and supervisor daemon running on his 64GB M4 Mac mini (host: eve).
Current local time: ${nowStr}.
Active chat platform: ${platform} (channel/room: ${channelName ? `#${channelName} (${targetId})` : targetId}).
${channelSpecialty}
${extraContext}

Your capabilities & operating principles:
1. You have direct access to Nathan's Apple Calendar, Apple Reminders, Apple Mail (Exchange & Google), Webex spaces/messages, Discord server channels, Obsidian Vault (~/notes), Minimized Chrome browser automation (chrome_nathan.*), Tmux sessions (tmux.*), Taskwarrior (task.*), and local shell/filesystem via \`clara_call_tool\`.
   - To list tmux sessions, use \`tmux.session.list\`.
   - To capture pane output, use \`tmux.pane.capture\`.
2. When scraping Webex meeting transcripts or authenticated web pages, use \`chrome_nathan.navigate\` (which defaults to a minimized background worker window using Nathan's logged-in Chrome profile), \`chrome_nathan.wait_for_load\`, \`chrome_nathan.read_page\`, and then \`chrome_nathan.cleanup_tabs\`.
3. When Nathan asks to modify or build Clara scripts/triggers in \`~/.config/clara\` or Go code in \`/Users/nathan/src/clara/main\`, either inspect/edit directly via \`fs.*\` / \`shell.run\` or call \`delegate_to_opencode\`.
4. Keep responses concise, well-formatted in Markdown, and action-oriented. Once a tool has returned data, provide the final answer directly without calling the same tool again.`;

  const messages: any[] = [
    { role: "system", content: systemPrompt },
    ...history,
    { role: "user", content: userMessage },
  ];

  for (let turn = 0; turn < 10; turn++) {
    let resp: Response;
    try {
      resp = await fetch(`${LLM_BASE_URL}/chat/completions`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: "Bearer dummy",
        },
        body: JSON.stringify({
          model: LLM_MODEL,
          messages,
          tools: AGENT_TOOLS,
          tool_choice: "auto",
        }),
      });
    } catch (fetchErr: any) {
      // If local MLX fails to connect, gracefully fall back to cloud Copilot endpoint
      if (LLM_BASE_URL === LOCAL_MLX_URL && CLOUD_LLM_URL) {
        console.warn(`[chat_router] Local MLX failed (${fetchErr.message}), falling back to cloud LLM...`);
        resp = await fetch(`${CLOUD_LLM_URL}/chat/completions`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: "Bearer dummy",
          },
          body: JSON.stringify({
            model: CLOUD_LLM_MODEL,
            messages,
            tools: AGENT_TOOLS,
            tool_choice: "auto",
          }),
        });
      } else {
        throw fetchErr;
      }
    }

    if (!resp.ok) {
      const errText = await resp.text();
      // If local model returns HTTP error, attempt cloud fallback
      if (LLM_BASE_URL === LOCAL_MLX_URL && CLOUD_LLM_URL) {
        console.warn(`[chat_router] Local MLX returned ${resp.status}, falling back to cloud LLM...`);
        const fallbackResp = await fetch(`${CLOUD_LLM_URL}/chat/completions`, {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
            Authorization: "Bearer dummy",
          },
          body: JSON.stringify({
            model: CLOUD_LLM_MODEL,
            messages,
            tools: AGENT_TOOLS,
            tool_choice: "auto",
          }),
        });
        if (!fallbackResp.ok) {
          throw new Error(`LLM API error (${fallbackResp.status}): ${(await fallbackResp.text()).slice(0, 300)}`);
        }
        resp = fallbackResp;
      } else {
        throw new Error(`LLM API error (${resp.status}): ${errText.slice(0, 300)}`);
      }
    }

    const data = (await resp.json()) as any;
    const msg = data.choices?.[0]?.message;
    if (!msg) {
      throw new Error("Empty response from LLM");
    }

    // Parse potential inline tool calls (e.g. ```json { "name": ... } ```, <tools>..., or <clara_call_tool>...)
    // if the local model rendered them into message.content instead of message.tool_calls
    if ((!msg.tool_calls || msg.tool_calls.length === 0) && msg.content) {
      const contentStr = String(msg.content);
      const jsonBlockMatch = contentStr.match(/```(?:json)?\s*(\{[\s\S]*?"name"[\s\S]*?\})\s*```/i);
      const toolTagMatch = contentStr.match(/<tools?>\s*(\{[\s\S]*?"name"[\s\S]*?\})\s*<\/tools?>/i);
      const xmlTagMatch = contentStr.match(/<clara_call_tool>[\s\S]*?<tool_name>([\s\S]*?)<\/tool_name>[\s\S]*?(?:<arguments>([\s\S]*?)<\/arguments>)?[\s\S]*?<\/clara_call_tool>/i);

      if (xmlTagMatch) {
        const toolName = xmlTagMatch[1].trim();
        let rawArgs = (xmlTagMatch[2] || "{}").trim();
        msg.tool_calls = [
          {
            id: `call_${Date.now()}`,
            type: "function",
            function: {
              name: "clara_call_tool",
              arguments: JSON.stringify({
                tool_name: toolName,
                arguments: rawArgs && rawArgs !== "{}" ? JSON.parse(rawArgs) : {},
              }),
            },
          },
        ];
      } else {
        const matchedJson = jsonBlockMatch?.[1] || toolTagMatch?.[1];
        if (matchedJson) {
          try {
            const parsedCall = JSON.parse(matchedJson);
            if (parsedCall.name) {
              msg.tool_calls = [
                {
                  id: `call_${Date.now()}`,
                  type: "function",
                  function: {
                    name: parsedCall.name,
                    arguments: typeof parsedCall.arguments === "string" ? parsedCall.arguments : JSON.stringify(parsedCall.arguments || {}),
                  },
                },
              ];
            }
          } catch {}
        }
      }
    }

    messages.push(msg);

    if (!msg.tool_calls || msg.tool_calls.length === 0) {
      const finalContent = String(msg.content || "").trim();
      appendChatHistory(historyKey, "user", userMessage);
      appendChatHistory(historyKey, "assistant", finalContent);
      return finalContent || "Done.";
    }

    for (const tc of msg.tool_calls) {
      const fnName = tc.function?.name;
      let fnArgs: any = {};
      try {
        fnArgs = JSON.parse(tc.function?.arguments || "{}");
      } catch {}

      let toolResultStr = "";
      try {
        if (fnName === "clara_call_tool") {
          const targetTool = String(fnArgs.tool_name || "");
          const targetArgs = fnArgs.arguments || {};
          console.log(`[chat_router] Calling Clara tool: ${targetTool}`, JSON.stringify(targetArgs));
          const res = await callTool(targetTool as any, targetArgs);
          toolResultStr = typeof res === "string" ? res : JSON.stringify(res);
        } else if (fnName.includes(".")) {
          // Model called the tool name directly (e.g. "tmux.session.list")
          console.log(`[chat_router] Calling Clara tool directly: ${fnName}`, JSON.stringify(fnArgs));
          const res = await callTool(fnName as any, fnArgs);
          toolResultStr = typeof res === "string" ? res : JSON.stringify(res);
        } else if (fnName === "delegate_to_opencode") {
          console.log(`[chat_router] Delegating to opencode: ${fnArgs.prompt}`);
          toolResultStr = await runOpenCodeHeadless(fnArgs.prompt, fnArgs.workdir);
        } else {
          toolResultStr = JSON.stringify({ error: `Unknown function: ${fnName}` });
        }
      } catch (err: any) {
        toolResultStr = JSON.stringify({ error: err.message });
      }

      if (toolResultStr.length > 16000) {
        toolResultStr = toolResultStr.slice(0, 16000) + "\n...[truncated]";
      }

      messages.push({
        role: "tool",
        tool_call_id: tc.id,
        content: toolResultStr,
      });
    }
  }

  return "Reached maximum tool-execution turns. Check `runs` or ask me to continue.";
}

// ==========================================
// Main Entrypoint
// ==========================================
async function main() {
  const isDryRun = Boolean(values["dry-run"]);
  let platform: Platform = (values.platform as Platform) || "webex";
  let targetId = values.room || DEFAULT_WEBEX_ROOM_ID;
  let text = values.prompt || "";
  let messageId = `cli-${Date.now()}`;
  let sender = "nathan@local";
  let channelName = "";
  let channelTopic = "";
  let attachmentUrl = "";
  let attachmentType = "";

  if (!text) {
    const ev = await readEvent<any>();
    const d = ev?.data || {};
    messageId = d.message_id || ev.id || messageId;

    if (ev?.source === "discord" || d.channel_id) {
      platform = "discord";
      targetId = d.channel_id;
      sender = d.user || "discord-user";
      text = String(d.content || "").trim();
      channelName = d.channel_name || "";
      channelTopic = d.channel_topic || "";
      attachmentUrl = d.attachment_url || "";
      attachmentType = d.attachment_type || "";
    } else {
      platform = "webex";
      targetId = d.room_id || targetId;
      sender = d.person_email || "";
      text = String(d.text || "").trim();
    }
  }

  // Never respond to bot's own messages
  if (
    sender.toLowerCase().endsWith("@webex.bot") ||
    sender.toLowerCase().endsWith("@sparkbot.io")
  ) {
    return;
  }

  // Handle Voice Note / Audio Attachment Transcription
  if (
    attachmentUrl &&
    (attachmentType.startsWith("audio/") ||
      attachmentType.includes("ogg") ||
      /\.(ogg|mp3|m4a|wav|aac|opus)(\?|$)/i.test(attachmentUrl))
  ) {
    if (isMessageProcessed(messageId)) return;
    markMessageProcessed(messageId, targetId, sender, `[voice:${attachmentUrl}]`);
    try {
      const transcript = await transcribeVoiceAttachment(attachmentUrl);
      if (!transcript) {
        await sendReply(platform, targetId, "🎙️ Could not detect speech in audio note.", isDryRun);
        return;
      }
      await sendReply(platform, targetId, `🎙️ *Transcribed:* "${transcript}"`, isDryRun);
      text = text ? `${text}\n${transcript}` : transcript;
    } catch (err: any) {
      await sendReply(
        platform,
        targetId,
        `⚠️ Voice transcription failed: \`${err.message}\``,
        isDryRun
      );
      return;
    }
  } else {
    if (!text) return;
    if (isMessageProcessed(messageId)) return;
    markMessageProcessed(messageId, targetId, sender, text);
  }

  console.log(
    `[chat_router] Processing [${platform}:${channelName || targetId}] (id=${messageId}, sender=${sender}, run=${runID()}, trigger=${triggerID()}): ${text}`
  );

  const normalized = text.trim().replace(/^@?(?:clara|oswin)[,:]?\s+/i, "").trim();
  const lower = normalized.toLowerCase();

  try {
    // ========================================================
    // 1. Check if this is a Bound Per-Session Discord Channel!
    // ========================================================
    const boundSession =
      platform === "discord" ? resolveBoundTmuxSession(channelName, channelTopic) : null;

    if (boundSession) {
      const { host, sessionName } = boundSession;
      const killTool = host === "sophia" ? "laptop.tmux.session.kill" : "tmux.session.kill";
      const captureTool = host === "sophia" ? "laptop.tmux.pane.capture" : "tmux.pane.capture";
      const sendKeysTool = host === "sophia" ? "laptop.tmux.pane.send_keys" : "tmux.pane.send_keys";

      if (lower === "/kill" || lower === "kill" || lower === ".kill") {
        await callTool(killTool as any, { name: sessionName });
        await sendReply(
          platform,
          targetId,
          `🛑 Killed \`tmux\` session **\`${host}:${sessionName}\`**. Deleting channel...`,
          isDryRun
        );
        await callTool("discord.channel.delete" as any, { channel_id: targetId });
        return;
      }

      if (lower === "capture" || lower === "/capture" || lower === ".capture") {
        const cap = (await callTool(captureTool as any, {
          name: sessionName,
          limit: 40,
        })) as any;
        await sendReply(
          platform,
          targetId,
          `\`\`\`text\n${String(cap?.content || "").trim().slice(-1850)}\n\`\`\``,
          isDryRun
        );
        return;
      }

      if (normalized.startsWith("?")) {
        const question = normalized.slice(1).trim() || "Summarize what this session is doing.";
        const cap = (await callTool(captureTool as any, {
          name: sessionName,
          limit: 60,
        })) as any;
        const paneContext = `This Discord channel (#${channelName}) is bound to ${host} tmux session '${sessionName}'.\nLatest terminal pane output:\n\`\`\`\n${String(cap?.content || "").slice(-4000)}\n\`\`\``;
        const answer = await runConversationalAgent(platform, targetId, question, paneContext);
        await sendReply(platform, targetId, answer, isDryRun);
        return;
      }

      // Handle slash-command forwarding:
      // If user typed `./clear`, strip the leading '.' so the session receives `/clear`
      let sessionInput = normalized;
      if (sessionInput.startsWith("./")) {
        sessionInput = sessionInput.slice(1);
      }

      // Record prompt hash in agy_prompt_mirror so agy_prompt_hook doesn't echo it back to Discord
      try {
        let h = 0;
        for (let i = 0; i < sessionInput.length; i++) {
          h = (Math.imul(31, h) + sessionInput.charCodeAt(i)) | 0;
        }
        const promptHash = `${sessionName}:${h.toString(36)}`;
        stateDb
          .query(
            "INSERT OR REPLACE INTO agy_prompt_mirror (prompt_hash, session_name, created_at) VALUES (?, ?, ?)"
          )
          .run(promptHash, sessionName, Date.now());
      } catch {}

      // Add instant visibility: reaction + typing indicator
      if (platform === "discord" && messageId && !messageId.startsWith("cli-")) {
        try {
          await callTool("discord.message.react" as any, {
            channel_id: targetId,
            message_id: messageId,
            emoji: "⏳",
          });
          await callTool("discord.channel.typing" as any, {
            channel_id: targetId,
          });
          stateDb
            .query(
              "INSERT OR REPLACE INTO pending_reactions (channel_id, message_id, session_name, created_at) VALUES (?, ?, ?, ?)"
            )
            .run(targetId, messageId, sessionName, Date.now());
        } catch {}
      }

      // Default inside #eve-<name> or #sophia-<name>: send directly to tmux session!
      await callTool(sendKeysTool as any, {
        name: sessionName,
        text: sessionInput,
        enter: true,
      });

      // Check if this tmux session is running `agy` (which pushes its own clean Markdown
      // response via the global Stop hook in ~/.gemini/config/hooks.json).
      let isAgySession = false;
      if (host === "eve") {
        try {
          const proc = Bun.spawn(
            ["/opt/homebrew/bin/tmux", "list-panes", "-t", sessionName, "-F", "#{pane_current_command}"],
            { stdout: "pipe", stderr: "pipe" }
          );
          const cmds = (await new Response(proc.stdout).text()).toLowerCase();
          if (cmds.includes("agy") || cmds.includes("antigravity")) {
            isAgySession = true;
          }
        } catch {}
      }

      if (isAgySession) {
        // agy's native Stop hook (`agy_stop_hook.ts`) will automatically post the clean
        // Markdown response when the turn finishes — no noisy terminal scrape needed!
        return;
      }

      // For non-agy sessions (opencode, zsh, python, etc.): poll the tmux pane until
      // output stabilizes and extract only the newly added output.
      const beforeCap = (await callTool(captureTool as any, {
        name: sessionName,
        limit: 40,
      })) as any;
      const beforeLines = String(beforeCap?.content || "")
        .trim()
        .split("\n")
        .map((l) => l.trim());

      const busyRegex = /(esc to cancel|Running command|ctrl\+o to expand|Thinking\.\.\.|Working\.\.\.|Generating|[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⣟])/i;
      let lastPane = "";
      let stableCount = 0;
      const startWait = Date.now();
      const maxWaitMs = 150_000;

      await new Promise((r) => setTimeout(r, 2500));
      while (Date.now() - startWait < maxWaitMs) {
        const cap = (await callTool(captureTool as any, {
          name: sessionName,
          limit: 40,
        })) as any;
        const currentPane = String(cap?.content || "").trim();
        const tailLines = currentPane.split("\n").slice(-8).join("\n");
        const isBusy = busyRegex.test(tailLines);

        if (!isBusy && currentPane === lastPane && currentPane.length > 0) {
          stableCount++;
          if (stableCount >= 2) {
            break;
          }
        } else {
          stableCount = 0;
        }
        lastPane = currentPane;
        await new Promise((r) => setTimeout(r, 2500));
      }

      // Filter out old scrollback lines that were already on screen before the prompt
      const afterLines = lastPane.split("\n");
      let startIdx = 0;
      const anchor = beforeLines.filter(Boolean).slice(-2)[0];
      if (anchor) {
        const foundIdx = afterLines.findLastIndex((l) => l.trim() === anchor);
        if (foundIdx !== -1 && foundIdx + 1 < afterLines.length) {
          startIdx = foundIdx + 1;
        }
      }
      const deltaOut = afterLines.slice(startIdx).join("\n").trim().slice(-1750);
      await sendReply(
        platform,
        targetId,
        `\`\`\`text\n${deltaOut || lastPane.slice(-1000) || "(no output)"}\n\`\`\``,
        isDryRun
      );
      return;
    }

    // ========================================================
    // 2. Global Fast-Path Commands (Webex & Discord)
    // Supports commands starting with `.` (preferred), `/`, or no prefix.
    // ========================================================
    const strippedLower = lower.replace(/^[./]/, "");

    if (strippedLower === "help" || strippedLower === "?") {
      await sendReply(platform, targetId, getHelpMarkdown(), isDryRun);
      return;
    }

    if (strippedLower === "status") {
      const out = await handleStatusCommand();
      await sendReply(platform, targetId, out, isDryRun);
      return;
    }

    if (strippedLower === "sync") {
      const out = await syncDiscordSessionChannels();
      await sendReply(platform, targetId, out, isDryRun);
      return;
    }

    if (strippedLower === "runs") {
      const out = await handleRunsCommand();
      await sendReply(platform, targetId, out, isDryRun);
      return;
    }

    if (strippedLower === "triggers") {
      const out = await handleTriggersCommand();
      await sendReply(platform, targetId, out, isDryRun);
      return;
    }

    if (strippedLower === "sessions") {
      const out = await handleSessionsCommand();
      await sendReply(platform, targetId, out, isDryRun);
      return;
    }

    if (strippedLower === "new" || strippedLower === "reset") {
      clearChatHistory(`${platform}:${targetId}`);
      await sendReply(platform, targetId, "Conversational context cleared.", isDryRun);
      return;
    }

    if (strippedLower.startsWith("run ")) {
      const targetTrigger = normalized.replace(/^[./]?run\s+/i, "").trim();
      await sendReply(platform, targetId, `Starting trigger \`${targetTrigger}\`...`, isDryRun);
      const proc = Bun.spawn(["clara", "trigger", "run", targetTrigger], {
        stdout: "pipe",
        stderr: "pipe",
      });
      const [stdout, stderr] = await Promise.all([
        new Response(proc.stdout).text(),
        new Response(proc.stderr).text(),
      ]);
      const code = await proc.exited;
      const summary =
        code === 0
          ? `✅ Trigger \`${targetTrigger}\` completed.\n\`\`\`text\n${stdout.trim().slice(-800)}\n\`\`\``
          : `❌ Trigger \`${targetTrigger}\` failed (exit ${code}):\n\`\`\`text\n${(stderr || stdout).trim().slice(-800)}\n\`\`\``;
      await sendReply(platform, targetId, summary, isDryRun);
      return;
    }

    if (strippedLower.startsWith("spawn ")) {
      const parts = normalized.replace(/^[./]?spawn\s+/i, "").trim().split(/\s+/);
      let rawName = parts[0];
      let targetHost: "eve" | "sophia" = "eve";
      if (rawName.toLowerCase().startsWith("sophia:") || rawName.toLowerCase().startsWith("laptop:")) {
        targetHost = "sophia";
        rawName = rawName.split(":")[1] || "dev";
      } else if (rawName.toLowerCase().startsWith("eve:")) {
        rawName = rawName.split(":")[1] || "dev";
      }
      const cmd =
        parts.slice(1).join(" ") ||
        (targetHost === "sophia" ? "/opt/homebrew/bin/opencode" : `cd ${HOME}/.config/clara && /opt/homebrew/bin/opencode`);
      const createTool = targetHost === "sophia" ? "laptop.tmux.session.create" : "tmux.session.create";
      const hostPrefix = targetHost === "sophia" ? LAPTOP_HOST_PREFIX : LOCAL_HOST_PREFIX;
      await callTool(createTool as any, { name: rawName, command: cmd });
      const chName = `${hostPrefix}-${sanitizeChannelSuffix(rawName)}`;
      let discordNote = "";
      try {
        const ch = (await callTool("discord.channel.create" as any, {
          name: chName,
          topic: `tmux:${targetHost}:${rawName} — Type directly to send keys to ${targetHost} tmux session '${rawName}', or prefix with '? ' to ask AI`,
          category: "Active Agent Sessions",
        })) as any;
        if (ch?.id) {
          discordNote = `\nCreated Discord channel **\`#${chName}\`** (<#${ch.id}>).`;
        }
      } catch {}
      await sendReply(
        platform,
        targetId,
        `Started detached \`tmux\` session **\`${rawName}\`** on **\`${targetHost}\`** running \`${cmd}\`.${discordNote}`,
        isDryRun
      );
      return;
    }

    if (strippedLower.startsWith("send ")) {
      const rest = normalized.replace(/^[./]?send\s+/i, "").trim();
      const firstSpace = rest.indexOf(" ");
      if (firstSpace === -1) {
        await sendReply(platform, targetId, "Usage: `.send <session_name> <text>`", isDryRun);
        return;
      }
      const sessionName = rest.slice(0, firstSpace).trim();
      const promptText = rest.slice(firstSpace + 1).trim();
      await callTool("tmux.pane.send_keys" as any, {
        name: sessionName,
        text: promptText,
        enter: true,
      });
      await new Promise((r) => setTimeout(r, 2500));
      const cap = (await callTool("tmux.pane.capture" as any, {
        name: sessionName,
        limit: 25,
      })) as any;
      await sendReply(
        platform,
        targetId,
        `Sent to **\`${sessionName}\`**. Latest pane output:\n\`\`\`text\n${String(cap?.content || "").trim().slice(-1500)}\n\`\`\``,
        isDryRun
      );
      return;
    }

    if (strippedLower.startsWith("capture ")) {
      const sessionName = normalized.replace(/^[./]?capture\s+/i, "").trim();
      const cap = (await callTool("tmux.pane.capture" as any, {
        name: sessionName,
        limit: 35,
      })) as any;
      await sendReply(
        platform,
        targetId,
        `**\`${sessionName}\`** pane output:\n\`\`\`text\n${String(cap?.content || "").trim().slice(-1800)}\n\`\`\``,
        isDryRun
      );
      return;
    }

    if (strippedLower.startsWith("code ")) {
      const codePrompt = normalized.replace(/^[./]?code\s+/i, "").trim();
      await sendReply(platform, targetId, `Running \`opencode\` on: *${codePrompt}*...`, isDryRun);
      const out = await runOpenCodeHeadless(codePrompt);
      await sendReply(platform, targetId, out.slice(0, 3500), isDryRun);
      return;
    }

    // ========================================================
    // 3. Default: Conversational Tool-Using Agent Loop
    // ========================================================
    const answer = await runConversationalAgent(platform, targetId, normalized, "", channelName);
    await sendReply(platform, targetId, answer, isDryRun);
  } catch (err: any) {
    console.error(`[chat_router] Error:`, err);
    await sendReply(
      platform,
      targetId,
      `⚠️ **Clara Chat Router Error:** \`${err.message}\``,
      isDryRun
    );
  }
}

await main();
