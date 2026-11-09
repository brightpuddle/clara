import { expect, test } from "bun:test";
import { parseSlashCommand } from "./commands.ts";

test("captures Clara slash commands", () => {
  expect(parseSlashCommand("/help")).toEqual({ name: "help", args: "" });
  expect(parseSlashCommand("/Model gemini-3.8-flash-low")).toEqual({
    name: "model",
    args: "gemini-3.8-flash-low",
  });
});

test("forwards agent commands and ordinary text", () => {
  expect(parseSlashCommand("/plan")).toBeNull();
  expect(parseSlashCommand("/compact now")).toBeNull();
  expect(parseSlashCommand(".help")).toBeNull();
  expect(parseSlashCommand("see /help")).toBeNull();
  expect(parseSlashCommand("/usr/bin/env")).toBeNull();
});
