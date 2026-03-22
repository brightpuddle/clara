---
plan_recommended: false
---

# Alex's Filesystem Organization Intent

## Context

Alex's filesystem is disorganized across Downloads, Documents, Desktop, Pictures, and Gmail. She
has several competing folder structures and important things are hard to find.

This intent organizes her filesystem progressively and non-destructively, using AI to understand
her organizational intent and consolidate duplicated structures.

**Dependencies:**
- FS MCP server
- LLM MCP server
- Alex's web UI (task 030)

## Key Constraint

Alex must be able to find important things. When in doubt, consolidate rather than delete. Never
delete without explicit approval shown with previews.

## Workflow

### Phase 0: Structure Discovery and Rule Generation

Analyze Alex's filesystem, identify patterns and duplicate structures, and generate
`~/.config/clara/alex-filesystem-rules.md`. Present the proposed consolidated structure in the web UI.

### Phase 2: Auto-Organization (low risk items)

Apply clear rules to low-risk files such as screenshots, receipts, and dated photos.

### Phase 3: Ambiguous Files - Web UI Review

Present unclear files in small visual batches with simple language and obvious options.

### Phase 4: Duplicate Detection

Find likely duplicate files and photos, show side-by-side previews, and never auto-delete.

### Phase 5: Email Photo Recovery

Find image attachments Alex emailed herself and offer to save them into a local folder.

## Acceptance Criteria

- First run produces a sensible consolidated structure proposal shown in the web UI
- Alex can approve or reject each proposal with simple buttons
- Auto-organized files are moved and logged
- Duplicate photo detection works with side-by-side comparison
- No file is permanently deleted without explicit approval
- Email photo attachments can be saved to a local folder from the web UI
