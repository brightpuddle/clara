# 0010: opencode web UI is the mobile client

Date: 2026-11-09. Status: accepted. Narrows Track E; builds on 0008 and 0009.

## Context

Track E planned a Clara client API, device registry, APNs and a native iOS app. The
opencode web UI was tried on Eve through the service with `opencode pair`, and works.
It organises sessions and projects the way Clara's own clients were meant to, and a
peer-client model (ADR 0008) means it shows the same live sessions as the TUI.
An official opencode iOS app is expected but not released. Building one from source
is possible with a personal Apple developer account, but would mean owning signing and
protocol churn.

## Decision

- The opencode web UI, reached over Tailscale (ADR 0009), is the mobile client.
- Do not build a Clara PWA, native app or `/v1` client API now. Do not build the
  opencode iOS app ourselves; wait for the official release and revisit then.
- Clara's role on mobile is notifications (Discord/Webex attention pings with deep
  links), the daily digest, triage and cross-host aggregation.
- Sophia stays on the SSH tunnel because Tailscale does not work with Cisco VPN, so
  its sessions need a proxy through Eve or a separate entry point (roadmap E3).

## Consequences

- Track E shrinks to Tailscale setup, deep links and the Sophia question.
- iOS web push and background reconnect are weak; Discord remains the notification path.
- Depends on opencode's web UI and pairing flow staying stable across versions.
