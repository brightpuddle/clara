import { expect, test } from "bun:test";
import { parseDotCommand } from "./commands.ts";

test("captures dot commands", () => {
  expect(parseDotCommand(".help")).toEqual({ name: "help", args: "" });
  expect(parseDotCommand(".Model gemini-3.8-flash-low")).toEqual({
    name: "model",
    args: "gemini-3.8-flash-low",
  });
});

test("leaves slash forwarding and ordinary text alone", () => {
  expect(parseDotCommand("./clear")).toBeNull();
  expect(parseDotCommand("...")).toBeNull();
  expect(parseDotCommand("fix .gitignore")).toBeNull();
  expect(parseDotCommand(".5 seconds")).toBeNull();
});
