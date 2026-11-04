# 0002: Cockpit clients use a new versioned sync API

Date: 2026-11-04. Status: accepted.

## Context

`/events` streams plugin events to automation with no cursor or replay. Mobile
clients disconnect constantly and must resume exactly.

## Decision

Add `/v1/sync` (cursor catch-up), a WebSocket for live events, and
`/v1/commands` (idempotent, client-keyed). `/events` and `/mcp` remain for
automation. The wire schema is a single source file used to generate Go and
Swift types. Clients reach Eve over Tailscale and authenticate with
per-device bearer tokens.

## Consequences

- Clean separation between automation (MCP, `/events`) and the product API.
- Retries are safe; reconnects are lossless.
- Schema generation tooling must be set up for Go and Swift.
