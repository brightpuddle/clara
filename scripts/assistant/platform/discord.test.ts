import { expect, test } from "bun:test";
import { projectTopic, resolveDiscordBoundTmuxSession } from "./discord.ts";

test("project channels are never mistaken for tmux session channels", () => {
  expect(resolveDiscordBoundTmuxSession("eve-clara", projectTopic("eve", "clara"))).toBeNull();
});

test("name-prefix fallback still binds channels without a topic", () => {
  expect(resolveDiscordBoundTmuxSession("eve-ims", "")).toEqual({
    host: "eve",
    sessionName: "ims",
  });
});
