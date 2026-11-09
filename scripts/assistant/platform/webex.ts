/**
 * scripts/assistant/platform/webex.ts
 *
 * Webex implementation of ChatPlatformAdapter.
 * Manages `[Clara:eve] <session>` and `[Clara:sophia] <session>` spaces.
 */

import { callTool } from "../../clara.ts";
import type {
  BoundSessionInfo,
  ChatPlatformAdapter,
  ManagedAgentChannel,
  ManagedChannel,
  PlatformType,
} from "./types.ts";
import { parseAgentWebexTitle, webexAgentTitle } from "../agent/binding.ts";

export const WEBEX_SPACE_PREFIX_EVE = "[Clara:eve]";
export const WEBEX_SPACE_PREFIX_SOPHIA = "[Clara:sophia]";

export function resolveWebexBoundTmuxSession(title: string): BoundSessionInfo | null {
  const trimmed = String(title || "").trim();
  if (parseAgentWebexTitle(trimmed)) return null;
  const eveMatch = trimmed.match(/^\[Clara:eve\]\s*(.+)$/i);
  if (eveMatch?.[1]) {
    return { host: "eve", sessionName: eveMatch[1].trim() };
  }
  const sophiaMatch = trimmed.match(/^\[Clara:sophia\]\s*(.+)$/i);
  if (sophiaMatch?.[1]) {
    return { host: "sophia", sessionName: sophiaMatch[1].trim() };
  }
  return null;
}

export class WebexAdapter implements ChatPlatformAdapter {
  readonly platform: PlatformType = "webex";

  async listManagedChannels(): Promise<ManagedChannel[]> {
    const rawRooms = ((await callTool("webex.rooms.list" as any, { max: 100 })) || []) as any[];
    const result: ManagedChannel[] = [];
    for (const r of rawRooms) {
      if (r.type !== "group") continue;
      const bound = resolveWebexBoundTmuxSession(r.title);
      if (bound) {
        result.push({
          id: String(r.id),
          name: String(r.title),
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
    return (
      channels.find(
        (c) =>
          c.boundSession?.host === host &&
          c.boundSession?.sessionName.toLowerCase() === sessionName.toLowerCase()
      ) || null
    );
  }

  async ensureSessionChannel(
    host: "eve" | "sophia",
    sessionName: string
  ): Promise<ManagedChannel> {
    const existing = await this.findChannelForSession(host, sessionName);
    if (existing) return existing;

    const prefix = host === "sophia" ? WEBEX_SPACE_PREFIX_SOPHIA : WEBEX_SPACE_PREFIX_EVE;
    const title = `${prefix} ${sessionName}`;

    const created = (await callTool("webex.room.create" as any, {
      title,
      invite_bot: true,
    })) as any;

    return {
      id: String(created.id),
      name: title,
      boundSession: { host, sessionName },
    };
  }

  async listAgentChannels(): Promise<ManagedAgentChannel[]> {
    const rooms = ((await callTool("webex.rooms.list" as any, { max: 100 })) || []) as any[];
    const out: ManagedAgentChannel[] = [];
    for (const r of rooms) {
      if (r.type !== "group") continue;
      const b = parseAgentWebexTitle(r.title);
      if (b) out.push({ id: String(r.id), name: String(r.title), ...b });
    }
    return out;
  }

  async ensureAgentChannel(
    host: string,
    session: string,
    name: string,
    _dir?: string
  ): Promise<ManagedAgentChannel> {
    const existing = (await this.listAgentChannels()).find(
      (c) => c.host === host && c.session === session
    );
    if (existing) return existing;
    const title = webexAgentTitle(host, name, session);
    const created = (await callTool("webex.room.create" as any, {
      title,
      invite_bot: true,
    })) as any;
    return { id: String(created.id), name: title, host, session };
  }

  async deleteChannel(channelId: string): Promise<void> {
    await callTool("webex.room.delete" as any, { room_id: channelId });
  }

  async cleanupAllManagedChannels(): Promise<string[]> {
    const channels = await this.listManagedChannels();
    const deleted: string[] = [];
    for (const ch of channels) {
      try {
        await this.deleteChannel(ch.id);
        deleted.push(`"${ch.name}"`);
      } catch (err: any) {
        console.warn(`[webex] Failed to delete space "${ch.name}" (${ch.id}): ${err.message}`);
      }
    }
    return deleted;
  }

  async sendMessage(channelId: string, content: string): Promise<string> {
    const maxLen = 4000;
    let remaining = content;
    let lastMsgId = "";
    while (remaining.length > 0) {
      let idx = remaining.length <= maxLen ? remaining.length : remaining.lastIndexOf("\n", maxLen);
      if (idx < 500) idx = Math.min(maxLen, remaining.length);
      const chunk = remaining.slice(0, idx);
      remaining = remaining.slice(idx).trimStart();
      const res = (await callTool("webex.notification.send" as any, {
        room_id: channelId,
        text: chunk,
        markdown: chunk,
      })) as any;
      if (res?.message_id) lastMsgId = String(res.message_id);
    }
    return lastMsgId;
  }

  async addReaction(channelId: string, messageId: string, emoji: string): Promise<string | void> {
    // Webex doesn't support bot reactions, so in-progress states post a temporary
    // indicator message. Terminal states (done, cancelled, failed) post nothing:
    // the indicator is deleted and the reply itself is the signal.
    const text = webexIndicatorText(emoji);
    if (!text) return;
    try {
      const res = (await callTool("webex.notification.send" as any, {
        room_id: channelId,
        text: text.plain,
        markdown: text.markdown,
      })) as any;
      if (res?.message_id) {
        return String(res.message_id);
      }
    } catch {}
  }

  async removeReaction(channelId: string, messageId: string, emoji: string): Promise<void> {
    // Delete the temporary waiting indicator message
    try {
      await callTool("webex.message.delete" as any, {
        message_id: messageId,
      });
    } catch {}
  }

  async sendTyping(channelId: string): Promise<void> {
    // Webex Messaging API has no typing indicator endpoint
  }
}

const IN_PROGRESS_EMOJI: Record<string, string> = { "⏳": "Thinking...", "🕒": "Queued..." };

export function webexIndicatorText(emoji: string): { plain: string; markdown: string } | null {
  const label = IN_PROGRESS_EMOJI[emoji];
  return label ? { plain: `${emoji} ${label}`, markdown: `${emoji} *${label}*` } : null;
}
