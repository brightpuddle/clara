---
plan_recommended: true
---

# ClaraBridge Extensions (EventKit, Apple Mail, Webex)

## Planning Context

ClaraBridge is the Swift MCP bridge in `swift/Sources/ClaraBridge/`. It currently provides macOS
system integrations. This task extends it with three new integration areas. Before implementing,
determine:

- **What ClaraBridge currently exposes**: audit the existing Swift code for what tools are already
  registered, to avoid duplication and understand the patterns to follow.
- **Webex feasibility via local control**: Can Webex (the macOS desktop app) be controlled via
  AppleScript, Accessibility APIs, or macOS scripting? If Webex exposes a scripting dictionary,
  what is its capability? If not, the Webex REST API with a personal bot token or personal access
  token is the fallback. The planning task should test feasibility before committing to an approach.
- **Apple Mail AppleScript scope**: Apple Mail has a well-known AppleScript dictionary. What is
  the required tool surface (read, move, flag, draft, send)? Determine if any actions require
  special macOS permissions (Full Disk Access, Automation) and document the entitlements needed.
- **EventKit permissions**: EventKit requires NSCalendarsUsageDescription and
  NSRemindersUsageDescription. Since ClaraBridge is a command-line Swift binary (not a GUI app),
  confirm that TCC permissions can be granted and will not be blocked by macOS Transparency,
  Consent, and Control policies for CLI tools.

## Context

Clara's priorities HUD, email automation, and Webex triage intents all require macOS-native
access to data sources. The architectural principle is: if capability is available to intents, it
must be delivered through MCP. These Swift MCP tools plug into the standard Clara registry.

## Capability 1: EventKit (Reminders + Calendar)

Extend ClaraBridge with an EventKit-backed MCP tool set:

**Reminders tools:**
- `reminders.list`
- `reminders.get`
- `reminders.create`
- `reminders.complete`
- `reminders.update`
- `reminders.delete`

**Calendar tools:**
- `calendar.events`
- `calendar.event_detail`
- `calendar.create_event`
- `calendar.busy_times`

## Capability 2: Apple Mail

Extend ClaraBridge with AppleScript-bridged mail tools:

**Mail tools:**
- `mail.list_inbox`
- `mail.get_message`
- `mail.move`
- `mail.flag`
- `mail.mark_read`
- `mail.create_draft`
- `mail.send`
- `mail.delete`
- `mail.get_mailboxes`

## Capability 3: Webex

Extend ClaraBridge with Webex tools. **Approach: ClaraBridge via local macOS control first
(AppleScript or Accessibility API). If Webex does not expose a usable scripting dictionary,
fall back to the Webex REST API with a personal access token stored in config/keychain.**

**Target Webex tools:**
- `webex.list_spaces`
- `webex.get_messages`
- `webex.send_message`
- `webex.mark_unread`
- `webex.hide_space` / `webex.unhide_space`
- `webex.get_direct_messages`

## Implementation Notes

- All tools follow the existing ClaraBridge tool registration pattern
- Tools that perform write operations (send, delete, move) must include a confirmation parameter
  or be gated behind the `require_approval` flag in the intent that calls them
- Error messages must be actionable
- The ClaraBridge binary is registered in `config.yaml` as an MCP stdio server

## Acceptance Criteria

- `reminders.list`, `calendar.events`, `mail.list_inbox`, and `mail.get_message` all return correct
  data for known test content
- `reminders.create`, `mail.create_draft` create items visible in the native macOS apps
- Webex: at minimum `webex.list_spaces` and `webex.get_messages` work
- All tools are registered in the Clara registry when ClaraBridge is configured as an MCP server
- Permissions are documented and a helpful error is returned if any permission is missing
