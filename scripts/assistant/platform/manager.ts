/**
 * scripts/assistant/platform/manager.ts
 *
 * Central Platform Manager for Clara.
 * Handles reading/writing the primary platform, resolving active adapters,
 * and performing seamless zero-orphan switching between Discord and Webex.
 */

import { homedir } from "node:os";
import { join } from "node:path";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { parse as parseYaml, stringify as stringifyYaml } from "yaml";
import { callTool } from "../../clara.ts";
import type { BoundSessionInfo, ChatPlatformAdapter, ManagedChannel, PlatformType } from "./types.ts";
import { DiscordAdapter, resolveDiscordBoundTmuxSession, sanitizeChannelSuffix } from "./discord.ts";
import { WebexAdapter, resolveWebexBoundTmuxSession } from "./webex.ts";

const CONFIG_PATH = join(homedir(), ".config/clara/config.yaml");

const discordAdapter = new DiscordAdapter();
const webexAdapter = new WebexAdapter();

export function getPlatformAdapter(platform: PlatformType): ChatPlatformAdapter {
  return platform === "webex" ? webexAdapter : discordAdapter;
}

/**
 * Read primary_agent_platform from ~/.config/clara/config.yaml (defaults to "discord").
 */
export function getPrimaryPlatform(): PlatformType {
  try {
    if (existsSync(CONFIG_PATH)) {
      const doc = parseYaml(readFileSync(CONFIG_PATH, "utf8")) || {};
      const val = String(doc.primary_agent_platform || "").toLowerCase();
      if (val === "webex" || val === "discord") {
        return val as PlatformType;
      }
    }
  } catch (err: any) {
    console.warn(`[platform/manager] Failed to read primary platform from config: ${err.message}`);
  }
  return "discord";
}

/**
 * Update primary_agent_platform in ~/.config/clara/config.yaml.
 */
export function setPrimaryPlatform(platform: PlatformType): void {
  try {
    if (existsSync(CONFIG_PATH)) {
      const content = readFileSync(CONFIG_PATH, "utf8");
      const doc = parseYaml(content) || {};
      doc.primary_agent_platform = platform;
      writeFileSync(CONFIG_PATH, stringifyYaml(doc), "utf8");
    }
  } catch (err: any) {
    console.warn(`[platform/manager] Failed to update config.yaml: ${err.message}`);
  }
}

/**
 * Return the adapter currently designated as primary for agent development channels.
 */
export function getPrimaryAdapter(): ChatPlatformAdapter {
  return getPlatformAdapter(getPrimaryPlatform());
}

/**
 * Detect bound tmux session from either Discord or Webex channel/room metadata.
 */
export function resolveAnyBoundTmuxSession(
  platform: PlatformType,
  nameOrTitle: string,
  topic: string = ""
): BoundSessionInfo | null {
  if (platform === "discord") {
    return resolveDiscordBoundTmuxSession(nameOrTitle, topic);
  } else {
    return resolveWebexBoundTmuxSession(nameOrTitle);
  }
}

/**
 * Sync active tmux sessions into the primary platform's channels/spaces.
 * If targetPlatform is specified, it sets that platform as primary and cleans up the other platform!
 */
export async function syncSessionChannels(
  targetPlatform?: PlatformType,
  backfillFn?: (channelId: string, host: "eve" | "sophia", sessionName: string) => Promise<void>
): Promise<string> {
  const currentPrimary = getPrimaryPlatform();
  const nextPrimary = targetPlatform || currentPrimary;
  const isSwitching = targetPlatform !== undefined && targetPlatform !== currentPrimary;

  if (targetPlatform) {
    setPrimaryPlatform(nextPrimary);
  }

  const primaryAdapter = getPlatformAdapter(nextPrimary);
  const otherAdapter = getPlatformAdapter(nextPrimary === "webex" ? "discord" : "webex");

  // If switching platforms, clean up all managed spaces on the inactive platform first
  let cleanedOther: string[] = [];
  if (isSwitching) {
    console.log(`[platform/manager] Switching primary agent platform: ${currentPrimary} -> ${nextPrimary}. Cleaning up ${otherAdapter.platform}...`);
    cleanedOther = await otherAdapter.cleanupAllManagedChannels();
  }

  // Discover all running tmux sessions and panes across eve and sophia
  // When a session contains multiple agent panes (agy, antigravity, opencode),
  // each agent pane is surfaced as an independent target: e.g. "clara:0", "clara:1".
  const activeSessions: { host: "eve" | "sophia"; sessionName: string }[] = [];

  // Query eve sessions and panes directly via tmux CLI
  try {
    const proc = Bun.spawn(
      [
        "/opt/homebrew/bin/tmux",
        "list-panes",
        "-a",
        "-F",
        "#{session_name}|#{window_index}|#{pane_index}|#{pane_id}|#{pane_current_command}",
      ],
      { stdout: "pipe", stderr: "pipe" }
    );
    const text = (await new Response(proc.stdout).text()).trim();
    if (text) {
      const agentPanesBySession = new Map<string, string[]>();
      const allSessions = new Set<string>();

      for (const line of text.split("\n")) {
        if (!line.trim()) continue;
        const [sName, _winIdx, paneIdx, _paneId, cmd] = line.split("|");
        if (!sName) continue;
        allSessions.add(sName);

        const c = (cmd || "").toLowerCase();
        const isAgent =
          c.includes("agy") || c.includes("antigravity") || c.includes("opencode");
        if (isAgent) {
          if (!agentPanesBySession.has(sName)) agentPanesBySession.set(sName, []);
          agentPanesBySession.get(sName)!.push(paneIdx);
        }
      }

      for (const sName of allSessions) {
        const agentPanes = agentPanesBySession.get(sName) || [];
        if (agentPanes.length > 1) {
          // Multiple agents running in panes: surface each pane as session:paneIndex
          for (const pIdx of agentPanes) {
            activeSessions.push({ host: "eve", sessionName: `${sName}:${pIdx}` });
          }
        } else {
          // Single agent or standard session
          activeSessions.push({ host: "eve", sessionName: sName });
        }
      }
    }
  } catch (err: any) {
    console.warn(`[platform/manager] Failed to list eve tmux panes directly: ${err.message}`);
    const eveSessions = ((await callTool("tmux.session.list" as any, {})) || []) as any[];
    for (const s of eveSessions) {
      activeSessions.push({ host: "eve", sessionName: s.name });
    }
  }

  // Sophia (satellite) sessions
  try {
    const res = await callTool("laptop.tmux.session.list" as any, {});
    if (Array.isArray(res)) {
      for (const s of res) {
        activeSessions.push({ host: "sophia", sessionName: s.name });
      }
    }
  } catch {}

  const existingChannels = await primaryAdapter.listManagedChannels();
  const activeKeys = new Set(activeSessions.map((s) => `${s.host}:${s.sessionName.toLowerCase()}`));

  const created: string[] = [];
  const existing: string[] = [];
  const removed: string[] = [];

  // 1. Ensure channels exist on primary platform for active sessions
  for (const s of activeSessions) {
    const found = existingChannels.find(
      (c) =>
        c.boundSession?.host === s.host &&
        c.boundSession?.sessionName.toLowerCase() === s.sessionName.toLowerCase()
    );

    let chId = "";
    if (found) {
      chId = found.id;
      existing.push(`${found.name} (\`${s.host}:${s.sessionName}\`)`);
    } else {
      const ch = await primaryAdapter.ensureSessionChannel(s.host, s.sessionName);
      chId = ch.id;
      created.push(`${ch.name} (\`${s.host}:${s.sessionName}\`)`);
    }

    if (chId && backfillFn) {
      await backfillFn(chId, s.host, s.sessionName);
    }
  }

  // 2. Remove stale channels on primary platform whose tmux session no longer exists
  for (const ch of existingChannels) {
    if (!ch.boundSession) continue;
    const key = `${ch.boundSession.host}:${ch.boundSession.sessionName.toLowerCase()}`;
    if (!activeKeys.has(key)) {
      try {
        await primaryAdapter.deleteChannel(ch.id);
        removed.push(ch.name);
      } catch (err: any) {
        console.warn(`[platform/manager] Failed to remove stale channel ${ch.name}: ${err.message}`);
      }
    }
  }

  const lines = [
    `### ${primaryAdapter.platform === "webex" ? "Webex" : "Discord"} ↔ Tmux Session Multiplexer Synced`,
    `- **Primary Platform:** **${primaryAdapter.platform.toUpperCase()}**`,
    `- **Active Session Channels (${activeSessions.length}):** ${[...created, ...existing].join(", ") || "None"}`,
  ];

  if (created.length > 0) lines.push(`- **Created:** ${created.join(", ")}`);
  if (removed.length > 0) lines.push(`- **Cleaned Up Ended Sessions:** ${removed.join(", ")}`);
  if (cleanedOther.length > 0) {
    lines.push(`- **Purged From Inactive Platform (${otherAdapter.platform}):** ${cleanedOther.join(", ")}`);
  }

  lines.push(
    `\n*Tip: Switch platforms anytime with \`.platform webex\` or \`.platform discord\`.*`
  );

  return lines.filter(Boolean).join("\n");
}
