# Glossary

Shared vocabulary for Clara. Cockpit terms first; extend as the model grows.

**Cockpit** — The agent-management product: the Go service on Eve plus the
iPhone/Mac clients. See `docs/superpowers/specs/2026-11-04-cockpit-design.md`.

**Host** — A machine running agents under Clara. Currently `eve` (primary) and
`sophia` (satellite). Has an online state and a last-seen time.

**Eve** — The primary host. Owns the event log and is the only thing clients
connect to.

**Sophia** — The satellite host (laptop). Reaches Eve over a reverse SSH tunnel
and forwards its events up. Never client-facing.

**Agent** — A coding agent program Clara drives. Currently `opencode` and
`agy` (Antigravity CLI).

**Adapter** — Clara code that normalizes one Agent into Sessions and Events
(`spawn`, `send`, `interrupt`, `status`, `events`).

**Session** — One running conversation with an agent on a host. Either
*long-lived* (kept open and fed over time) or *task* (short-lived: spawn,
delegate, review, dismiss). Replaces the "Discord channel mapped to a tmux
window" notion.

**Backing** — What a Session physically runs in: an opencode server session or
a tmux target. Not the source of truth for content.

**Event** — An immutable, append-only record in the event log with a
monotonically increasing `seq`. The source of truth for conversations.

**Event log** — The Go-owned SQLite log on Eve holding all Events.

**Cursor** — A `seq` position. Clients sync from a cursor and keep per-device
read cursors.

**Attention** — An agent's request for the user's input (a question, with
options and optional free text). Agents run fully permissive, so Attention is
about guidance, not permissions.

**Session state** — `running`, `idle`, `needs_you`, `failed`, `done`. Only
`needs_you`, `done` and `failed` notify.

**Device** — A registered client (iPhone, Mac) with an APNs token, per-device
cursors, and a revocable credential.

**Client adapter** — A consumer of the event log that presents it elsewhere
(Discord, later). Not a source of truth.

**Fallback terminal** — Access to the raw agent TUI via tmux or `opencode
attach`. Optional and secondary to Events.
