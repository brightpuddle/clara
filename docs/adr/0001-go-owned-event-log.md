# 0001: Conversation state is a Go-owned event log on Eve

Date: 2026-11-04. Status: accepted.

## Context

Conversation state currently lives in `assistant_state.db`, owned by the
TypeScript chat router, alongside Go's `clara.db`. Agent output is obtained by
scraping tmux (opencode) or reading hooks (agy). `GET /events` is a
non-replayable SSE firehose with no per-device state.

## Decision

Introduce an append-only event log in SQLite, owned by Go and hosted on Eve.
Each event has a monotonic `seq`. Devices hold server-side read cursors. The
TS chat router (Discord) becomes a client adapter that consumes the log.

## Consequences

- One source of truth across Go, TS and native clients.
- Offline clients catch up by cursor; read state syncs across devices.
- The chat router's tables (`chat_history`, `agy_hook_dedup`, etc.) migrate or
  retire, feature by feature.
- Eve is a single point of failure for the Cockpit (accepted).
