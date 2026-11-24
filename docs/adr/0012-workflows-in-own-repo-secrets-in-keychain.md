# 0012: Workflows live in their own repo; secrets come from the keychain

Date: 2026-11-24. Status: accepted.

## Context

Live workflows (`~/.config/clara/scripts`, triggers, rules) had no version control, and
`config.yaml` held plaintext tokens. `config.Save` also rewrote the whole file with resolved
values, so any `clara mcp add/remove` would have re-written secrets to disk.

## Decision

- Personal workflows are not part of the clara codebase. `~/.config/clara` is its own git
  repo with an allowlist-style `.gitignore`: scripts, `triggers-available/`, rule/theme/sync
  YAML, and `config.yaml`. Runtime state (`audit.db`, `learned_rules.yaml`), `certs/`,
  built `integrations/`, installed `schemas/` and `sdk/`, and per-host `conf.d/` are not tracked.
- Enabled triggers are per-host symlinks in `triggers/` and are not tracked.
- Secrets: 1Password vault `Clara` is the source of truth; a sync script copies items into
  the login keychain as `op-<title>` on each host. Config references them as
  `${keychain:op-<title>}`, resolved at load via `/usr/bin/security` (after YAML parsing,
  never re-expanded). A missing item is a startup error. `config.Save` preserves references.
  `clara secrets check` verifies a host. Scripts never read secrets directly; tools do.
- A native encrypted vault was rejected: it would still need a master key, and the keychain
  is that place.

## Consequences

- Eve must keep its login keychain unlocked (auto-login, no lock timeout).
- Per-host config differences (e.g. `agents.host`, `laptop` MCP server) should move to an
  untracked `conf.d/host.yaml`; not done yet.
- Roadmap A5 is satisfied.
