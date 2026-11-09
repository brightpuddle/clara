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
