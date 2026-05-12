# Notes Cleanup and Extraction Intent Design

**Goal:** An automated system built into Clara using the `local` LLM model and vector search to synchronize Obsidian note embeddings and periodically propose structural improvements (tags, links, folder routing, and zettelkasten extraction) through interactive notifications.

**Architecture:** A single Starlark intent script (`notes_cleanup.star`) that utilizes `fs.on_change` for real-time embedding updates, a batch sync function for initial bootstrapping, and independent scheduled tasks for each of the five cleanup operations. It leverages SQLite FTS5 and `sqlite-vec` via the Clara `db` integration for fast semantic analysis and routing.

**Tech Stack:** Clara Intents (Starlark), `zk` integration, `db` (sqlite-vec), `llm` (local generation and embeddings), `fs` (change events), `notify` (interactive prompts).

---

## 1. Vector Synchronization

The foundation of the cleanup system requires up-to-date embeddings of note content stored in the SQLite database.

- **Storage:** We will store the `note_path`, `hash` (to prevent re-embedding identical content), and the `embedding` in a dedicated SQLite table (e.g., `zk_embeddings`).
- **Initial Bootstrap / Full Sync (`sync_all_embeddings`):** A task that can be triggered manually. It calls `zk.note_list`, compares existing hashes in the DB, and embeds/upserts any notes that are new or changed. It also deletes rows for notes that no longer exist.
- **Real-time Sync (`on_note_change`):** A task bound to `fs.on_change` watching the Vault directory. When a note is modified, it reads the note, checks if the content actually changed (via hash), generates an embedding using `llm.embed(category="local", ...)`, and updates the database.

## 2. Cleanup Tasks

Each of the following tasks will be implemented as an isolated function within the same `.star` file. They will be registered with separate `clara.task()` schedules to spread out the background load.

### 2a. Link Discovery (`task_discover_links`)
- Iterates through unlinked or lightly-linked notes.
- Uses `db.vec_search` to find highly similar notes.
- Passes the target note and highly similar notes to `llm.generate(category="local")`.
- **Prompt:** Analyze the relationship. If a strong topical connection exists, propose exactly where and how a `[[wikilink]]` should be inserted.
- **Action:** If proposed, sends a `notify.send` with a markdown explanation and `obsidian://open` links to both files, followed by `notify.ask`.
- **Feedback Loop:** If the user provides custom feedback, the request is re-submitted to the LLM with the feedback appended, and the process repeats.

### 2b. Tag Application (`task_propose_tags`)
- Selects recently modified or untagged notes.
- Fetches the global tag list via `zk.tag_list`.
- **Prompt:** Given the note content and the existing tag vocabulary, propose highly relevant tags. Do not force tag creation if none fit well.
- **Action:** If tags are proposed, prompts the user via `notify.ask`. If approved, updates the note via `zk.note_update` (adding tags to the frontmatter).

### 2c. Filepath Routing (`task_route_notes`)
- Targets notes residing in the `Cisco/` and `Notes/` directories.
- Uses `db.vec_search` to find similar notes in `Archive/` or `Brain/` to determine the most likely destination.
- **Prompt:** Analyze the note and the context of similar notes. Decide if this note is ephemeral (should move to `Archive/`), evergreen (should move to `Brain/`), or should be deleted.
- **Action:** Sends a detailed `notify.send` explaining the rationale, then `notify.ask`. On approval, uses `fs` to move the file or `zk.note_delete`.

### 2d. Zettelkasten Extraction (`task_extract_insights`)
- Targets `Journal/` (both current and archived), `Notes/`, and `Brain/` notes.
- **Prompt:** Read the stream-of-consciousness or raw note. Identify distinct, evergreen insights, concepts, or philosophical thoughts. Propose extracting them into a standalone note in `Brain/`. Provide a title and the proposed content for the new note.
- **Action:** `notify.send` with the rationale and the proposed new note content. `notify.ask` to confirm. On approval, uses `zk.note_create` and updates the source note to insert a `[[wikilink]]` pointing to the new extraction.

### 2e. Maintenance: Dead Links & Empty Notes (`task_maintenance`)
- Iterates through `zk.note_list`.
- **Empty Notes:** If `len(content.strip()) == 0`, prompts the user to either fill it or delete it.
- **Dead Links:** Checks all wikilinks in a note using `zk.note_resolve_wikilink`. If the target is empty string, it is dead.
- **Action:** Prompts user via `notify.ask` for dead links (Remove link vs Keep). If remove, `zk.note_update` strips the brackets.

## 3. Safety and Control

- **`DRY_RUN` Constant:** Top of file, defaults to `True`. When true, uses `print()` instead of `zk.note_update` or `fs.move`.
- **Feedback Loop Handling:** For tasks involving LLMs (links, routing, extraction), the `notify.ask` custom text response will trigger a recursive call or loop, allowing the user to say "Use Brain/Personal instead" and have the LLM correct its proposal before taking action.
- **Context Links:** Every `notify.send` preceding an action will include an `obsidian://open?vault=<vault_name>&file=<url_encoded_path>` link to provide immediate context to the user.

---
