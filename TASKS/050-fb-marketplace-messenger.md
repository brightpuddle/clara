---
plan_recommended: false
---

# Facebook Marketplace - Messenger Auto-Reply Intent

## Context

Facebook provides buyers with suggested one-click questions ("Is this still available?",
"What size is this?") that result in a flood of easily-answerable messages for sellers. Alex
currently has to manually respond to all of these. This intent monitors her Messenger inbox for
new marketplace-related messages and handles the simple ones automatically, while routing complex
or personal conversations to her.

**Dependencies:**
- `010-chrome-mcp-extension.md` - Chrome MCP for browser automation of messenger.com
- `030-alex-web-ui.md` - For surfacing low-confidence messages requiring her attention
- Ollama on the Mac Mini for message evaluation (avoids Gemini free tier)

## Workflow

### 1. Monitor Messenger

The intent runs on a configurable polling schedule (default: every 5-10 minutes with randomized
jitter). The Chrome MCP server navigates to `messenger.com` (or the Facebook Marketplace inbox
tab) and reads new/unread message threads.

For each unread thread:
- Read the conversation (last few messages for context)
- Identify if the conversation is related to a specific Marketplace listing (usually linked from
  the listing)
- Fetch the relevant listing details from the `fb_listings_snapshot` table (or re-read from the
  browser if needed) to use as context for the AI

Store the thread in `fb_messenger_threads` table with status `pending_review`.

### 2. AI Classification and Response Generation

Send the message thread + listing context to Ollama on the Mac Mini.

The AI:
1. Classifies the message type:
   - `generic`: stock Facebook suggested question (availability, size, price - all already in the ad)
   - `answerable`: a genuine question with a clear, factual answer derivable from the listing
   - `personal`: the buyer is engaging in conversation, negotiating, or asking something
     that requires Alex's judgment
   - `unrelated`: message is not about a listing (spam, unrelated conversation)

2. Assigns a confidence level (0.0-1.0) that the classification is correct

3. Generates a suggested reply:
   - For `generic` / `answerable` (high confidence >= 0.85): a direct, helpful reply using
     information from the listing (size, condition, price, availability)
   - For `personal` / uncertain (confidence < 0.85): a human-sounding delay message such as
     "Hi! I'm out right now but I'll reply when I get back" or "At work right now, will get back
     to you soon!" - vary these so they don't become repetitive (store used variants)

The prompt is in `~/.config/clara/fb-prompts/messenger-system.md` and
`messenger-reply.md` (watched for changes).

### 3. Response Dispatch

**High-confidence generic/answerable replies (confidence >= 0.85):**
- The Chrome MCP server sends the reply directly, then marks the thread as read
- Log the action in `fb_messenger_threads` with the sent reply and timestamp
- Do NOT notify Alex; she doesn't need to review these

**Low-confidence or personal conversations:**
- The Chrome MCP server sends the delay/hold message
- Then marks the thread as **UNREAD** (so Alex sees it in her own Messenger)
- Stores the AI-generated suggested reply in `fb_messenger_threads` for her reference
- The web UI (task 030) also surfaces these in her attention queue with:
  - The conversation thread
  - The listing photos and details
  - The AI's suggested reply for her to copy/send/edit if desired

**Anti-detection:**
- Randomized delay before sending any reply (2-15 seconds after detection)
- Do not reply to the same thread more than once per hour
- Vary reply wording; do not send identical text to consecutive messages

### 4. Logging

All actions are logged. Alex can review:
- What was auto-replied and what was routed to her
- Full conversation history with AI classification and confidence
- Statistics: how many messages handled, auto-reply rate, response time

## Data Model (SQLite)

```sql
CREATE TABLE fb_messenger_threads (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id       TEXT NOT NULL,
    listing_id      TEXT,
    last_message    TEXT,
    classification  TEXT,
    confidence      REAL,
    suggested_reply TEXT,
    sent_reply      TEXT,
    action          TEXT,
    status          TEXT NOT NULL,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL
);
```

## Prompt File Layout

```
~/.config/clara/fb-prompts/
  messenger-system.md
  messenger-reply.md
  messenger-delay.md
```

## Key Behavioral Requirements

- Alex should not have to see or respond to "is this still available" type questions
- She should never miss a genuine inquiry - low-confidence messages are always surfaced
- Auto-replies must sound like Alex: friendly, brief, helpful, informal
- The intent must never send duplicate replies to the same thread in a short time window
- Response timing is randomized and not instantaneous (avoid appearing robotic)
- No Facebook API calls; all interaction is through browser automation

## Acceptance Criteria

- The intent polls Messenger on schedule and reads new marketplace message threads
- Generic/simple questions are auto-replied without Alex's involvement
- Ambiguous/personal messages receive a delay response and are marked unread for Alex
- Both auto-replied and routed messages appear in the web UI log
- The prompt files for reply style are watched for changes and applied immediately
- No identical reply text sent to two consecutive different buyers (variance enforced)
