/**
 * scripts/assistant/platform/discord.ts
 *
 * Discord implementation of ChatPlatformAdapter.
 * Manages `#eve-<name>` and `#sophia-<name>` channels under "Active Agent Sessions".
 */

import { callTool } from "../../clara.ts";
import type {
  BoundSessionInfo,
  ChatPlatformAdapter,
  ManagedAgentChannel,
  ManagedChannel,
  PlatformType,
} from "./types.ts";
import { agentTopic, parseAgentTopic } from "../agent/binding.ts";

export const LOCAL_HOST_PREFIX = "eve";
export const LAPTOP_HOST_PREFIX = "sophia";
export const DISCORD_CATEGORY = "Active Agent Sessions";

export function sanitizeChannelSuffix(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9-_]/g, "-")
    .replace(/-+/g, "-")
    .replace(/^-|-$/g, "");
}

export function resolveDiscordBoundTmuxSession(
  channelName: string,
  channelTopic: string
): BoundSessionInfo | null {
  if (parseAgentTopic(channelTopic)) return null;
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

export class DiscordAdapter implements ChatPlatformAdapter {
  readonly platform: PlatformType = "discord";

  async listManagedChannels(): Promise<ManagedChannel[]> {
    const rawChannels = ((await callTool("discord.channels.list" as any, {})) || []) as any[];
    const result: ManagedChannel[] = [];
    for (const c of rawChannels) {
      if (c.type !== "text") continue;
      const bound = resolveDiscordBoundTmuxSession(c.name, c.topic || "");
      if (bound) {
        result.push({
          id: String(c.id),
          name: String(c.name),
          topic: c.topic,
          boundSession: bound,
        });
      }
    }
    return result;
  }

  async findChannelForSession(
    host: "eve" | "sophia",
    sessionName: string
  ): Promise<ManagedChannel | null> {
    const channels = await this.listManagedChannels();
    const expectedSuffix = sanitizeChannelSuffix(sessionName);
    const expectedName = `${host}-${expectedSuffix}`;
    return (
      channels.find(
        (c) =>
          c.boundSession?.host === host &&
          (c.boundSession?.sessionName.toLowerCase() === sessionName.toLowerCase() ||
            c.name.toLowerCase() === expectedName)
      ) || null
    );
  }

  async ensureSessionChannel(
    host: "eve" | "sophia",
    sessionName: string
  ): Promise<ManagedChannel> {
    const existing = await this.findChannelForSession(host, sessionName);
    if (existing) return existing;

    const chName = `${host}-${sanitizeChannelSuffix(sessionName)}`;
    const topic = `tmux:${host}:${sessionName} — Type directly to send keys to ${host} tmux session '${sessionName}', or prefix with '? ' to ask AI`;

    const created = (await callTool("discord.channel.create" as any, {
      name: chName,
      topic,
      category: DISCORD_CATEGORY,
    })) as any;

    return {
      id: String(created.id),
      name: chName,
      topic,
      boundSession: { host, sessionName },
    };
  }

  async listAgentChannels(): Promise<ManagedAgentChannel[]> {
    const raw = ((await callTool("discord.channels.list" as any, {})) || []) as any[];
    const out: ManagedAgentChannel[] = [];
    for (const c of raw) {
      if (c.type !== "text") continue;
      const b = parseAgentTopic(c.topic || "");
      if (b) out.push({ id: String(c.id), name: String(c.name), ...b });
    }
    return out;
  }

  async ensureAgentChannel(
    host: string,
    session: string,
    name: string
  ): Promise<ManagedAgentChannel> {
    const existing = (await this.listAgentChannels()).find(
      (c) => c.host === host && c.session === session
    );
    if (existing) return existing;
    const chName = `${host}-${sanitizeChannelSuffix(name)}`;
    const created = (await callTool("discord.channel.create" as any, {
      name: chName,
      topic: agentTopic(host, session),
      category: DISCORD_CATEGORY,
    })) as any;
    return { id: String(created.id), name: chName, host, session };
  }

  async deleteChannel(channelId: string): Promise<void> {
    await callTool("discord.channel.delete" as any, { channel_id: channelId });
  }

  async cleanupAllManagedChannels(): Promise<string[]> {
    const channels = await this.listManagedChannels();
    const deleted: string[] = [];
    for (const ch of channels) {
      try {
        await this.deleteChannel(ch.id);
        deleted.push(`#${ch.name}`);
      } catch (err: any) {
        console.warn(`[discord] Failed to delete channel #${ch.name} (${ch.id}): ${err.message}`);
      }
    }
    return deleted;
  }

  async sendMessage(channelId: string, content: string): Promise<string> {
    const maxLen = 1900;
    let remaining = content;
    let lastMsgId = "";
    while (remaining.length > 0) {
      let idx = remaining.length <= maxLen ? remaining.length : remaining.lastIndexOf("\n", maxLen);
      if (idx < 400) idx = Math.min(maxLen, remaining.length);
      const chunk = remaining.slice(0, idx);
      remaining = remaining.slice(idx).trimStart();
      const res = (await callTool("discord.message.send" as any, {
        channel_id: channelId,
        content: chunk,
      })) as any;
      if (res?.id) lastMsgId = String(res.id);
    }
    return lastMsgId;
  }

  async addReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    if (!messageId || messageId.startsWith("cli-")) return;
    try {
      await callTool("discord.message.react" as any, {
        channel_id: channelId,
        message_id: messageId,
        emoji,
      });
    } catch {}
  }

  async removeReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    if (!messageId || messageId.startsWith("cli-")) return;
    try {
      await callTool("discord.message.remove_reaction" as any, {
        channel_id: channelId,
        message_id: messageId,
        emoji,
      });
    } catch {}
  }

  async sendTyping(channelId: string): Promise<void> {
    try {
      await callTool("discord.channel.typing" as any, {
        channel_id: channelId,
      });
    } catch {}
  }
}
