describe("Create Facebook Marketplace drafts from inbox photos")

WORK_ROOT = "${HOME}/.local/share/clara/fb-marketplace"
PROMPT_ROOT = "${HOME}/.config/clara/fb-prompts"
PHOTO_ALBUM_NAME = "Marketplace Inbox"

PENDING_ROOT = WORK_ROOT + "/pending"
ARCHIVE_ROOT = WORK_ROOT + "/archive"

DEFAULT_SYSTEM_PROMPT = """You are helping create Facebook Marketplace drafts for a local seller.

Return only valid JSON with this exact shape:
{
  "title": string,
  "description": string,
  "price": number,
  "category": string,
  "condition": string,
  "confidence": number,
  "notes": string
}

Requirements:
- Focus on children's clothing, shoes, toys, and household goods.
- Price for a local resale listing, significantly below retail.
- Title should contain useful search terms like brand, size, and item type when visible.
- Description should be concise and front-load condition, size, and brand details.
- Keep category and condition aligned with Facebook Marketplace style labels.
- If details are uncertain, make the best reasonable estimate and explain uncertainty in notes.
"""

DEFAULT_USER_PROMPT = """Create a Facebook Marketplace draft recommendation for the attached photos.

Use these preferences:
- Door pickup only.
- No shipping language.
- Optimize for quick local sale.
- Prefer clear, practical wording over marketing fluff.

Additional category guidance:
{{CATEGORIES}}
"""

DEFAULT_CATEGORIES = """Preferred category guidance:
- Clothing, Shoes & Accessories
- Baby & Kids
- Toys & Games
- Household

Condition guidance:
- New
- Like New
- Good
- Fair
"""

WORKER_INTERVAL = "30s"
GROUP_WINDOW_SECONDS = 10
HUMAN_DELAY_MIN_MS = 2000
HUMAN_DELAY_MAX_MS = 7000
POST_DRAFT_GAP_SECONDS = 180

def ensure_layout():
    tool("fs.create_directory", path = WORK_ROOT)
    tool("fs.create_directory", path = PENDING_ROOT)
    tool("fs.create_directory", path = ARCHIVE_ROOT)
    tool("fs.create_directory", path = PROMPT_ROOT)

    ensure_prompt_file("new-ad-system.md", DEFAULT_SYSTEM_PROMPT)
    ensure_prompt_file("new-ad-user.md", DEFAULT_USER_PROMPT)
    ensure_prompt_file("categories.md", DEFAULT_CATEGORIES)

def ensure_prompt_file(name, content):
    path = PROMPT_ROOT + "/" + name
    info = tool("fs.path_exists", path = path)
    if not info.get("exists"):
        tool("fs.write_file", path = path, content = content)

def ensure_db_schema():
    tool("db.exec", sql = """
CREATE TABLE IF NOT EXISTS fb_inbox_photos (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    path        TEXT NOT NULL,
    group_key   TEXT,
    status      TEXT NOT NULL,
    created_at  DATETIME NOT NULL,
    updated_at  DATETIME NOT NULL
)
""")
    tool("db.exec", sql = """
CREATE TABLE IF NOT EXISTS fb_listing (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    photo_paths     TEXT NOT NULL,
    title           TEXT NOT NULL,
    description     TEXT NOT NULL,
    price           REAL NOT NULL,
    category        TEXT NOT NULL,
    condition       TEXT NOT NULL,
    status          TEXT NOT NULL,
    gemini_raw      TEXT,
    created_at      DATETIME NOT NULL,
    updated_at      DATETIME NOT NULL,
    draft_url       TEXT,
    source_group    TEXT,
    notification_at DATETIME
)
""")

def iso_now():
    rows = tool(
        "db.query",
        sql = "SELECT strftime('%Y-%m-%dT%H:%M:%SZ', 'now') AS value",
    )
    return rows[0].get("value") or ""

def derive_group_key(path):
    info = tool("fs.get_file_info", path = path)
    mod = info.get("mod_time") or ""

    # 2-minute grouping bucket
    if len(mod) < 16:
        return mod
    minute_str = mod[14:16]
    if minute_str.isdigit():
        m = int(minute_str)
        m = m - (m % 2)
        m_padded = str(m)
        if len(m_padded) == 1:
            m_padded = "0" + m_padded
        return mod[:14] + m_padded + ":00Z"
    return mod

def read_prompt(name):
    return tool("fs.read_file", path = PROMPT_ROOT + "/" + name)

def record_inbox_photo(path, group_key):
    now = iso_now()
    existing = tool(
        "db.query",
        sql = "SELECT id FROM fb_inbox_photos WHERE path = ? LIMIT 1",
        params = [path],
    )
    if len(existing) > 0:
        return

    tool(
        "db.exec",
        sql = """
INSERT INTO fb_inbox_photos (path, group_key, status, created_at, updated_at)
VALUES (?, ?, 'pending', ?, ?)
""",
        params = [path, group_key, now, now],
    )

def stage_new_inbox_files():
    ensure_layout()
    assets = tool("macos.photos_album_assets", album_name = PHOTO_ALBUM_NAME, limit = 50)
    if len(assets) == 0:
        return []

    staged = []
    for asset in assets:
        asset_id = asset.get("identifier") or ""
        if asset_id == "":
            continue
        export = tool(
            "macos.photos_export_assets",
            asset_ids = [asset_id],
            destination_dir = PENDING_ROOT,
        )
        if len(export) == 0:
            continue
        destination = export[0].get("path") or ""
        if destination == "":
            continue
        group_key = derive_group_key(destination)
        record_inbox_photo(destination, group_key)
        tool(
            "macos.photos_album_remove_assets",
            album_name = PHOTO_ALBUM_NAME,
            asset_ids = [asset_id],
        )
        staged.append(destination)
    return staged

def basename(path):
    normalized = path.replace("\\", "/")
    parts = normalized.split("/")
    return parts[len(parts) - 1]

def age_seconds(iso_time):
    current = tool(
        "db.query",
        sql = "SELECT CAST(strftime('%s', 'now') AS INTEGER) AS current_ts, " +
              "CAST(strftime('%s', ?) AS INTEGER) AS target_ts",
        params = [iso_time],
    )
    if len(current) == 0:
        return 0
    row = current[0]
    current_ts = row.get("current_ts") or 0
    target_ts = row.get("target_ts") or current_ts
    return current_ts - target_ts

def llm_analysis(image_paths):
    system_prompt = read_prompt("new-ad-system.md")
    user_prompt = read_prompt("new-ad-user.md")
    categories = read_prompt("categories.md")
    prompt = user_prompt.replace("{{CATEGORIES}}", categories)

    result = tool(
        "llm.generate_vision",
        prompt = prompt,
        system = system_prompt,
        images = image_paths,
        category = "vision",
    )
    parsed = result.get("parsed")
    if parsed == None:
        fail("Gemini response did not parse as JSON object")
    return [result, parsed]

def sanitize_listing(parsed):
    title = parsed.get("title") or "Untitled"
    description = parsed.get("description") or ""
    category = parsed.get("category") or ""
    condition = parsed.get("condition") or ""
    price_value = parsed.get("price") or 0

    return {
        "title": title,
        "description": description,
        "category": category,
        "condition": condition,
        "price": price_value,
    }

def mark_photos_status(group_key, status):
    tool(
        "db.exec",
        sql = "UPDATE fb_inbox_photos SET status = ?, updated_at = ? WHERE group_key = ?",
        params = [status, iso_now(), group_key],
    )

def update_listing_status(listing_id, status, draft_url = ""):
    tool(
        "db.exec",
        sql = "UPDATE fb_listing SET status = ?, draft_url = ?, updated_at = ? WHERE id = ?",
        params = [status, draft_url, iso_now(), listing_id],
    )

def create_marketplace_draft(image_paths, listing):
    nav = tool(
        "chrome.browser_navigate",
        url = "https://www.facebook.com/marketplace/create/item",
        background = False,
    )
    tab_id = nav.get("tab_id")

    tool("chrome.browser_wait_for_load", tab_id = tab_id, timeout_seconds = 60)
    tool("chrome.browser_wait_for_selector", tab_id = tab_id, selector = "input[type='file']", timeout_seconds = 30)

    # CDP upload (Fixed in extension to avoid double upload)
    tool(
        "chrome.browser_upload_file",
        tab_id = tab_id,
        selector = "input[type='file']",
        file_paths = image_paths,
        human_delay = False,
    )

    # Title - Using nth-of-type index found during testing
    tool("chrome.browser_fill", tab_id = tab_id, selector = "input:nth-of-type(1)", value = listing.get("title"), human_delay = False)

    # Price
    tool("chrome.browser_fill", tab_id = tab_id, selector = "input:nth-of-type(2)", value = str(listing.get("price")), human_delay = False)

    # Description
    tool("chrome.browser_fill_by_label", tab_id = tab_id, label = "Description", value = listing.get("description"), tag = "textarea", human_delay = False)

    # Click Title again to help commit React state
    tool("chrome.browser_click", tab_id = tab_id, selector = "input:nth-of-type(1)", human_delay = False)

    # Save draft
    tool("chrome.browser_click", tab_id = tab_id, selector = "[aria-label='Save draft']", human_delay = False)

    # Wait for navigation
    tool("db.exec", sql = "SELECT 1")  # Short pause

    snapshot = tool("chrome.browser_read_page", tab_id = tab_id)
    url = snapshot.get("url") or ""

    # Keep tab open for user inspection as requested
    return url

def process_group(group_key, rows):
    image_paths = []
    for row in rows:
        image_paths.append(row.get("path"))

    mark_photos_status(group_key, "processing")

    llm_result, parsed = llm_analysis(image_paths)
    listing = sanitize_listing(parsed)

    # Logic to insert into DB skipped for manual test integration

    draft_url = create_marketplace_draft(image_paths, listing)
    return {"listing_id": 0, "tab_id": 0, "draft_url": draft_url}

def run_once():
    # Simplified for testing
    grouped = pending_groups()
    ready = choose_ready_groups(grouped)

    results = []
    for item in ready:
        group_key = item[0]
        rows = item[1]
        results.append(process_group(group_key, rows))
    return {"processed": len(results), "results": results}

def main():
    return run_once()

task(main, interval = WORKER_INTERVAL)
