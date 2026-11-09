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
import { parseAgentWebexTitle } from "../agent/binding.ts";
import { NAME_FALLBACK, NAME_TITLE, type NameSource } from "../agent/naming.ts";
import { repoFromDir } from "../agent/sync.ts";
import { threadStore } from "../agent/threads.ts";

/** Webex spaces have no parent; this marks their rows in the shared binding table. */
export const WEBEX_ROOM_PARENT = "webex";

export function shortId(session: string): string {
  return session.slice(-4).toLowerCase();
}

export function webexSpaceTitle(host: string, project: string, label: string): string {
  return `${host}-${project}-${label}`.slice(0, 120);
}

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

  // Serializes concurrent space creation for the same session.
  private inflightAgent = new Map<string, Promise<ManagedAgentChannel>>();

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

  /**
   * Agent spaces are titled `<host>-<project>-<name>` and bound through the
   * assistant state database. Spaces with the older `[Clara:host] … agent:id`
   * title are still recognised until migrated.
   */
  async listAgentChannels(): Promise<ManagedAgentChannel[]> {
    const store = threadStore();
    const rooms = await this.listGroupRooms();
    const byId = new Map(rooms.list.map((r) => [String(r.id), r]));
    const out: ManagedAgentChannel[] = [];
    const bound = new Set<string>();
    for (const row of store.list()) {
      if (row.parentId !== WEBEX_ROOM_PARENT) continue;
      const room = byId.get(row.threadId);
      if (!room) {
        // A truncated listing can't prove a space is gone.
        if (rooms.complete) store.remove(row.threadId);
        else out.push({ id: row.threadId, name: row.name, host: row.host, session: row.session });
        continue;
      }
      bound.add(row.threadId);
      out.push({ id: row.threadId, name: String(room.title), host: row.host, session: row.session });
    }
    for (const r of rooms.list) {
      if (bound.has(String(r.id))) continue;
      const legacy = parseAgentWebexTitle(r.title);
      if (legacy) out.push({ id: String(r.id), name: String(r.title), ...legacy });
    }
    return out;
  }

  private async listGroupRooms(): Promise<{ list: any[]; complete: boolean }> {
    const max = 1000;
    const raw = ((await callTool("webex.rooms.list" as any, { max })) || []) as any[];
    return { list: raw.filter((r) => r.type === "group"), complete: raw.length < max };
  }

  async ensureAgentChannel(
    host: string,
    session: string,
    name: string,
    dir?: string,
    titled: boolean = true
  ): Promise<ManagedAgentChannel> {
    const existing = (await this.listAgentChannels()).find(
      (c) => c.host === host && c.session === session
    );
    if (existing) return existing;
    const key = `${host}:${session}`;
    const pending = this.inflightAgent.get(key);
    if (pending) return pending;
    const creating = this.createAgentSpace(host, session, name, dir, titled).finally(() => {
      this.inflightAgent.delete(key);
    });
    this.inflightAgent.set(key, creating);
    return creating;
  }

  private async createAgentSpace(
    host: string,
    session: string,
    name: string,
    dir?: string,
    titled: boolean = true
  ): Promise<ManagedAgentChannel> {
    const project = (dir ? repoFromDir(dir) : "") || "misc";
    const label = titled ? name.trim() || shortId(session) : shortId(session);
    const title = webexSpaceTitle(host, project, label);
    const created = (await callTool("webex.room.create" as any, {
      title,
      invite_bot: true,
    })) as any;
    const id = String(created.id);
    threadStore().put({
      threadId: id,
      parentId: WEBEX_ROOM_PARENT,
      host,
      session,
      project,
      name: label,
      nameSource: titled ? NAME_TITLE : NAME_FALLBACK,
    });
    return { id, name: title, host, session };
  }

  async renameAgentChannel(channelId: string, name: string, source: NameSource): Promise<void> {
    const store = threadStore();
    if (!store.applyName(channelId, name, source)) return;
    const row = store.byThread(channelId);
    if (!row) return;
    try {
      await callTool("webex.room.update" as any, {
        room_id: channelId,
        title: webexSpaceTitle(row.host, row.project, name),
      });
    } catch (err: any) {
      console.warn(`[webex] rename of ${channelId} failed: ${err.message}`);
    }
  }

  /** Moves spaces with the old bracketed title to the new title and binding. */
  async migrateLegacyAgentChannels(
    sessions: Array<{ id: string; host: string; dir: string; title?: string }>
  ): Promise<string[]> {
    const store = threadStore();
    const moved: string[] = [];
    const rooms = await this.listGroupRooms();
    for (const r of rooms.list) {
      const legacy = parseAgentWebexTitle(r.title);
      if (!legacy || store.byThread(String(r.id))) continue;
      const s = sessions.find((x) => x.host === legacy.host && x.id === legacy.session);
      if (!s) continue;
      const project = repoFromDir(s.dir) || "misc";
      const titled = Boolean(s.title?.trim());
      const label = titled ? s.title!.trim() : shortId(s.id);
      const title = webexSpaceTitle(legacy.host, project, label);
      try {
        await callTool("webex.room.update" as any, { room_id: String(r.id), title });
        store.put({
          threadId: String(r.id),
          parentId: WEBEX_ROOM_PARENT,
          host: legacy.host,
          session: legacy.session,
          project,
          name: label,
          nameSource: titled ? NAME_TITLE : NAME_FALLBACK,
        });
        moved.push(title);
      } catch (err: any) {
        console.warn(`[webex] migrating "${r.title}" failed: ${err.message}`);
      }
    }
    return moved;
  }

  async deleteChannel(channelId: string): Promise<void> {
    await callTool("webex.room.delete" as any, { room_id: channelId });
    threadStore().remove(channelId);
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
