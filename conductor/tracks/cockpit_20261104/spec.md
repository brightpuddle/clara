# Track cockpit_20261104: Cockpit

## Summary
One purpose-built app for directing agents (agy, opencode) from anywhere, with
reliable attention signals and the same state on every device. Replaces Discord
as the supervision surface for agent sessions.

## Source documents
- `docs/superpowers/specs/2026-11-04-cockpit-design.md` (vision, architecture)
- `docs/superpowers/specs/2026-11-04-cockpit-event-schema-design.md` (schema,
  commands, sync API, adapter interface)
- `docs/superpowers/specs/2026-11-04-opencode-v2-spike.md` (evidence)
- `docs/adr/0001` to `0005`, `docs/GLOSSARY.md`

## Acceptance criteria
1. A managed agy session and a managed opencode session run through one ACP
   adapter on both Eve and Sophia, and appear in one event log on Eve.
2. A client can disconnect for hours and resume exactly from its cursor.
3. A finished turn or a question produces exactly one push notification on the
   iPhone, with reply from the notification for options.
4. Prompts can be queued, edited, and cancelled; a running turn can be
   interrupted.
5. Sophia sleeping and waking loses no events.
6. Read state syncs across devices.
7. Discord remains a working client adapter during the transition and can be
   retired for agent sessions.

## Non-goals
Terminal emulation in v1, multi-user, non-Apple clients, hosted agent clouds.
