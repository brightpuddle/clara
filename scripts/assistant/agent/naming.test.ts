import { expect, test } from "bun:test";
import {
  NAME_FALLBACK,
  NAME_PROMPT,
  NAME_TITLE,
  promptName,
  shouldRename,
  titleName,
} from "./naming.ts";

test("prompt names are short, one line and skip commands", () => {
  expect(promptName("Fix the debounce bug in trigger manager\nmore detail")).toBe(
    "Fix the debounce bug in trigger manager"
  );
  expect(promptName("please look at https://example.com/x and refactor the whole router module")).toBe(
    "please look at and refactor the whole…"
  );
  expect(promptName("/plan something")).toBeNull();
  expect(promptName("  ")).toBeNull();
  expect(promptName("ok")).toBeNull();
});

test("stronger sources replace weaker ones", () => {
  expect(shouldRename(NAME_FALLBACK, NAME_PROMPT, false)).toBe(true);
  expect(shouldRename(NAME_PROMPT, NAME_PROMPT, false)).toBe(false);
  expect(shouldRename(NAME_TITLE, NAME_PROMPT, false)).toBe(false);
  expect(shouldRename(NAME_PROMPT, NAME_TITLE, false)).toBe(true);
  expect(shouldRename(NAME_TITLE, NAME_TITLE, true)).toBe(false);
});

test("titles", () => {
  expect(titleName("  Track   B  ")).toBe("Track B");
  expect(titleName("")).toBeNull();
});
