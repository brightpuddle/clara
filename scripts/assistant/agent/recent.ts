/**
 * scripts/assistant/agent/recent.ts
 *
 * Groups open sessions by project and renders the `/recent` overview: one entry
 * per project, newest activity first, with that project's latest session.
 */

import { repoFromDir, sessionLabel, type SessionInfo } from "./sync.ts";

export interface RecentSession extends SessionInfo {
  last_activity?: string;
}

export interface ProjectSummary {
  project: string;
  latest: RecentSession;
  sessions: number;
  lastActivity: number;
}

export function activityMs(s: RecentSession): number {
  const t = s.last_activity ? Date.parse(s.last_activity) : NaN;
  return Number.isFinite(t) && t > 0 ? t : 0;
}

export function groupByProject(
  sessions: RecentSession[],
  projectOf: (dir: string) => string = repoFromDir
): ProjectSummary[] {
  const byProject = new Map<string, ProjectSummary>();
  for (const s of sessions.filter((x) => x.state !== "closed")) {
    const project = projectOf(s.dir) || "misc";
    const at = activityMs(s);
    const cur = byProject.get(project);
    if (!cur) {
      byProject.set(project, { project, latest: s, sessions: 1, lastActivity: at });
      continue;
    }
    cur.sessions++;
    if (at >= cur.lastActivity) {
      cur.latest = s;
      cur.lastActivity = at;
    }
  }
  return [...byProject.values()].sort((a, b) => b.lastActivity - a.lastActivity);
}

export function ago(ms: number, now: number): string {
  if (!ms) return "unknown";
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 60) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.round(m / 60);
  if (h < 48) return `${h}h ago`;
  return `${Math.round(h / 24)}d ago`;
}

const STATE_ICON: Record<string, string> = {
  running: "🟢",
  needs_you: "🟠",
  failed: "🔴",
  idle: "⚪",
  starting: "🟡",
};

export function formatRecent(
  sessions: RecentSession[],
  opts: {
    now?: number;
    limit?: number;
    link?: (s: RecentSession) => string | null;
    projectOf?: (dir: string) => string;
  } = {}
): string {
  const now = opts.now ?? Date.now();
  const groups = groupByProject(sessions, opts.projectOf);
  if (groups.length === 0) {
    return "No open agent sessions. Use `/spawn <name> [agy|opencode] [dir] [model=<id>]`.";
  }
  const shown = groups.slice(0, opts.limit ?? 8);
  const rows = shown.map((g) => {
    const icon = STATE_ICON[g.latest.state] ?? "⚪";
    const where = opts.link?.(g.latest) ?? `**${sessionLabel(g.latest)}**`;
    const more = g.sessions > 1 ? ` (+${g.sessions - 1} more)` : "";
    return `- ${icon} **${g.project}** — ${where}, ${g.latest.state}, ${ago(g.lastActivity, now)}${more}`;
  });
  const hidden = groups.length - shown.length;
  return [
    `### Recent activity (${groups.length} project${groups.length === 1 ? "" : "s"})`,
    ...rows,
    ...(hidden > 0 ? [`…and ${hidden} older project${hidden === 1 ? "" : "s"}. \`/sessions\` lists everything.`] : []),
  ].join("\n");
}
