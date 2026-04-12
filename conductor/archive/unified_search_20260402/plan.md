# Implementation Plan: Unified Search & Indexing Strategy
## Phase 1: Foundation & Shared Logic [checkpoint: 0c787d9]
- [x] Task: Create internal SQLite indexing library for FTS5 with external content table support. 0bf623b
    - [x] Write tests for SQLite schema creation and FTS5 indexing. 0bf623b
    - [x] Implement Go library for shared SQLite indexing logic. 0bf623b
- [x] Task: Define shared search interfaces for MCP servers. 0bf623b
    - [x] Write tests for search tool registration and response formatting. 0bf623b
    - [x] Implement common search structures and interfaces in Go. 0bf623b
- [x] Task: Conductor - User Manual Verification 'Phase 1: Foundation & Shared Logic' (Protocol in workflow.md) 0bf623b

## Phase 2: General File Search (fs.search) [checkpoint: bd4b49c]
- [x] Task: Implement `fs.search` tool using `mdfind`. bd4b49c
    - [x] Write tests for `mdfind` command generation and output parsing. bd4b49c
    - [x] Implement `fs.search` in the `fs` MCP server. bd4b49c
    - [x] Support `-onlyin` argument in the tool. bd4b49c
- [x] Task: Conductor - User Manual Verification 'Phase 2: General File Search (fs.search)' (Protocol in workflow.md) bd4b49c

## Phase 3: ZK Search Enhancement (zk.search) [checkpoint: 91974fe]
- [x] Task: Integrate SQLite FTS5 indexing into the `zk` MCP server. 91974fe
    - [x] Write tests for ZK markdown parsing and indexing (tags, backlinks). 91974fe
    - [x] Implement background indexing task for the ZK vault. 91974fe
    - [x] Update `zk.search` to use the FTS5 index. 91974fe
- [x] Task: Conductor - User Manual Verification 'Phase 3: ZK Search Enhancement (zk.search)' (Protocol in workflow.md) 91974fe

## Phase 4: Native Mail Search (mail.search) [checkpoint: 0bb34c1]
- [x] Task: Create new `search` MCP server in Go. 0bb34c1
    - [x] Write tests for MCP server initialization and tool registration. 0bb34c1
    - [x] Implement basic MCP server boilerplate in Go. 0bb34c1
- [x] Task: Implement Mail indexing in the `search` MCP server. 0bb34c1
    - [x] Write tests for `~/Library/Mail` directory traversal and email parsing (EML/MBOX). 0bb34c1
    - [x] Write tests for mail header extraction (`Message-Id`, `In-Reply-To`, etc.). 0bb34c1
    - [x] Implement mail indexing logic into SQLite. 0bb34c1
- [x] Task: Implement `mail.search` tool in the `search` MCP server. 0bb34c1
    - [x] Write tests for FTS5 queries against mail content and headers. 0bb34c1
    - [x] Implement the `mail.search` tool. 0bb34c1
- [x] Task: Conductor - User Manual Verification 'Phase 4: Native Mail Search (mail.search)' (Protocol in workflow.md) 0bb34c1
## Phase 5: Unification & Final Polish [checkpoint: 06df6fe]
- [x] Task: Verify unified tool registration across all search tools. 06df6fe
    - [x] Write integration tests for `mail.search`, `fs.search`, and `zk.search` tool calls. 06df6fe
    - [x] Ensure correct namespacing in the tool catalog. 06df6fe
- [x] Task: Conductor - User Manual Verification 'Phase 5: Unification & Final Polish' (Protocol in workflow.md) 06df6fe
