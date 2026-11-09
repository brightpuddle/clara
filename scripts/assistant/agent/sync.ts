/**
 * scripts/assistant/agent/sync.ts
 *
 * Reconciles managed agent sessions with their chat channels: open sessions get
 * a channel, channels of closed or unknown sessions are removed.
 */

import { execSync } from "node:child_process";
import { basename, dirname, resolve } from "node:path";

export interface SessionInfo {
  id: string;
  host: string;
  agent: string;
  dir: string;
  title?: string;
  state: string;
}

export interface AgentChannel {
  id: string;
  name: string;
  host: string;
  session: string;
}

export interface SyncPlan {
  create: SessionInfo[];
  remove: AgentChannel[];
  keep: AgentChannel[];
}

export function planSync(sessions: SessionInfo[], channels: AgentChannel[]): SyncPlan {
  const open = sessions.filter((s) => s.state !== "closed");
  const openKey = new Set(open.map((s) => `${s.host}:${s.id}`));
  const have = new Set<string>();
  const create: SessionInfo[] = [];
  const keep: AgentChannel[] = [];
  const remove: AgentChannel[] = [];

  // Keep the first channel per (host, session); drop the rest as duplicates.
  for (const c of channels) {
    const key = `${c.host}:${c.session}`;
    if (!openKey.has(key)) {
      remove.push(c);
    } else if (have.has(key)) {
      remove.push(c);
    } else {
      have.add(key);
      keep.push(c);
    }
  }
  for (const s of open) {
    if (!have.has(`${s.host}:${s.id}`)) create.push(s);
  }
  return { create, remove, keep };
}

/**
 * Extracts the git repository name for a directory, handling bare repositories,
 * worktrees, standard git repositories, and common worktree directory patterns.
 */
export function repoFromDir(dir?: string): string {
  if (!dir) return "";
  try {
    const common = execSync("git rev-parse --git-common-dir", {
      cwd: dir,
      encoding: "utf8",
      stdio: ["pipe", "pipe", "ignore"],
    }).trim();
    if (common) {
      const full = resolve(dir, common);
      const b = basename(full);
      if (b === ".bare" || b === ".git") {
        return basename(dirname(full));
      }
      return b;
    }
  } catch {}
  try {
    const top = execSync("git rev-parse --show-toplevel", {
      cwd: dir,
      encoding: "utf8",
      stdio: ["pipe", "pipe", "ignore"],
    }).trim();
    if (top) return basename(top);
  } catch {}
  const norm = dir.replace(/[\/\\]+$/, "");
  const m = norm.match(/(?:^|[\/\\])([^/\\]+)(?:[\/\\](?:main|master|develop|v[0-9]+|\.bare|\.crush|\.gemini|worktrees[\/\\][^/\\]+))$/);
  if (m) return m[1];
  return basename(norm);
}

/**
 * A readable, mostly-unique channel name for a session.
 *
 * Sessions in the same repo with no title would otherwise collide on the repo
 * name, so the no-title fallbacks always append a short id suffix to stay unique.
 */
export function sessionLabel(s: SessionInfo): string {
  const title = s.title?.trim().slice(0, 40).trim();
  if (title) return title;
  const repo = repoFromDir(s.dir);
  const stem = repo || basename(s.dir) || s.agent;
  return `${stem}-${s.id.slice(-4).toLowerCase()}`;
}
