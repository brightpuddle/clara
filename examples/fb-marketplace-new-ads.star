describe("Create Facebook Marketplace drafts from inbox photos")

WORK_ROOT = "${HOME}/.local/share/clara/fb-marketplace"
PROMPT_ROOT = "${HOME}/.config/clara/fb-prompts"
PHOTO_ALBUM_NAME = "Marketplace Inbox"
TEST_ALBUM_NAME = "Marketplace Test"

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

WORKER_INTERVAL = "15s"
GROUP_WINDOW_SECONDS = 5
HUMAN_DELAY_MIN_MS = 500
HUMAN_DELAY_MAX_MS = 1000
POST_DRAFT_GAP_SECONDS = 60

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
    if len(mod) < 16: return mod
    minute_str = mod[14:16]
    if minute_str.isdigit():
        m = int(minute_str)
        m = m - (m % 2)
        m_padded = str(m)
        if len(m_padded) == 1: m_padded = "0" + m_padded
        return mod[:14] + m_padded + ":00Z"
    return mod

def read_prompt(name):
    return tool("fs.read_file", path = PROMPT_ROOT + "/" + name)

def record_inbox_photo(path, group_key):
    now = iso_now()
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
    group_key = iso_now()
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

def uniquify_path(path):
    info = tool("fs.path_exists", path = path)
    if not info.get("exists"):
        return path

    name = basename(path)
    dot = name.rfind(".")
    if dot == -1:
        stem = name
        ext = ""
    else:
        stem = name[:dot]
        ext = name[dot:]

    parent = path[:len(path) - len(name)]
    for counter in range(1, 1000):
        candidate = parent + stem + "-" + str(counter) + ext
        info = tool("fs.path_exists", path = candidate)
        if not info.get("exists"):
            return candidate
    fail("Unable to allocate unique path for " + path)
    return ""

def pending_groups():
    rows = tool(
        "db.query",
        sql = """
SELECT group_key, path, created_at
FROM fb_inbox_photos
WHERE status = 'pending'
ORDER BY created_at ASC, id ASC
""",
    )
    grouped = {}
    for row in rows:
        key = row.get("group_key") or row.get("path")
        current = grouped.get(key)
        if current == None:
            grouped[key] = [row]
        else:
            current.append(row)
    return grouped

def choose_ready_groups(grouped):
    ready = []
    for key in grouped:
        rows = grouped.get(key)
        if len(rows) == 0:
            continue
        created = rows[0].get("created_at") or ""
        if created == "":
            ready.append([key, rows])
            continue
        if age_seconds(created) >= GROUP_WINDOW_SECONDS:
            ready.append([key, rows])
    return ready

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

def record_listing(group_key, image_paths, llm_result, listing):
    now = iso_now()
    photo_paths = "\n".join(image_paths)
    raw = llm_result.get("text") or ""
    tool(
        "db.exec",
        sql = """
INSERT INTO fb_listing (
    photo_paths, title, description, price, category, condition,
    status, gemini_raw, created_at, updated_at, source_group
) VALUES (?, ?, ?, ?, ?, ?, 'draft_pending', ?, ?, ?, ?)
""",
        params = [
            photo_paths,
            listing.get("title"),
            listing.get("description"),
            listing.get("price"),
            listing.get("category"),
            listing.get("condition"),
            raw,
            now,
            now,
            group_key,
        ],
    )

    row = tool(
        "db.query",
        sql = "SELECT id FROM fb_listing WHERE source_group = ? ORDER BY id DESC LIMIT 1",
        params = [group_key],
    )
    return row[0].get("id")

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
        human_delay_min_ms = HUMAN_DELAY_MIN_MS,
        human_delay_max_ms = HUMAN_DELAY_MAX_MS,
    )
    tab_id = nav.get("tab_id")
    
    tool("chrome.browser_wait_for_load", tab_id = tab_id, timeout_seconds = 60)
    tool("chrome.browser_wait_for_selector", tab_id = tab_id, selector = "div[role='main']", timeout_seconds = 30)

    # CDP upload
    tool(
        "chrome.browser_upload_file",
        tab_id = tab_id,
        selector = "input[type='file']",
        file_paths = image_paths,
        human_delay = False,
    )

    # ROBUST DISCOVERY: Query elements first to find the exact IDs for Title, Price, and Description
    # Order within [role='main']: 
    # 1. input[type='text'] -> Title
    # 2. input[type='text'] -> Price
    # 3. textarea           -> Description
    inputs = tool("chrome.browser_query_elements", tab_id = tab_id, selector = "div[role='main'] input[type='text']")
    textareas = tool("chrome.browser_query_elements", tab_id = tab_id, selector = "div[role='main'] textarea")
    
    if len(inputs) < 2:
        fail("Expected at least 2 text inputs (Title, Price), found " + str(len(inputs)))
    if len(textareas) < 1:
        fail("Expected at least 1 textarea (Description), found " + str(len(textareas)))

    title_selector = "#" + inputs[0].get("id")
    price_selector = "#" + inputs[1].get("id")
    desc_selector = "#" + textareas[0].get("id")

    # Native typing via CDP
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = title_selector, text = listing.get("title"), human_delay = False)
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = price_selector, text = str(listing.get("price")), human_delay = False)
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = desc_selector, text = listing.get("description"), human_delay = False)

    # Helper click to commit React state
    tool("chrome.browser_click", tab_id = tab_id, selector = title_selector, human_delay = False)
    
    # Save draft
    tool("chrome.browser_click", tab_id = tab_id, selector = "[aria-label='Save draft']", human_delay = False)

    # Small pause for navigation
    tool("db.query", sql = "SELECT 1")
    
    snapshot = tool("chrome.browser_read_page", tab_id = tab_id)
    url = snapshot.get("url") or ""
    
    return url

def archive_images(group_key, image_paths):
    group_archive = ARCHIVE_ROOT + "/" + group_key.replace(":", "-")
    tool("fs.create_directory", path = group_archive)
    archived = []
    for path in image_paths:
        destination = group_archive + "/" + basename(path)
        destination = uniquify_path(destination)
        tool("fs.move_file", source = path, destination = destination)
        archived.append(destination)
    return archived

def notify_draft_created(listing, archived_paths):
    body = listing.get("title") + " - $" + str(listing.get("price"))
    url = "https://www.facebook.com/marketplace/you/selling"
    tool(
        "macos.notify_send_interactive",
        title = "Facebook Marketplace draft created",
        body = body,
        url = url,
        actions = [
            {"id": "default", "title": "Open Selling Page", "foreground": True}
        ]
    )

def process_group(group_key, rows):
    image_paths = []
    for row in rows:
        image_paths.append(row.get("path"))

    mark_photos_status(group_key, "processing")

    llm_result, parsed = llm_analysis(image_paths)
    listing = sanitize_listing(parsed)
    listing_id = record_listing(group_key, image_paths, llm_result, listing)

    draft_url = create_marketplace_draft(image_paths, listing)
    archived = archive_images(group_key, image_paths)

    mark_photos_status(group_key, "archived")
    update_listing_status(listing_id, "draft_created", draft_url)
    notify_draft_created(listing, archived)
    return {"listing_id": listing_id, "draft_url": draft_url}

def run_once():
    ensure_layout()
    ensure_db_schema()
    
    # stage_new_inbox_files()
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
