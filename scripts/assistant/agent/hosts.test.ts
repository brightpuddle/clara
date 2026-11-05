import { describe, expect, test } from "bun:test";
import { agentToolFor, parseHostPrefixes, splitHost } from "./hosts.ts";

const remotes = { sophia: "laptop." };

describe("agentToolFor", () => {
  test("prefixes remote hosts only", () => {
    expect(agentToolFor("sophia", "agent.prompt.send", remotes)).toBe("laptop.agent.prompt.send");
    expect(agentToolFor("eve", "agent.prompt.send", remotes)).toBe("agent.prompt.send");
    expect(agentToolFor(undefined, "agent.session.list", remotes)).toBe("agent.session.list");
  });
});

describe("parseHostPrefixes", () => {
  test("defaults and overrides", () => {
    expect(parseHostPrefixes(undefined)).toEqual({ sophia: "laptop." });
    expect(parseHostPrefixes("a=x., b=y.")).toEqual({ a: "x.", b: "y." });
  });
});

describe("splitHost", () => {
  test("host prefixes", () => {
    expect(splitHost("sophia:dev", remotes)).toEqual({ host: "sophia", name: "dev" });
    expect(splitHost("laptop:dev", remotes)).toEqual({ host: "sophia", name: "dev" });
    expect(splitHost("eve:dev", remotes)).toEqual({ host: "eve", name: "dev" });
    expect(splitHost("plain", remotes)).toEqual({ host: undefined, name: "plain" });
    expect(splitHost("foo:bar", remotes)).toEqual({ host: undefined, name: "foo:bar" });
  });
});
