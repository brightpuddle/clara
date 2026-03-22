describe("Test robust Facebook Marketplace population with placeholders via discovery")

PHOTO_PATHS = [
    "/Users/nathan/src/github.com/brightpuddle/clara/tmp/s-l1600.webp",
    "/Users/nathan/src/github.com/brightpuddle/clara/tmp/s-l1600-1.webp",
    "/Users/nathan/src/github.com/brightpuddle/clara/tmp/s-l1600-5.webp"
]

def create_placeholder_draft():
    # 1. Navigate
    nav = tool("chrome.browser_navigate", url = "https://www.facebook.com/marketplace/create/item")
    tab_id = nav.get("tab_id")
    
    # 2. Wait for load
    tool("chrome.browser_wait_for_load", tab_id = tab_id, timeout_seconds = 60)
    tool("chrome.browser_wait_for_selector", tab_id = tab_id, selector = "div[role='main']", timeout_seconds = 30)

    # 3. Upload
    tool("chrome.browser_upload_file", tab_id = tab_id, selector = "input[type='file']", file_paths = PHOTO_PATHS)

    # 4. Discovery Phase
    # Instead of hardcoding selectors that React might break, we query for candidates first.
    inputs = tool("chrome.browser_query_elements", tab_id = tab_id, selector = "div[role='main'] input[type='text']")
    textareas = tool("chrome.browser_query_elements", tab_id = tab_id, selector = "div[role='main'] textarea")
    
    if len(inputs) < 2:
        fail("Expected at least 2 text inputs (Title, Price), found " + str(len(inputs)))
    if len(textareas) < 1:
        fail("Expected at least 1 textarea (Description), found " + str(len(textareas)))

    # We use IDs if they exist, or fallback to positional. 
    # But since we just queried them, we can use the IDs directly from the query result!
    title_id = inputs[0].get("id")
    price_id = inputs[1].get("id")
    desc_id = textareas[0].get("id")
    
    print("Discovered Title ID: " + title_id)
    print("Discovered Price ID: " + price_id)
    print("Discovered Description ID: " + desc_id)

    # 5. Fill using discovered IDs
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = "#" + title_id, text = "Robust Discovery Title", human_delay = False)
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = "#" + price_id, text = "75", human_delay = False)
    tool("chrome.browser_type_by_selector", tab_id = tab_id, selector = "#" + desc_id, text = "Robust Discovery Description.", human_delay = False)

    # 6. Commit and Save
    tool("chrome.browser_click", tab_id = tab_id, selector = "#" + title_id, human_delay = False)
    tool("chrome.browser_click", tab_id = tab_id, selector = "[aria-label='Save draft']", human_delay = False)

    # 7. Result
    tool("db.query", sql = "SELECT 1")
    snapshot = tool("chrome.browser_read_page", tab_id = tab_id)
    print("Draft URL: " + (snapshot.get("url") or "unknown"))
    return snapshot

def main():
    return create_placeholder_draft()

task(main)
