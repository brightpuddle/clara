---
plan_recommended: false
---

# Facebook Marketplace - New Ad Creation Intent

## Context

Alex sells children's clothing, shoes, and household items on Facebook
Marketplace. She currently has approximately 250-300 active listings and creates
new ones by manually photographing items, researching price/category, and
filling out the Facebook listing form. This intent automates everything after
the photo is taken.

This prototype is laptop-first and account-specific: it will be tested entirely
on Alex's laptop, in Alex's Facebook account, using the installed Chrome
extension and Facebook Marketplace webpage. The intent should automatically
create Facebook Marketplace drafts with no Clara-side approval gate. Alex will
review, edit, and post those drafts manually from the Facebook website.

**Priority: HIGH - target working prototype within 1-2 days.**

**Dependencies:**

- `010-chrome-mcp-extension.md` must be complete (Chrome MCP server operational)
- Clara must be installed and running on Alex's M2 MacBook
- Alex has access to Facebook Marketplace in Chrome
- A configurable local photo inbox source is available on Alex's Mac
- Gemini access must be available either through an existing Clara LLM tool or a
  new Gemini-capable MCP integration built as part of this task

## Workflow

### 1. Photo Detection

Alex's workflow starts on her iPhone: she photographs the item and moves the
photo into a "Marketplace Inbox" album in the Photos app.

ClaraBridge should monitor that Photos album through PhotoKit and expose the
album contents to Clara. When a new photo appears:

- Record the photo path in Clara's SQLite DB (table: `fb_inbox_photos`) with
  status `pending`
- Export the photo into a Clara working directory (for example
  `~/.local/share/clara/fb-photos/pending/`) so the workflow has a stable local
  path to process
- Remove the exported asset from the Photos inbox album so the inbox is cleared
- Photos of the same item can be grouped: if multiple photos arrive within a
  short window (configurable, default 2 minutes), treat them as a single listing

### 2. Item Research via Gemini

For each pending photo batch, use Gemini vision to:

- Identify the item (brand, type, size/dimensions if visible, condition)
- Estimate retail value and suggested resale price (Alex's pricing model:
  significant discount below retail, e.g. a $50-70 shirt retails -> $10-20
  listing price)
- Suggest a listing title that includes key search terms (brand, size, type)
- Suggest a listing description: concise, including all important details
  upfront (size, condition, brand) to reduce "is this available" / "what size"
  messages
- Suggest a category (Facebook Marketplace category list)
- Suggest a condition selection

**Prompt files:** The prompts sent to Gemini must be stored in markdown files in
a configurable directory (e.g., `~/.config/clara/fb-prompts/`), NOT hardcoded in
the intent. The intent watches these files for changes. This lets Alex (or you
on her behalf) tune the language, pricing guidance, and any category-specific
rules over time without editing code. Write sensible default prompt templates to
this folder when the directory is empty.

**API call:** Do not assume a built-in `http` MCP tool exists. Use an existing
Gemini-capable Clara tool if one is already available in Alex's install (for
example `llm.generate_vision`), otherwise build the minimal Gemini-capable MCP
integration needed for this task first and wire it into Clara's active config.
If a new MCP server or config entry is added, update
`~/.config/clara/config.yaml` (or Alex's active Clara config file) and restart
Clara before end-to-end validation. This is still a prototype; it does not need
to wait for the fuller multi-provider design in task 070.

### 3. Draft Creation via Chrome MCP

The Chrome MCP server navigates to Facebook Marketplace, opens the "Create new
listing" flow, uploads the photos, and fills in all fields. The automation must
stop after saving a draft; it must not publish the listing.

- `https://www.facebook.com/marketplace/create/item`
- Fill in Title, Price, Category (drop down), Condition (drop down), and
  Description
- Click "Next"
- Check "Door Pickup" checkbox
- Select "Save draft"

There is no Clara approval step for this task. Alex reviews the saved drafts
directly on the Facebook webpage and decides whether to edit or post them.

Anti-detection rules (enforced by the Chrome MCP server):

- Randomized delay between live Facebook actions, using a conservative default
  of 5-10 seconds during laptop testing against Alex's real Facebook account
- Throttle new listings: no more than ~5 new listings per session, with
  multi-minute gaps between drafts
- If a CAPTCHA or unusual challenge appears, pause automation, stop creating
  drafts, and mark the item for manual review

After successfully saving the draft, the `fb_listing` record is updated to
status `draft_created` and the photo is moved to an archive directory.

## Implementation Notes

- Phase 1 is acceptable if needed: first build and validate Gemini interaction,
  then wire it into the full photo-to-draft workflow
- SQLite records in this task are for internal tracking, observability, retry,
  and auditability; they are not a human approval queue
- No Clara web UI work is required for this task's approval flow

## Data Model (SQLite)

```sql
CREATE TABLE fb_inbox_photos (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path        TEXT NOT NULL,
    group_key   TEXT,
    status      TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);

CREATE TABLE fb_listing (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_paths TEXT NOT NULL,
    title       TEXT NOT NULL,
    description TEXT NOT NULL,
    price       REAL NOT NULL,
    category    TEXT NOT NULL,
    condition   TEXT NOT NULL,
    status      TEXT NOT NULL,
    gemini_raw  TEXT,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
);
```

## Prompt File Layout

```
~/.config/clara/fb-prompts/
  new-ad-system.md
  new-ad-user.md
  categories.md
```

## Acceptance Criteria

- New photos appearing in the watched Photos album are detected within 60
  seconds
- Gemini correctly identifies item, suggests plausible title, description, and
  price for at least 80% of clothing/shoe items in testing
- Ad details are stored in SQLite
- The Chrome MCP server creates a saved draft in Facebook Marketplace end-to-end
  without publishing the listing
- All live Facebook interactions use randomized 5-10 second delays; batch draft
  creation is throttled
- Prompt markdown files are watched for changes; updating the prompt file
  affects the next run without restarting Clara
- If Gemini support is not already present, the task includes the minimum MCP
  and config work needed to make Gemini vision calls from Clara on Alex's laptop
