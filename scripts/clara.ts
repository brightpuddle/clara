// scripts/clara.ts
// Standard TypeScript helper SDK for scripts executing under Clara supervision.
// Works seamlessly with Bun, Node, or Deno.

import type { ClaraToolName, ClaraTools, CloudEvent } from "./clara.d.ts";

export type { ClaraToolName, ClaraTools, CloudEvent };

/**
 * Read the incoming CloudEvent payload passed by the Clara supervisor via stdin or environment variable.
 */
export async function readEvent<T = any>(): Promise<CloudEvent<T>> {
  const envJson = process.env.CLARA_EVENT_JSON;
  if (envJson && envJson.trim().length > 0) {
    return JSON.parse(envJson) as CloudEvent<T>;
  }

  // Read from standard input
  let input = "";
  if (typeof Bun !== "undefined") {
    // Bun-native fast stream read
    input = await Bun.stdin.text();
  } else {
    // Node.js fallback
    input = await new Promise<string>((resolve, reject) => {
      let data = "";
      process.stdin.setEncoding("utf8");
      process.stdin.on("data", (chunk) => {
        data += chunk;
      });
      process.stdin.on("end", () => resolve(data));
      process.stdin.on("error", (err) => reject(err));
    });
  }

  if (input && input.trim().length > 0) {
    return JSON.parse(input) as CloudEvent<T>;
  }

  return {
    specversion: "1.0",
    id: "unknown",
    source: "stdin",
    type: "unknown",
    data: {} as T,
  };
}

/**
 * Invokes a Clara MCP tool via the Clara CLI with type-safe arguments and result shapes.
 */
export async function callTool<K extends ClaraToolName>(
  name: K,
  args: ClaraTools[K]["args"]
): Promise<ClaraTools[K]["result"]> {
  const cliArgs: string[] = ["tool", "call", name];

  if (args && typeof args === "object") {
    for (const [k, v] of Object.entries(args)) {
      if (v === undefined) continue;
      const valStr = typeof v === "object" ? JSON.stringify(v) : String(v);
      cliArgs.push(`${k}=${valStr}`);
    }
  }

  let stdout = "";
  let stderr = "";
  let exitCode = 0;

  if (typeof Bun !== "undefined") {
    const proc = Bun.spawn(["clara", ...cliArgs], {
      stdout: "pipe",
      stderr: "pipe",
    });
    stdout = await new Response(proc.stdout).text();
    stderr = await new Response(proc.stderr).text();
    exitCode = await proc.exited;
  } else {
    const { execFile } = await import("node:child_process");
    const { promisify } = await import("node:util");
    const execFileAsync = promisify(execFile);
    try {
      const res = await execFileAsync("clara", cliArgs);
      stdout = res.stdout;
      stderr = res.stderr;
    } catch (err: any) {
      exitCode = err.code ?? 1;
      stdout = err.stdout ?? "";
      stderr = err.stderr ?? err.message;
    }
  }

  if (exitCode !== 0) {
    throw new Error(`Clara tool ${name} failed (exit ${exitCode}): ${stderr || stdout}`);
  }

  try {
    return JSON.parse(stdout);
  } catch {
    return stdout.trim();
  }
}

/**
 * Returns the current Clara execution run ID if running under Clara supervision.
 */
export function runID(): string {
  return process.env.CLARA_RUN_ID || "unknown";
}

/**
 * Returns the current Clara trigger ID if running under Clara supervision.
 */
export function triggerID(): string {
  return process.env.CLARA_TRIGGER_ID || "unknown";
}
