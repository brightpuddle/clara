---
plan_recommended: false
---

# Facebook Marketplace - Renewal and Re-list Intent

## Context

Alex has approximately 250-300 active Facebook Marketplace listings at any time. Ads age and
disappear from buyers' feeds. Facebook allows renewing a listing a few times (~3) and then
requires delete-and-relist to bring it back to the top. Alex currently manually scrolls through
hundreds of listings to decide which to renew based on season, weather, upcoming weekend, and
other factors.

This intent automates the decision-making and execution of renewals, while keeping Alex in the
loop for approvals.

**Dependencies:**
- `010-chrome-mcp-extension.md` - Chrome MCP server must be operational
- `030-alex-web-ui.md` - Web UI must be running for the approval queue
- Ollama running on the M4 Mac Mini (via local network) for AI evaluation
- Weather data: a free weather API (e.g., Open-Meteo, which requires no API key) for the 10-day
  forecast

## Workflow

### 1. Data Collection (scheduled, daily)

The intent runs on a configurable schedule (default: daily, configurable time, e.g. 8am).

**Gather contextual factors:**
- 10-day weather forecast for Alex's location via Open-Meteo API (HTTP call from Starlark)
- Day of week, upcoming weekend, holidays from calendar or computed dates
- Any high-priority context Alex has provided through the comment workflow (stored in SQLite)

**Read current active listings:**
The Chrome MCP server navigates to Alex's Marketplace selling page and reads all active listings.
For each listing, record:
- Listing ID (Facebook internal), title, current price
- Photos (thumbnail URLs or references)
- Listing age, renewal status (can renew / can relist / already renewed X times)
- Category/type (derived from title if not directly available)

Store this snapshot in SQLite (`fb_listings_snapshot` table). New snapshots replace old ones;
keep the last 7 days for trend analysis.

### 2. AI Evaluation (Ollama on Mac Mini)

Send the listing data + contextual factors to Ollama on the Mac Mini via HTTP (direct Ollama API
call from Starlark, using the mac mini's local network address). This avoids consuming Gemini free
tier for high-volume batch requests.

**Batching:** The Gemini context window is large enough to evaluate all ~250 listings in a single
prompt if needed, but Ollama with a capable model (e.g., qwen2.5 or llama3) can handle reasonable
batches. Batch size is configurable; default is to send all listings in one request with the full
context.

The prompt (stored in `~/.config/clara/fb-prompts/renewal-criteria.md`) describes the renewal
decision criteria in natural language, e.g.:
- People shop more on Friday and Saturday
- Hot weather -> swimsuits, shorts, tank tops; Cold weather -> long sleeves, coats, boots
- December -> Christmas items; Back-to-school season -> school uniforms, backpacks
- Alex's specific overrides from comment history (e.g., "no swimsuits")

AI responds with a JSON list: `[{"listing_id": "...", "action": "renew"|"relist"|"skip",
"confidence": 0.0-1.0, "reason": "..."}]`

### 3. Build Approval Queue

For listings where `action != "skip"` and `confidence >= threshold` (configurable, default 0.7):
- Store in `fb_renewal_queue` table with status `pending_approval`
- Include photo reference, title, recommended action, AI justification

### 4. Alex's Approval Workflow (Web UI)

The web UI (task 030) renders the renewal queue. For each recommended item Alex sees:
- The listing photo(s) - prominently displayed
- Title and current price
- Recommended action (Renew or Relist) and AI justification
- Three buttons: **Renew/Relist (1)** / **Skip (2)** / **Comment (3)**

**Comment/Feedback mode (IMPORTANT):**
When Alex presses Comment (3) and types feedback (e.g., "don't show me any more swimsuits,
the box where swimsuits are is too hard to get to"):
1. Her comment is stored in SQLite as high-priority context
2. The web UI shows a loading indicator
3. The intent is immediately re-triggered with the comment as additional high-priority context
   injected into the renewal criteria prompt
4. The queue is updated in real-time to remove all affected listings
5. Alex does not wait until tomorrow for her feedback to take effect

This feedback is persistent: it accumulates in the `fb_user_context` table and is injected into
every future evaluation run until explicitly cleared.

### 5. Execution (Chrome MCP)

Once Alex approves items, the Chrome MCP server processes them:
- For each approved item: navigate to the listing, click Renew or Relist as appropriate
- Anti-detection: randomized delays between each action (2-8 seconds); process no more than
  20 listings per session with multi-minute breaks; randomize order
- If a listing's renew button is not available (already at renewal limit), automatically treat as
  relist
- After execution, update `fb_renewal_queue` record to status `completed`
- Notify Alex via macOS notification or web UI update when done

## Data Model (SQLite)

```sql
CREATE TABLE fb_listings_snapshot (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    listing_id  TEXT NOT NULL,
    title       TEXT NOT NULL,
    price       REAL,
    photo_urls  TEXT,
    age_days    INTEGER,
    renew_count INTEGER,
    can_renew   BOOLEAN,
    can_relist  BOOLEAN,
    snapshot_date DATE NOT NULL
);

CREATE TABLE fb_renewal_queue (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    listing_id  TEXT NOT NULL,
    title       TEXT NOT NULL,
    photo_urls  TEXT,
    action      TEXT NOT NULL,
    confidence  REAL,
    reason      TEXT,
    status      TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

CREATE TABLE fb_user_context (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    comment     TEXT NOT NULL,
    source      TEXT,
    active      BOOLEAN DEFAULT TRUE,
    created_at  DATETIME NOT NULL
);
```

## Prompt File Layout

```
~/.config/clara/fb-prompts/
  renewal-criteria.md
  renewal-system.md
```

## Acceptance Criteria

- The intent runs on schedule and reads all active listings from Facebook via Chrome MCP
- Weather forecast and day-of-week data are gathered and injected into the AI prompt
- AI produces a recommendation list; items above confidence threshold appear in Alex's approval queue
- Alex can approve, skip, or comment on each item in the web UI
- Comment feedback immediately re-triggers evaluation and updates the queue without waiting for the
  next scheduled run
- Approved items are acted upon in Facebook (renew/relist) with randomized, human-paced delays
- Alex's accumulated comments persist across runs and are included in future evaluations
- The renewal criteria prompt file is watched for changes; updates take effect on the next run
  without restarting Clara
