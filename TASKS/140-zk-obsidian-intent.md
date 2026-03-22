---
plan_recommended: false
---

# Obsidian / ZK Vault Enhancement Intent

## Context

My knowledge base is in Obsidian using a zettelkasten structure. The vault is rich with cross-
references that don't exist yet: unlinked mentions, related concepts, and inconsistent tags.

**Dependencies:**
- ZK MCP server
- LLM MCP server (task 070)
- ZK vault path configured in `config.yaml`

## Workflow

### 1. Backlink Discovery

Build an embedding index of notes. For each note:
- Find semantically similar notes
- Check whether they're already linked
- Auto-apply high-confidence backlinks
- Queue lower-confidence suggestions for review

### 2. Tag Standardization

Analyze tags, identify clusters that mean the same thing, propose canonical tags, and surface batch
updates for approval.

### 3. Quick Note Aggregation

Find short, old fleeting notes. Group related ones, suggest merges with AI-drafted synthesis, and
archive originals instead of deleting them.

### 4. File Organization

Identify notes that appear to be in the wrong folder and propose moves.

### 5. Content Improvement Suggestions

Check for broken links, suggest related notes, and surface relevant notes based on recent activity.

## Acceptance Criteria

- Backlinks are added where titles appear verbatim
- Lower-confidence suggestions appear in the TUI
- Tag clusters are identified and presented for review
- The embedding index is incremental, not rebuilt every run
- No note content is modified without approval except high-confidence backlink additions
- All changes are logged with enough data for reversal
