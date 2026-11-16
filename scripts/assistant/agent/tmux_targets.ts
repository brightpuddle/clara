/**
 * scripts/assistant/agent/tmux_targets.ts
 *
 * Decides which tmux sessions get a legacy tmux-scraping channel.
 *
 * opencode sessions are mirrored through the opencode service and get an
 * `agent:` channel from the reconciler, so a tmux session hosting an opencode
 * pane must not also get a tmux channel.
 */

export interface TmuxPane {
  session: string;
  paneIndex: string;
  command: string;
}

export function parseTmuxPanes(text: string): TmuxPane[] {
  const panes: TmuxPane[] = [];
  for (const line of text.split("\n")) {
    if (!line.trim()) continue;
    const [session, , paneIndex, , command] = line.split("|");
    if (!session) continue;
    panes.push({ session, paneIndex: paneIndex ?? "", command: command ?? "" });
  }
  return panes;
}

export function legacyTmuxTargets(panes: TmuxPane[]): string[] {
  const all = new Set<string>();
  const hasOpencode = new Set<string>();

  for (const p of panes) {
    all.add(p.session);
    const c = p.command.toLowerCase();
    if (c.includes("opencode")) {
      hasOpencode.add(p.session);
    }
  }

  const targets: string[] = [];
  for (const s of all) {
    if (hasOpencode.has(s)) continue;
    targets.push(s);
  }
  return targets;
}
