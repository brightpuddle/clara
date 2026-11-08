import { expect, test } from "bun:test";
import {
  agentTopic,
  formatOrigin,
  parseAgentTopic,
  parseAgentWebexTitle,
  parseOrigin,
  webexAgentTitle,
} from "./binding.ts";
import { resolveDiscordBoundTmuxSession } from "../platform/discord.ts";

test("topic round trip and tmux topics are not agent bindings", () => {
  expect(parseAgentTopic(agentTopic("eve", "01ABC"))).toEqual({ host: "eve", session: "01ABC" });
  expect(parseAgentTopic("tmux:eve:dev — x")).toBeNull();
});

test("webex title round trip", () => {
  expect(parseAgentWebexTitle(webexAgentTitle("eve", "dev", "01ABC"))).toEqual({
    host: "eve",
    session: "01ABC",
  });
  expect(parseAgentWebexTitle("[Clara:eve] dev")).toBeNull();
});

test("origin round trip; cli and foreign origins are rejected", () => {
  const o = { platform: "discord" as const, channelId: "1", messageId: "2" };
  expect(parseOrigin(formatOrigin(o))).toEqual(o);
  expect(parseOrigin("discord:1:cli-99")).toBeNull();
  expect(parseOrigin("cli")).toBeNull();
});

test("agent channels are invisible to the tmux resolver (so .sync keeps them)", () => {
  expect(resolveDiscordBoundTmuxSession("eve-dev", agentTopic("eve", "01ABC"))).toBeNull();
  expect(resolveDiscordBoundTmuxSession("eve-dev", "")).toEqual({ host: "eve", sessionName: "dev" });
});

test("hyphenated tmux session names are not truncated", () => {
  expect(
    resolveDiscordBoundTmuxSession("eve-ims-resolver", "tmux:eve:ims-resolver — Type directly")
  ).toEqual({ host: "eve", sessionName: "ims-resolver" });
});
