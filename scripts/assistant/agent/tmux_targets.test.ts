import { describe, expect, test } from "bun:test";
import { legacyTmuxTargets, parseTmuxPanes } from "./tmux_targets";

const targets = (text: string) => legacyTmuxTargets(parseTmuxPanes(text));

describe("legacyTmuxTargets", () => {
  test("plain shell sessions keep a tmux channel", () => {
    expect(targets("ims|0|0|%1|zsh")).toEqual(["ims"]);
  });

  test("sessions hosting opencode are skipped", () => {
    expect(targets("clara|0|0|%1|opencode\nclara|0|1|%2|zsh\nims|0|0|%3|zsh")).toEqual(["ims"]);
  });

  test("agy panes keep the legacy path, one target per pane when several", () => {
    expect(targets("a|0|0|%1|agy")).toEqual(["a"]);
    expect(targets("a|0|0|%1|agy\na|0|1|%2|agy")).toEqual(["a:0", "a:1"]);
  });

  test("opencode plus agy keeps only the agy panes", () => {
    expect(targets("a|0|0|%1|opencode\na|0|1|%2|agy")).toEqual(["a"]);
  });
});
