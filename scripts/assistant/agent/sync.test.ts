import { expect, test } from "bun:test";
import { planSync, repoFromDir, sessionLabel } from "./sync.ts";

const s = (id: string, state = "idle", dir = "/a/proj") => ({ id, host: "eve", agent: "agy", dir, state });
const c = (session: string) => ({ id: "c" + session, name: "n", host: "eve", session });

test("creates missing, removes closed and unknown, keeps live", () => {
  const plan = planSync([s("A"), s("B"), s("C", "closed")], [c("B"), c("C"), c("Z")]);
  expect(plan.create.map((x) => x.id)).toEqual(["A"]);
  expect(plan.remove.map((x) => x.session)).toEqual(["C", "Z"]);
  expect(plan.keep.map((x) => x.session)).toEqual(["B"]);
});

test("removes duplicate channels bound to the same session", () => {
  const plan = planSync(
    [s("B")],
    [
      { id: "c1", name: "n", host: "eve", session: "B" },
      { id: "c2", name: "n", host: "eve", session: "B" },
    ]
  );
  expect(plan.keep.map((x) => x.id)).toEqual(["c1"]);
  expect(plan.remove.map((x) => x.id)).toEqual(["c2"]);
});

test("label uses title or git repo name", () => {
  expect(sessionLabel({ ...s("01ABCDWXYZ"), title: "Fix bug" })).toBe("Fix bug");
  expect(sessionLabel(s("01ABCDWXYZ", "idle", "/Users/nathan/src/clara/main"))).toBe("clara-wxyz");
  expect(sessionLabel(s("01ABCDWXYZ", "idle", "/a/proj"))).toBe("proj-wxyz");
});

test("repoFromDir resolves git repo and bare worktree paths", () => {
  expect(repoFromDir("/Users/nathan/src/clara/main")).toBe("clara");
  expect(repoFromDir("/Users/nathan/src/lucia/main")).toBe("lucia");
  expect(repoFromDir("/Users/nathan/src/clara/.bare")).toBe("clara");
  expect(repoFromDir("/Users/nathan/src/some-repo/worktrees/feat-x")).toBe("some-repo");
});
