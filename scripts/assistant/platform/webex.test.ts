import { expect, test } from "bun:test";
import { resolveWebexBoundTmuxSession, shortId, webexIndicatorText, webexSpaceTitle } from "./webex.ts";

test("in-progress emoji produce a temporary indicator", () => {
  expect(webexIndicatorText("⏳")).toEqual({ plain: "⏳ Thinking...", markdown: "⏳ *Thinking...*" });
  expect(webexIndicatorText("🕒")?.plain).toBe("🕒 Queued...");
});

test("terminal emoji never post a message", () => {
  for (const e of ["✅", "🛑", "⚠️"]) expect(webexIndicatorText(e)).toBeNull();
});

test("agent spaces are titled host-project-label without brackets", () => {
  expect(webexSpaceTitle("eve", "clara", shortId("ses_e42c1c16dffezdsulAuv05Hcjf"))).toBe("eve-clara-hcjf");
  expect(webexSpaceTitle("sophia", "lucia", "Tier 3 readiness")).toBe("sophia-lucia-Tier 3 readiness");
});

test("agent space titles are not mistaken for tmux spaces", () => {
  expect(resolveWebexBoundTmuxSession("eve-clara-hcjf")).toBeNull();
});
