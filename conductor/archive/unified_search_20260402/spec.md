# Specification: Unified Search & Indexing Strategy (V2)

## Overview
This track aims to modernize and unify search capabilities across the Clara ecosystem. Current search implementations (Applescript for mail, simple string matching for ZK) are either too slow or lack advanced features. We will implement a high-performance search system using `mdfind` for general file search and SQLite FTS5 for indexing application-specific content (Mail, ZK).

## Functional Requirements
- **General File Search**:
  - Implement an `fs.search` tool (within the `fs` MCP server) that wraps `mdfind`.
  - Support `-onlyin` scoping to limit search depth/breadth.
  - Bypass `~/Library/Mail` and other restricted directories as per macOS privacy policy (using native `mdfind` behavior).
- **Mail Search (Go-native)**:
  - Implement a `mail.search` tool within a **NEW** `search` MCP server.
  - Index `~/Library/Mail` content into SQLite using FTS5.
  - Parse and index headers like `Message-Id`, `In-Reply-To`, and `X-Universally-Unique-Identifier` to support threading and advanced queries.
  - Index content using external content tables in SQLite for disk efficiency.
- **ZK (Zettelkasten) Search**:
  - Enhance the existing `zk` MCP server with SQLite FTS5 indexing.
  - Index markdown files from the configured vault directory.
  - Support ZK-specific intelligence: tags, backlinks, and potentially frontmatter.
- **Architecture**:
  - Use the new namespace-aware tool registration (e.g., `mail.search`, `fs.search`, `zk.search`) to provide a unified tool interface.
  - The `search` MCP server (Go) provides the `mail.search` tool, while the `fs` and `zk` servers provide their own search tools.
  - Use distributed SQLite databases for each index (one for mail, one for zk) to maintain isolation and simplify management.

## Non-Functional Requirements
- **Performance**: Search results must be returned in sub-second time for typical queries.
- **Storage Efficiency**: Use SQLite's FTS5 external content tables to minimize index size.
- **Language**: All new search logic must be implemented in Go.

## Acceptance Criteria
- `fs.search` successfully uses `mdfind` to locate files outside restricted directories.
- `mail.search` (via the new search MCP) indexes a subset of the user's mail directory and returns results with header metadata.
- `zk.search` returns results from the ZK vault with support for tag/backlink filtering.
- All tools are correctly registered and accessible through their respective namespaces (e.g., `mail`, `fs`, `zk`).

## Out of Scope
- Real-time indexing (file watchers) - indexing will be triggered manually or on a schedule for the first iteration.
- Complex NLP-based semantic search.
- Indexing content within binary files (PDFs, Docx, etc.) beyond what `mdfind` provides.
