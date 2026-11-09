# 0009: Mobile access to opencode over Tailscale only

Date: 2026-11-09. Status: accepted. Resolves the pending roadmap decision on remote
access to the opencode service.

## Context

Track E needs the phone to reach the opencode service on Eve. The service can run shell
commands and edit files as the user, so anyone who reaches it effectively has remote
code execution on Eve. Options considered:

- Public route through the existing Caddy instance: one routing mistake exposes a shell
  to the internet; needs mTLS or an SSO gate to be acceptable.
- Cloudflare Tunnel or WARP: traffic is terminated at a third party.
- Tailscale: WireGuard, end-to-end encrypted, direct peer to peer where NAT traversal
  succeeds, encrypted DERP relay otherwise. No public exposure.

Sophia cannot run Tailscale alongside Cisco AnyConnect (route and DNS conflicts, blocked
UDP). It already reaches Eve over the reverse SSH tunnel.

## Decision

- The phone (and other personal devices) reach Eve over Tailscale. No public route to
  the opencode service.
- opencode stays bound to `127.0.0.1`. It is exposed on the tailnet through
  `tailscale serve` or Caddy bound to the tailnet interface only.
- opencode's own server password stays enabled as a second layer.
- Tailnet ACLs restrict access to the user's own devices. Tailnet Lock is optional.
- Eve: disable key expiry, prevent system sleep, and use a Tailscale install that
  recovers unattended after reboot (standalone build with auto-login, or the Homebrew
  `tailscaled` LaunchDaemon).
- Sophia keeps the reverse SSH tunnel (`com.brightpuddle.clara.vpn-tunnel.plist`).
  Tailscale is not used there.
- First mobile step: try `opencode web` over Tailscale before building a PWA.

## Consequences

- No internet-facing attack surface for the agent service.
- Depends on Tailscale's coordination server for identity and key distribution.
- Mobile access requires the phone to be on the tailnet.
- Two remote paths to maintain: Tailscale (Eve and phone) and SSH tunnel (Sophia).
