# 0011: IMS incidents reach Clara as events; Clara owns the agent workflow

Date: 2026-11-12. Status: accepted.

## Context

IMS (inventory app with its own ticketing) launched agents itself: it hardcoded the
agent harness, working directory and prompt, managed a long-lived "IMS Ticket Resolver"
session, and ran a blocking `notify.ask` flow. That mixed IMS (inventory) with Clara
(agentic development) and made IMS choose the provider, model and harness.

## Decision

- IMS emits one-way, best-effort CloudEvents on the bus (source `ims`):
  `ims.incident.created` and `ims.incident.agent_requested`. Payload: incident id, url,
  user, marketplace, operation, item, tab id, message, comments. IMS knows nothing else
  about Clara.
- Clara owns the workflow in `~/.config/clara/triggers/ims_incident.yaml` and
  `scripts/ims/incident.ts`:
  - `created` posts a Discord `#notify` card with Launch Agent / Ignore. Agents never
    start without approval.
  - `agent_requested` (the button inside IMS) launches an agent immediately.
  - Clara picks the agent (`opencode` by default, `CLARA_IMS_AGENT` to override), the
    directory and the prompt. Each incident gets a fresh task session.
- Agents commit fixes directly to `main` (small single-user app; git is the backout) and
  close the ticket with `ims incident resolve <id> --summary ...`.

## Consequences

- Notifications are push-based and prompt. If Clara is down, IMS only logs a warning;
  there is no replay of missed events yet.
- A pending card is a blocked script run (24h timeout); a daemon restart drops it, and
  the agent can still be launched from the IMS button.
- Reminders integration (complete reminder -> close issue) is deferred.
