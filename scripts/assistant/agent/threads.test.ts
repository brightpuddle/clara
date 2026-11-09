import { Database } from "bun:sqlite";
import { expect, test } from "bun:test";
import { ThreadStore } from "./threads.ts";

const b = {
  threadId: "t1",
  parentId: "p1",
  host: "eve",
  session: "ses_1",
  project: "clara",
  name: "fix debounce",
  nameSource: 2,
};

test("stores, finds and removes bindings", () => {
  const s = new ThreadStore(new Database(":memory:"));
  s.put(b);
  expect(s.byThread("t1")).toEqual(b);
  expect(s.byThread("nope")).toBeNull();
  expect(s.list()).toEqual([b]);
  s.remove("t1");
  expect(s.list()).toEqual([]);
});

test("applyName honours name sources", () => {
  const s = new ThreadStore(new Database(":memory:"));
  s.put({ ...b, name: "clara-hcjf", nameSource: 0 });
  expect(s.applyName("t1", "Fix the bug", 1)).toBe(true);
  expect(s.applyName("t1", "Another prompt", 1)).toBe(false);
  expect(s.applyName("t1", "Real title", 2)).toBe(true);
  expect(s.applyName("t1", "Late prompt", 1)).toBe(false);
  expect(s.byThread("t1")?.name).toBe("Real title");
  expect(s.applyName("nope", "x", 2)).toBe(false);
  s.remove("t1");
  expect(s.list()).toEqual([]);
});
