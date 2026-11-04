import { expect, test } from "bun:test";
import { webexIndicatorText } from "./webex.ts";

test("in-progress emoji produce a temporary indicator", () => {
  expect(webexIndicatorText("⏳")).toEqual({ plain: "⏳ Thinking...", markdown: "⏳ *Thinking...*" });
  expect(webexIndicatorText("🕒")?.plain).toBe("🕒 Queued...");
});

test("terminal emoji never post a message", () => {
  for (const e of ["✅", "🛑", "⚠️"]) expect(webexIndicatorText(e)).toBeNull();
});
