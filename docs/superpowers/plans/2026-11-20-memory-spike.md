# Plan: Unified Memory Spike (Track H)

Source of truth for task status. Process: `AGENTS.md` "Development Process".
Roadmap: `docs/ROADMAP.md` (Track H; runs after Track I1 for the run source).
Legend: `[ ]` todo, `[~]` in progress, `[x]` done (short SHA).

## Question this spike answers

Can one small MCP surface (`memory.search`, `memory.get`, optionally `memory.remember`)
give any agent (opencode, a Bun script, Webex/Discord router) better context than it gets
by guessing among `zk.*`, audit, and grep, at a cost low enough to keep?

Non-goals: a UI, a new storage engine, indexing every source, replacing opencode's own
project context, or cross-host sync in the first pass.

## Existing pieces to reuse (do not rebuild)

| Need | Already there |
|---|---|
| Keyword index | `internal/search/fts5.go` (`Indexer`: Index/Search/Delete, tx support) |
| Vector index | `sqlite-vec` loaded in `internal/store/store.go` (`vec0`, `db.vec_search`) |
| Embeddings | `llm.embed` tool in `cmd/integrations/llm` (Ollama or Gemini) |
| Notes | `zk` plugin (`ListNotes`, `GetNote`, `SearchNotes`) |
| Run history | `trigger_runs`, `tool_calls` in `internal/store/audit.go` |
| Triggers on change | declarative fsnotify watch paths (`cbda1d61`), event bus |
| Docs | `docs/adr/`, `docs/GLOSSARY.md`, `docs/superpowers/` |

## Design (keep small)

Tools, hosted as a builtin (`internal/builtin/memory/`), so `laptop.*` mounting gives
Sophia the same surface:

- `memory.search {query, sources?, since?, limit=8}` returns stubs only:
  `{uri, source, title, ts, snippet, score}`. Hard cap on snippet length.
- `memory.get {uri}` returns the full body for one stub. Progressive disclosure keeps
  context small: agents search first, fetch only what they choose.
- `memory.remember {text, tags?, source_uri?}` (phase 3, only if reads prove useful).

URI scheme: `zk://<note>`, `adr://0008`, `run://<run_id>`, `doc://<path>`, `mem://<id>`.
Each source implements one interface: `List(since) -> []Doc`, `Get(uri) -> Doc`.

Ranking: FTS5 (bm25) and vec0 cosine, merged with reciprocal rank fusion. Exact
identifiers (error codes, ticket ids, repo names) must hit through FTS even when the
vector side misses.

Storage: separate `memory.db` (not `clara.db`) so it can be rebuilt or deleted freely. It
is a derived index. Source of truth stays in the vault, repo, and audit store.

## Phases

### H0 Decide and baseline (half a day)
- [ ] Write 15-20 real questions you have actually asked an agent or yourself in the last
      month ("what did we decide about X", "why did mail triage fail on Tuesday", "where
      is the IMS catch-up logic"). Save as `testdata/memory/questions.yaml` with the
      expected source URI(s). This is the eval set; without it the spike cannot fail.
- [ ] Baseline: answer each with today's tools only (zk search, grep, audit). Note hit
      rate and rough token cost.
- [ ] Check what opencode already gives a session (project docs, skills, compaction
      memory). Remove questions it already answers well.
- Gate: if baseline hit rate is already high, stop and record an ADR saying so.

### H1 Keyword-only memory over two sources (1-2 days)
- [ ] Source interface and URI parsing, with tests.
- [ ] `zk` source and `doc` source (ADRs, GLOSSARY, plans, specs).
- [ ] Index into FTS5 via `internal/search`; chunk by heading, not by fixed size.
- [ ] `memory.search` / `memory.get` builtin tools; `clara tool call memory.search` works.
- [ ] Reindex on fsnotify trigger for the vault and `docs/`; full rebuild command
      `clara memory reindex`.
- [ ] Run eval set; record hit@5 and tokens returned vs baseline.
- Daily-driver check: add `memory.search` to your opencode MCP config and use it for a week.

### H2 Add vectors and run history (1-2 days)
- [ ] Embed chunks via `llm.embed`; store in `vec0`; cache by content hash so reindex is cheap.
- [ ] Hybrid ranking with reciprocal rank fusion; compare to H1 on the eval set.
- [ ] `run` source: failed or notable `trigger_runs` (status, trigger id, stderr tail,
      linked tool errors). Skip successful routine runs.
- [ ] Decide embedding model and record the cost/latency (local Ollama preferred).
- Gate: vectors must beat H1 hit@5 by a margin you can feel, or they are dropped.

### H3 Write-back and federation (only if H1/H2 pay off)
- [ ] `memory.remember` with tags; stored as `mem://` docs, plain markdown mirrored into
      the vault so the human can read and edit them.
- [ ] Federated live pull for Discord/Webex threads (do not duplicate locally unless needed).
- [ ] Sophia exposure via the existing `laptop.*` mount.

## Risks

- **Stale or noisy memory is worse than none.** Mitigation: every stub carries `ts` and
  source; `remember` entries are human-readable and deletable; indexing is derived and
  rebuildable.
- **Confidential content.** Cisco-proprietary vault and Webex content must stay local.
  Embeddings must use local Ollama, not an external API, for these sources. Record this in
  an ADR before H2.
- **Overlap with opencode.** Covered by the H0 gate.
- **Scope creep into a knowledge platform.** Three tools, two to four sources, one DB.

## Success criteria

- hit@5 on the eval set clearly above the baseline.
- Typical `search` response under about 1.5k tokens; `get` only on demand.
- You reach for it unprompted during a week of normal work.
