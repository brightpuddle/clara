# Overview

I need a terminal-centric "HUD" application to aggregate disparate streams of
information, prioritize what needs my attention, and automate the logistics of
data organization.

# Context

I'm a datacenter automation engineer and software developer. I'm using agentic
AI extensively in all parts of my workflow. The capability and acceleration in
productivity is incredible, but it has also accelerated my output to the point
of information overload. I'm having a hard time keeping up with the velocity of
my own productivity.

# Challenges

- I have todo items in Apple reminders, taskwarrior, todos in notes ('- [ ]' or
  'TODO'), flagged emails.
- I have data spread across mail, markdown notes, Chrome bookmarks, etc. I do
  not have any means means of finding related content or the insight this could
  offer
- When I start working on a task in reminders I have a number of related
  reference material, which is scattered throughout these other sources
- I can just barely keep up with WebEx, messages, and email, even with rules and
  various organizational strategies. This has a very high signal-to-noise ratio.
- I struggle to keep up with organizing my files, bookmarks, terminal windows.
  My file system has a very strong organizational strategy, but project work
  produces temporary files, downloads, new tabs, etc, and these end up in
  Desktop, Downloads, bookmarks, and a project-oriented docs folder. Cleanup
  takes time
- I have a lot of notes that are named with ISO date stamps, that are journal
  entries, a lot more that are loosely Zettelkasten, and then sprawl that needs
  organization. They need orgnaization, aggregation, backlinking, tagging, etc.
  They also relate to items that are outside of notes, e.g. documents,
  bookmarks.
- I'm often working on many projects at the same time with each project in
  Wezterm or GhosTTY tab. I use split panes in each tab to run neovim, copilot,
  shell, etc. The agentic workflow involves typing a prompt, giving it to the
  agent, waiting, and while I wait I shift to other tasks. I'm having to search
  through the tabs, see if copilot is done, remember the context, and do what's
  next. This is a significant cognitive load on top of everything else.

# Solution

I would like to build a self-contained agent, server, and TUI client. The server
handles AI insights through embeddings and LLM queries. This may run on my local
machine or remotely. The agent is the worker process on my local machine and
will handle data access (read/write), background automation tasks, cleanup, will
send content to the server for embeddings or any other analysis requiring LLM.
The TUI is my primary interface and will provide a focused view into what needs
my attention, insights/suggestions, and related context.

# Architecture

- Local-First & Embedded: No Docker, no external Postgres. Use embedded
  databases and local inference (Ollama) to ensure privacy, low latency, and to
  simplify deployment.
- Terminal-First: The primary interface is a TUI. A macOS-native app (SwiftUI)
  may become a later consumer of the same data.
- Dumb UI: The agent is the source of truth for all local data. The client
  connects to the agent for all data. The agent proxies local data (mdfind,
  reminders) to the UI, and handles connectivity to the server for AI insights.
  This allows for multiple UI clients with identical underlying functionality.
- Focused Context: The TUI will provide the ability to accomplish many routine
  tasks, like editing notes, completing tasks, but
- Async & Non-Blocking: High-volume data (logs, emails) is processed in the
  background via local worker queues and the user should not experience delay
- Unified Data Model: Use a single SQLite instance for both relational metadata
  and vector embeddings to simplify relationship mapping.
- Univeral Translation: All data artifacts (including virtual artifacts, such as
  internal data cleanup suggestions) will be represented by a universal language
  and if writable, will be editable through markdown with yaml frontmatter, e.g.
  a todo task from Apple reminders, taskwarrior, a note, the title of a Chrome
  bookmark can all be edited directly through Clara. This is not a replacement
  for native, purpose-built apps, but reduces the frequency of context switching
- Multi-tier triage for categorizing and prioritizing items. This is
  configurable and will learns.
  1. heuristic layer - regex and metadata filters
  2. vector comparison - compare to embedding clusters in the vector db
  3. LLM - if it still can't be categorized, pass it to the LLM for analysis
  4. Learning is driven by user actions: when a user manually archives or
     prioritizes an item in the TUI, the agent captures this as a 'reference
     artifact' to update the vector clusters.
- Reversible: All automated changes (reading email, webex, moving files, adding
  backlinks or tags) should be auditable and reversible until accepted

# Functional Requirements

## 1. Data Janitor

### Organization

- Create embeddings from files in select folders (throttled, as to be CPU
  friendly)
- Monitor select folders using `fsnotify`.
- Send new file metadata to the Server for classification.
- Move files to project-specific folders or a "Transient" folder based on triage
  process
- Create suggestions for backlinks and tags
- Suggestions will either be auto-approved from settings or treated like an
  artifact and presented in the unified artifact view

### Email / Webex

- The agent runs a background worker that integrates with mail and webex (either
  through cloud APIs or through local integration)
- Sort, clean, suggest against mail and webex, with history and undo
- Email will be ingested as reference clouds, e.g. 50-100 examples of different
  classification of email, to perform similarity search against. Reference cloud
  will be built up dynamically from day 1, not from historic emails

## 2. Priority Queue

- Identify actionable items from reminders, notes, flagged email, unread email,
  terminal logs (copilot complete, brew update, etc)
- Triage and prioritize these items
- Provide a TUI view that shows status and priority (status factors into
  priority, i.e. in progress means we cannot act yet)

## 3. Contextual Relationships

- Data is ingested by the agent
- Store embeddings for "artifacts", i.e. files, notes, messages, reminders, etc
  in `sqlite-vec` in the server
- Provide a "Related Context" view in the TUI to shows other artifacts related
  to the current artifact

## Makefile Requirements

- The Makefile must include a `help` target and dev targets using `air` for
  hot-reloading:
  - `make help`: Displays available commands.
  - `make dev-server`: Runs `air` for the server component.
  - `make dev-agent`: Runs `air` for the agent component.
  - `make dev-tui`: Runs the TUI component.
  - `make build`: Compiles all Go binaries.
- Include any additional targets for setup, e.g. download/install the
  `sqlite-vec` `.dylib` or `.so`

# Technology Stack

- Language: Go (1.26+) for Server, Agent, and TUI.
- Database: `ncruces/go-sqlite3` (WASM-based pure Go driver) with the
  `sqlite-vec` extension for vector search.
- TUI Framework: `charmbracelet/bubbletea`, `lipgloss`.
- Errors: cockroachdb/errors
- Logging: zerolog, written to file for debugging
- Inference: Ollama (API-driven) for embeddings and text summarization.
- Communication: gRPC over Unix Domain Sockets for IPC between TUI and agent.
  Standard gRPC from agent to server to allow the server to be moved off-host.
- Desktop Integration: Swift/SwiftUI for macOS-specific features (native client
  app, menu bar).
- Monorepo: unified Go project to provide tight interlock between components

# TUI design

- Master-detail design pattern, modeled on lazygit
- The left pane will provide a unified artifact pane and a related context pane
  - `j`/`k` to navigate within a list; `h`/`l` or `Tab`/`Shift+Tab` to cycle
    between Artifacts and Related Context.
  - Accordion Behavior: When a pane is focused, it should take up ~60/70% of the
    sidebar height, while the others collapse. Based on vertical height, others
    may be collapsed to only a header
  - `/` unified search across all artifacts, narrows search as I type
  - Quick actions:
    - `Space`: Toggle as actioned/done and archives the artifact
    - `Enter`: "Drill down", e.g. a file in `$EDITOR`
    - `o`: open - opens "artifact" in native application, e.g. terminal

# Overview

I need a terminal-centric "HUD" application to aggregate disparate streams of
information, prioritize what needs my attention, and automate the logistics of
data organization.

# Context

I'm a datacenter automation engineer and software developer. I'm using agentic
AI extensively in all parts of my workflow. The capability and acceleration in
productivity is incredible, but it has also accelerated my output to the point
of information overload. I'm having a hard time keeping up with the velocity of
my own productivity.

# Challenges

- I have todo items in Apple reminders, taskwarrior, todos in notes ('- [ ]' or
  'TODO'), flagged emails.
- I have data spread across mail, markdown notes, Chrome bookmarks, etc. I do
  not have any means means of finding related content or the insight this could
  offer
- When I start working on a task in reminders I have a number of related
  reference material, which is scattered throughout these other sources
- I can just barely keep up with WebEx, messages, and email, even with rules and
  various organizational strategies. This has a very high signal-to-noise ratio.
- I struggle to keep up with organizing my files, bookmarks, terminal windows.
  My file system has a very strong organizational strategy, but project work
  produces temporary files, downloads, new tabs, etc, and these end up in
  Desktop, Downloads, bookmarks, and a project-oriented docs folder. Cleanup
  takes time
- I have a lot of notes that are named with ISO date stamps, that are journal
  entries, a lot more that are loosely Zettelkasten, and then sprawl that needs
  organization. They need orgnaization, aggregation, backlinking, tagging, etc.
  They also relate to items that are outside of notes, e.g. documents,
  bookmarks.
- I'm often working on many projects at the same time with each project in
  Wezterm or GhosTTY tab. I use split panes in each tab to run neovim, copilot,
  shell, etc. The agentic workflow involves typing a prompt, giving it to the
  agent, waiting, and while I wait I shift to other tasks. I'm having to search
  through the tabs, see if copilot is done, remember the context, and do what's
  next. This is a significant cognitive load on top of everything else.

# Solution

I would like to build a self-contained agent, server, and TUI client. The server
handles AI insights through embeddings and LLM queries. This may run on my local
machine or remotely. The agent is the worker process on my local machine and
will handle data access (read/write), background automation tasks, cleanup, will
send content to the server for embeddings or any other analysis requiring LLM.
The TUI is my primary interface and will provide a focused view into what needs
my attention, insights/suggestions, and related context.

# Architecture

- Local-First & Embedded: No Docker, no external Postgres. Use embedded
  databases and local inference (Ollama) to ensure privacy, low latency, and to
  simplify deployment.
- Terminal-First: The primary interface is a TUI. A macOS-native app (SwiftUI)
  may become a later consumer of the same data.
- Dumb UI: The agent is the source of truth for all local data. The client
  connects to the agent for all data. The agent proxies local data (mdfind,
  reminders) to the UI, and handles connectivity to the server for AI insights.
  This allows for multiple UI clients with identical underlying functionality.
- Focused Context: The TUI will provide the ability to accomplish many routine
  tasks, like editing notes, completing tasks, but
- Async & Non-Blocking: High-volume data (logs, emails) is processed in the
  background via local worker queues and the user should not experience delay
- Unified Data Model: Use a single SQLite instance for both relational metadata
  and vector embeddings to simplify relationship mapping.
- Univeral Translation: All data artifacts (including virtual artifacts, such as
  internal data cleanup suggestions) will be represented by a universal language
  and if writable, will be editable through markdown with yaml frontmatter, e.g.
  a todo task from Apple reminders, taskwarrior, a note, the title of a Chrome
  bookmark can all be edited directly through Clara. This is not a replacement
  for native, purpose-built apps, but reduces the frequency of context switching
- Multi-tier triage for categorizing and prioritizing items. This is
  configurable and will learns.
  1. heuristic layer - regex and metadata filters
  2. vector comparison - compare to embedding clusters in the vector db
  3. LLM - if it still can't be categorized, pass it to the LLM for analysis
- Reversible: All automated changes (reading email, webex, moving files, adding
  backlinks or tags) should be auditable and reversible until accepted

# Functional Requirements

## 1. Data Janitor

### Organization

- Create embeddings from files in select folders (throttled, as to be CPU
  friendly)
- Monitor select folders using `fsnotify`.
- Send new file metadata to the Server for classification.
- Move files to project-specific folders or a "Transient" folder based on triage
  process
- Create suggestions for backlinks and tags
- Suggestions will either be auto-approved from settings or treated like an
  artifact and presented in the unified artifact view

### Email / Webex

- The agent runs a background worker that integrates with mail and webex (either
  through cloud APIs or through local integration)
- Sort, clean, suggest against mail and webex, with history and undo
- Email will be ingested as reference clouds, e.g. 50-100 examples of different
  classification of email, to perform similarity search against. Reference cloud
  will be built up dynamically from day 1, not from historic emails

## 2. Priority Queue

- Identify actionable items from reminders, notes, flagged email, unread email,
  terminal logs (copilot complete, brew update, etc)
- Triage and prioritize these items
- Provide a TUI view that shows status and priority (status factors into
  priority, i.e. in progress means we cannot act yet)

## 3. Contextual Relationships

- Data is ingested by the agent
- Store embeddings for "artifacts", i.e. files, notes, messages, reminders, etc
  in `sqlite-vec` in the server
- Provide a "Related Context" view in the TUI to shows other artifacts related
  to the current artifact

## Makefile Requirements

The Makefile must include a `help` target and dev targets using `air` for
hot-reloading:

- `make help`: Displays available commands.
- `make dev-server`: Runs `air` for the server component.
- `make dev-agent`: Runs `air` for the agent component.
- `make dev-tui`: Runs the TUI component.
- `make build`: Compiles all Go binaries.

# Technology Stack

- Language: Go (1.26+) for Server, Agent, and TUI.
- Database: `ncruces/go-sqlite3` (WASM-based pure Go driver) with the
  `sqlite-vec` extension for vector search.
- TUI Framework: `charmbracelet/bubbletea`, `lipgloss`.
- Errors: cockroachdb/errors
- Logging: zerolog, written to file for debugging
- Inference: Ollama (API-driven) for embeddings and text summarization.
- Communication: gRPC over Unix Domain Sockets for IPC between TUI and agent.
  Standard gRPC from agent to server to allow the server to be moved off-host.
- Desktop Integration: Swift/SwiftUI for macOS-specific features (native client
  app, menu bar).
- Monorepo: unified Go project to provide tight interlock between components

# TUI design

- Master-detail design pattern, modeled on lazygit
- The left pane will provide a unified artifact pane and a related context pane
  - `j`/`k` to navigate within a list; `h`/`l` or `Tab`/`Shift+Tab` to cycle
    between Artifacts and Related Context.
  - Accordion Behavior: When a pane is focused, it should take up ~60/70% of the
    sidebar height, while the others collapse. Based on vertical height, others
    may be collapsed to only a header
  - `/` unified search across all artifacts, narrows search as I type
  - Quick actions:
    - `Space`: Toggle as actioned/done and archives the artifact
    - `Enter`: "Drill down", e.g. a file in `$EDITOR`
    - `o`: open - opens "artifact" in native application, e.g. terminal pane,
      Obsidian, Mail, Reminders. For terminal artifacts, use the terminal's CLI
      (e.g. `wezterm cli activate-pane`) to switch focus to the specific
      pane/tab where the process is running.
    - `s`: search within view, e.g. if viewing related artifacts, search only
      within related artifacts
  - Items should be prioritized with an actionability or heat score, e.g. a
    reminder due in 5 minutes and an error log from an active terminal process
    both have high "heat" so they both float to the top regardless of type
  - Visual differentiation with nerd font icons and color coding. Color should
    indicate type and actionable/due/active (needs attention) and icon should
    indicate the artifact type
- The right pane will show preview and/or metadata (detail view)

## Phase 1: MVP (The Foundation)

1. Skeleton: Setup the monorepo structure as per this design.
2. Basic server: create the server with `ncruces/go-sqlite3`, `sqlite-vec`,
   gRPC, and ollama connectivity.
3. Basic Agent: create background daemon that watches provided directories and
   incomplete reminders and creates a DB entry and embedding for every new item
4. Apple native worker: A swift worker process (daemon) that provides the agent
   with efficient, native access to local Apple APIs, e.g. `EventStore` for
   reminders and `FileManager` for spotlight index search.
5. Agent should provide highly performant, efficient, async access to local
   artifacts (files, reminders, logs)
6. Priority TUI: A Bubbletea interface that displays a browsable, searchable,
   prioritized list of items as per the TUI description in this prompt ,
   Obsidian, Mail, Reminders - `s`: search within view, e.g. if viewing related
   artifacts, search only within related artifacts - Items should be prioritized
   with an actionability or heat score, e.g. a reminder due in 5 minutes and an
   error log from an active terminal process both have high "heat" so they both
   float to the top regardless of type - Visual differentiation with nerd font
   icons and color coding. Color should indicate type and actionable/due/active
   (needs attention) and icon should indicate the artifact type

- The right pane will show preview and/or metadata (detail view)

## Phase 1: MVP (The Foundation)

1. Skeleton: Setup the monorepo structure as per this design.
2. Basic server: create the server with `ncruces/go-sqlite3`, `sqlite-vec`,
   gRPC, and ollama connectivity.
3. Basic Agent: create background daemon that watches provided directories and
   incomplete reminders and creates a DB entry and embedding for every new item
4. Apple native worker: A swift worker process (daemon) that provides the agent
   with efficient, native access to local Apple APIs, e.g. `EventStore` for
   reminders and `FileManager` for spotlight index search.
5. Agent should provide highly performant, efficient, async access to local
   artifacts (files, reminders, logs)
6. Priority TUI: A Bubbletea interface that displays a browsable, searchable,
   prioritized list of items as per the TUI description in this prompt
