/**
 * scripts/assistant/agent/relay_core.ts
 *
 * Pure translation of agent.* host events into chat actions (replies and
 * status reactions). Side effects go through the Store and Chat ports so the
 * logic is testable without Discord, Webex or Clara.
 */

import { parseOrigin, type Origin } from "./binding.ts";

export interface HostEvent {
  /** Global hub sequence (the relay cursor). */
  seq?: number;
  host_seq: number;
  host: string;
  session: string;
  type: string;
  payload: any;
}

export interface PromptRecord {
  session: string;
  origin: Origin | null;
  /** Current status emoji on the user's message, if any. */
  status?: string;
  /** Platform-specific indicator id (Webex posts a temporary message). */
  indicator?: string;
}

export interface Store {
  getPrompt(id: string): PromptRecord | undefined;
  putPrompt(id: string, rec: PromptRecord): void;
  getTurnPrompt(turnId: string): string | undefined;
  setTurnPrompt(turnId: string, promptId: string): void;
  isRunning(session: string): boolean;
  setRunning(session: string, running: boolean): void;
}

export interface ChannelRef {
  platform: "discord" | "webex";
  channelId: string;
}

export interface Chat {
  channelFor(host: string, session: string): Promise<ChannelRef | null>;
  ensureChannel?(host: string, session: string, payload: any): Promise<ChannelRef | null>;
  post(ch: ChannelRef, text: string): Promise<void>;
  /** Replaces the status emoji prev with next; returns a new indicator id if the platform has one. */
  setStatus(
    origin: Origin,
    prev: { emoji?: string; indicator?: string },
    next: string | null
  ): Promise<string | undefined>;
}

export const EMOJI = {
  queued: "🕒",
  running: "⏳",
  done: "✅",
  cancelled: "🛑",
  failed: "⚠️",
};

export class Relay {
  constructor(
    private store: Store,
    private chat: Chat
  ) {}

  async handle(ev: HostEvent): Promise<void> {
    const p = ev.payload || {};
    switch (ev.type) {
      case "session.created": {
        if (this.chat.ensureChannel) {
          await this.chat.ensureChannel(ev.host, ev.session, p);
        }
        return;
      }
      case "prompt.queued": {
        const origin = parseOrigin(p.origin);
        const rec: PromptRecord = { session: ev.session, origin };
        this.store.putPrompt(p.prompt_id, rec);
        if (origin && this.store.isRunning(ev.session)) await this.status(p.prompt_id, EMOJI.queued);
        return;
      }
      case "turn.started": {
        this.store.setRunning(ev.session, true);
        if (p.prompt_id) {
          this.store.setTurnPrompt(p.turn_id, p.prompt_id);
          await this.status(p.prompt_id, EMOJI.running);
        }
        return;
      }
      case "message.assistant": {
        const text = String(p.text || "").trim();
        if (!text) return;
        const ch = await this.chat.channelFor(ev.host, ev.session);
        if (ch) await this.chat.post(ch, text);
        return;
      }
      case "turn.ended": {
        this.store.setRunning(ev.session, false);
        const promptId = this.store.getTurnPrompt(p.turn_id);
        if (promptId) {
          const emoji =
            p.stop_reason === "end_turn"
              ? EMOJI.done
              : p.stop_reason === "cancelled"
                ? EMOJI.cancelled
                : EMOJI.failed;
          await this.status(promptId, emoji);
        }
        if (p.stop_reason === "error") await this.notice(ev, "⚠️ The turn ended with an error.");
        return;
      }
      case "session.status": {
        if (p.state === "failed") {
          await this.notice(ev, `❌ Session failed${p.reason ? `: ${p.reason}` : "."}`);
        } else if (p.state === "blocked") {
          const resets = p.blocked?.resets_at ? ` (resets ${p.blocked.resets_at})` : "";
          await this.notice(ev, `⏸️ Session blocked: ${p.blocked?.kind || p.reason || "unknown"}${resets}`);
        }
        return;
      }
      case "attention.requested": {
        if (p.kind !== "question") return;
        const opts = (p.options || []).map((o: any, i: number) => `${i + 1}. ${o.label}`).join("\n");
        await this.notice(ev, `❓ ${p.title || "The agent needs input."}${opts ? `\n${opts}` : ""}`);
        return;
      }
    }
  }

  private async notice(ev: HostEvent, text: string) {
    const ch = await this.chat.channelFor(ev.host, ev.session);
    if (ch) await this.chat.post(ch, text);
  }

  private async status(promptId: string, emoji: string) {
    const rec = this.store.getPrompt(promptId);
    if (!rec?.origin) return;
    try {
      const indicator = await this.chat.setStatus(
        rec.origin,
        { emoji: rec.status, indicator: rec.indicator },
        emoji
      );
      this.store.putPrompt(promptId, { ...rec, status: emoji, indicator });
    } catch (err: any) {
      console.warn(`[agent_relay] status update failed: ${err.message}`);
    }
  }
}
