# 0003: Native SwiftUI clients, in the Clara repo

Date: 2026-11-04. Status: accepted.

## Context

Requirements: reliable native notifications (APNs, actions, time-sensitive),
dictation and future voice, iPhone plus Mac, all-Apple, one user. The terminal
is no longer a first-class UI, which removes the main argument for web
(xterm.js). The user will pay for the Apple Developer Program.

## Decision

Build native SwiftUI clients (iOS first, then macOS) sharing a Swift package,
under `swift/Cockpit/` in this repo alongside `ClaraBridge`. Do not build a PWA
or hybrid shell. Keep the repo a monorepo so protocol changes are atomic.

## Consequences

- Full access to APNs, Live Activities, App Intents and on-device speech.
- Slower than a PWA to first prototype; mitigated by a simple first UI and a
  UI-less replay/tail CLI for protocol testing.
- Xcode project noise and CI cost; mitigated by path-scoped builds.
- If the app outgrows the repo it can be split later along the schema file.
