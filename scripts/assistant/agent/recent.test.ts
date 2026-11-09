import { expect, test } from "bun:test";
import { formatRecent, groupByProject, type RecentSession } from "./recent.ts";

const mk = (id: string, dir: string, at: string, state = "idle"): RecentSession => ({
  id,
  host: "eve",
  agent: "opencode",
  dir,
  title: id,
  state,
  last_activity: at,
});
const projectOf = (d: string) => d.split("/").pop() || "";

test("one entry per project, newest first, latest session wins", () => {
  const g = groupByProject(
    [
      mk("a1", "/x/alpha", "2026-11-09T10:00:00Z"),
      mk("a2", "/x/alpha", "2026-11-09T12:00:00Z"),
      mk("b1", "/x/beta", "2026-11-09T13:00:00Z"),
      mk("c1", "/x/gamma", "2026-11-09T14:00:00Z", "closed"),
    ],
    projectOf
  );
  expect(g.map((p) => p.project)).toEqual(["beta", "alpha"]);
  expect(g[1].latest.id).toBe("a2");
  expect(g[1].sessions).toBe(2);
});

test("formats with relative time, state and links", () => {
  const out = formatRecent([mk("a1", "/x/alpha", "2026-11-09T10:00:00Z", "running")], {
    now: Date.parse("2026-11-09T12:00:00Z"),
    projectOf,
    link: () => "<#123>",
  });
  expect(out).toContain("🟢 **alpha** — <#123>, running, 2h ago");
});

test("empty", () => {
  expect(formatRecent([])).toContain("No open agent sessions");
});
