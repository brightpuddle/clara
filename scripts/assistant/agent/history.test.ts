import { expect, test } from "bun:test";
import { formatHistory } from "./history.ts";

const u = (text: string) => ({ type: "message.user", payload: { text } });
const a = (text: string) => ({ type: "message.assistant", payload: { text } });

test("formats the conversation and ignores other events", () => {
  const out = formatHistory([u("poem?"), { type: "tool.call", payload: {} }, a("roses")]);
  expect(out).toContain("**You:** poem?");
  expect(out).toContain("**Agent:** roses");
  expect(out).not.toContain("last");
});

test("keeps the newest messages within budget", () => {
  const out = formatHistory([u("old ".repeat(100)), a("new")], 50);
  expect(out).toContain("**Agent:** new");
  expect(out).not.toContain("old");
  expect(out).toContain("last 1 of 2");
});

test("empty history yields nothing", () => {
  expect(formatHistory([{ type: "turn.started", payload: {} }])).toBe("");
});
