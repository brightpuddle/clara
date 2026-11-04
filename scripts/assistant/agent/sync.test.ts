import { expect, test } from "bun:test";
import { planSync, sessionLabel } from "./sync.ts";

const s = (id: string, state = "idle") => ({ id, host: "eve", agent: "agy", dir: "/a/proj", state });
const c = (session: string) => ({ id: "c" + session, name: "n", host: "eve", session });

test("creates missing, removes closed and unknown, keeps live", () => {
  const plan = planSync([s("A"), s("B"), s("C", "closed")], [c("B"), c("C"), c("Z")]);
  expect(plan.create.map((x) => x.id)).toEqual(["A"]);
  expect(plan.remove.map((x) => x.session)).toEqual(["C", "Z"]);
  expect(plan.keep.map((x) => x.session)).toEqual(["B"]);
});

test("label uses title or dir plus id suffix", () => {
  expect(sessionLabel({ ...s("01ABCDWXYZ"), title: "Fix bug" })).toBe("Fix bug-wxyz");
  expect(sessionLabel(s("01ABCDWXYZ"))).toBe("proj-wxyz");
});
