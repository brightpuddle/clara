---
plan_recommended: true
---

# Alex's Clara Web UI

## Planning Context

Alex is non-technical and will not regularly work in a terminal. The Facebook
Marketplace approval workflows (task 040, and Messenger in task 050) require her
to review photos and make decisions. A lightweight local web interface is the
chosen solution. This planning task should address:

- **Architecture**: A Go HTTP server (using Echo framework) embedded in Clara,
  serving a local web app. The web server communicates with Clara's daemon the
  same way the TUI does - via the IPC control socket or by connecting as a
  dynamic MCP peer. Which connection model is appropriate?
- **Serving images**: How are photos (stored locally in Alex's filesystem)
  served to the browser? The web server needs to serve image files from
  configurable local directories.
- **Authentication**: This server runs on localhost and is only accessible to
  Alex on her own machine. A simple localhost-only bind with no auth is likely
  sufficient. Confirm.
- **Frontend framework choices**: Echo (backend) + templ (Go HTML templates) +
  htmx (interactivity)
  - Tailwind CSS + DaisyUI (components). This is a simple approval workflow UI,
    not a complex SPA. Is a full Svelte build necessary, or is htmx + templ
    sufficient for this use case?
- **Scope**: Is this Alex-only, or does it become a general "Clara Web"
  interface? For now, treat it as Alex-specific, but design it so it could be
  extended.
- **Auto-start**: The web server should start automatically as part of
  `clara serve`. Configure via `config.yaml` (enabled: true/false, port: 8080).

## Context

Alex uses an M2 MacBook (8GB RAM). She is not comfortable in the terminal. The
TUI is not appropriate for her primary workflow. A web interface accessible in
her browser (Chrome) is the right solution. Key requirements:

- Accessible at `http://localhost:8080` (or configurable port) in her browser
- Shows photos inline - this is critical for marketplace approval decisions
- Language must be simple, clear, and non-technical
- Notification-driven: when Clara has something for Alex to review, it surfaces
  to the top of the page or sends a macOS notification directing her to the
  browser

## Required Capabilities (for FB Marketplace)

### Renewal Approval Queue

Displays AI-recommended renewals/re-lists from the renewals intent (task 040):

- Photo(s) of the listing displayed prominently
- Title and current price
- AI justification for why renewal is recommended (weather, season, day of week,
  etc.)
- **Renew** (1) / **Skip** (2) / **Comment** (3) buttons, matching the keyboard
  shortcuts described in the renewals task
- Comment mode: a text input field where Alex can type feedback (e.g., "don't
  show swimsuits, the box is hard to get to"). The comment is processed
  immediately - triggers a re-run of the recommendation logic with the comment
  as high-priority context. Shows a loading indicator while processing and
  updates the queue.

### General Notification Feed

A simple feed of items from Clara that need her attention (beyond just FB).
Initially FB-focused, but structured to accept any `notify_send_interactive`
calls from Clara's MCP notification system.

## Technical Design

**Backend:** Go, using the Echo framework (or standard `net/http` if simpler).
The web server is a component started by `clara serve` when enabled in config.

**Frontend:** Server-rendered HTML via `templ` (Go HTML templating), htmx and
alpine.js for interactive updates (polling or SSE for queue updates, form
submissions), Tailwind CSS + DaisyUI for styling. Keep the JS footprint
minimal - avoid a full SPA build unless needed.

**Image serving:** A dedicated `/images/` route in the web server that serves
files from configured local paths (e.g.,
`~/Library/Application Support/clara/fb-photos/`). Paths are validated against
an allowlist to prevent directory traversal.

**State:** The web UI reads state from Clara's SQLite DB (via the DB MCP server
or direct read). Approvals write state back via MCP tool calls (same as TUI).

**Auto-launch:** Clara's serve command starts the web server as a goroutine when
`web.enabled: true` in config. A macOS notification (via the notification MCP
tool) is sent when there are new items in the queue.

## Config

```yaml
web:
  enabled: true
  port: 8080
  image_serve_paths:
    - "~/Library/Application Support/clara/fb-photos"
```

## Acceptance Criteria

- Alex can open `http://localhost:8080` in Chrome and see pending FB listing
  drafts with photos
- The renewal queue shows photos, titles, and AI justification, with 1/2/3
  decision buttons
- Comment submission updates the recommendation queue in real-time (loading
  indicator shown)
- The web server starts automatically with `clara serve` when enabled in config
- No authentication required (localhost only); document this clearly in the
  config
