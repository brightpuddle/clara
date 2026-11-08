import { expect, test } from "bun:test";
import { formatOrigin } from "./binding.ts";
import { Relay, type ChannelRef, type Chat, type HostEvent, type PromptRecord, type Store } from "./relay_core.ts";

function memStore(): Store {
  const prompts = new Map<string, PromptRecord>();
  const turns = new Map<string, string>();
  const running = new Set<string>();
  return {
    getPrompt: (id) => prompts.get(id),
    putPrompt: (id, r) => void prompts.set(id, r),
    getTurnPrompt: (t) => turns.get(t),
    setTurnPrompt: (t, p) => void turns.set(t, p),
    isRunning: (s) => running.has(s),
    setRunning: (s, r) => void (r ? running.add(s) : running.delete(s)),
  };
}

function fakeChat(bound = true) {
  const posts: string[] = [];
  const statuses: Array<string | null> = [];
  const ensured: Array<{ host: string; session: string; payload: any }> = [];
  const chat: Chat = {
    channelFor: async (): Promise<ChannelRef | null> =>
      bound ? { platform: "discord", channelId: "C1" } : null,
    ensureChannel: async (host, session, payload): Promise<ChannelRef | null> => {
      ensured.push({ host, session, payload });
      return { platform: "discord", channelId: "C1" };
    },
    post: async (_c, text) => void posts.push(text),
    setStatus: async (_o, _prev, next) => {
      statuses.push(next);
      return undefined;
    },
  };
  return { chat, posts, statuses, ensured };
}

let seq = 0;
const ev = (type: string, payload: any, session = "S1"): HostEvent => ({
  host_seq: ++seq,
  host: "eve",
  session,
  type,
  payload,
});

const origin = formatOrigin({ platform: "discord", channelId: "C1", messageId: "M1" });

test("prompt lifecycle drives reactions and posts the reply", async () => {
  const { chat, posts, statuses } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("prompt.queued", { prompt_id: "P1", origin, position: 0 }));
  await r.handle(ev("turn.started", { turn_id: "T1", prompt_id: "P1" }));
  await r.handle(ev("message.assistant", { message_id: "A1", turn_id: "T1", text: "hello" }));
  await r.handle(ev("turn.ended", { turn_id: "T1", stop_reason: "end_turn" }));
  expect(posts).toEqual(["hello"]);
  expect(statuses).toEqual(["⏳", "✅"]);
});

test("a prompt queued behind a running turn is marked queued", async () => {
  const { chat, statuses } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("turn.started", { turn_id: "T1" }));
  await r.handle(ev("prompt.queued", { prompt_id: "P2", origin, position: 0 }));
  expect(statuses).toEqual(["🕒"]);
});

test("cancelled and error turns get distinct reactions", async () => {
  const { chat, statuses, posts } = fakeChat();
  const r = new Relay(memStore(), chat);
  for (const [i, reason] of ["cancelled", "error"].entries()) {
    await r.handle(ev("prompt.queued", { prompt_id: `P${i}`, origin, position: 0 }));
    await r.handle(ev("turn.started", { turn_id: `T${i}`, prompt_id: `P${i}` }));
    await r.handle(ev("turn.ended", { turn_id: `T${i}`, stop_reason: reason }));
  }
  expect(statuses).toEqual(["⏳", "🛑", "⏳", "⚠️"]);
  expect(posts).toHaveLength(1);
});

test("prompts without a chat origin (CLI) get no reactions but replies still post", async () => {
  const { chat, posts, statuses } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("prompt.queued", { prompt_id: "P1", origin: "cli", position: 0 }));
  await r.handle(ev("turn.started", { turn_id: "T1", prompt_id: "P1" }));
  await r.handle(ev("message.assistant", { message_id: "A", turn_id: "T1", text: "hi" }));
  expect(statuses).toEqual([]);
  expect(posts).toEqual(["hi"]);
});

test("sessions without a channel are ignored", async () => {
  const { chat, posts } = fakeChat(false);
  const r = new Relay(memStore(), chat);
  await r.handle(ev("message.assistant", { message_id: "A", turn_id: "T", text: "hi" }));
  expect(posts).toEqual([]);
});

test("failed and blocked sessions and questions are announced", async () => {
  const { chat, posts } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("session.status", { state: "failed", reason: "boom" }));
  await r.handle(ev("session.status", { state: "blocked", blocked: { kind: "quota" } }));
  await r.handle(ev("attention.requested", { kind: "question", title: "Pick", options: [{ label: "a" }] }));
  await r.handle(ev("attention.requested", { kind: "turn_ended" }));
  expect(posts).toEqual(["❌ Session failed: boom", "⏸️ Session blocked: quota", "❓ Pick\n1. a"]);
});

test("session.created triggers ensureChannel on chat", async () => {
  const { chat, ensured } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("session.created", { agent: "agy", dir: "/path" }, "S123"));
  expect(ensured).toEqual([{ host: "eve", session: "S123", payload: { agent: "agy", dir: "/path" } }]);
});

test("TUI prompts are mirrored, chat prompts are not", async () => {
  const { chat, posts } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(ev("message.user", { message_id: "U1", text: "typed in tui", origin: "opencode" }));
  await r.handle(ev("message.user", { message_id: "U2", text: "from chat", origin }));
  expect(posts).toEqual(["🖥️ **TUI:**\ntyped in tui"]);
});

test("form attentions invite a reply and resolution elsewhere is announced", async () => {
  const { chat, posts } = fakeChat();
  const r = new Relay(memStore(), chat);
  await r.handle(
    ev("attention.requested", {
      attention_id: "F1",
      kind: "question",
      title: "Which db?",
      options: [{ id: "pg", label: "Postgres" }],
      native: { form_id: "F1" },
    })
  );
  await r.handle(ev("attention.resolved", { attention_id: "F1", by: "discord", answer: "pg" }));
  await r.handle(ev("attention.resolved", { attention_id: "F1", by: "opencode", answer: "pg" }));
  expect(posts[0]).toContain("1. Postgres");
  expect(posts[0]).toContain("Reply in this channel");
  expect(posts).toHaveLength(2);
  expect(posts[1]).toContain("answered in opencode");
});

import { attentionAnswer } from "./relay_core.ts";

test("attentionAnswer maps numbers, labels and free text", () => {
  const att = [
    { id: "F1", session: "S1", open: true, native: { form_id: "F1" }, allow_text: true,
      options: [{ id: "pg", label: "Postgres" }, { id: "my", label: "MySQL" }] },
  ];
  expect(attentionAnswer(att, "S1", "2")).toEqual({ id: "F1", answer: "my" });
  expect(attentionAnswer(att, "S1", "postgres")).toEqual({ id: "F1", answer: "pg" });
  expect(attentionAnswer(att, "S1", "something else")).toEqual({ id: "F1", answer: "something else" });
  expect(attentionAnswer(att, "S2", "2")).toBeNull();
  expect(attentionAnswer([{ ...att[0], allow_text: false }], "S1", "nope")).toBeNull();
});
