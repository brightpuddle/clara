# notes_cleanup.star
clara.describe("Automated Zettelkasten maintenance and embedding synchronization")

DRY_RUN = True
VAULT_ROOT = "/Users/nathan/Library/CloudStorage/GoogleDrive-nhemingway@gmail.com/My Drive/notes"

def init_db():
    # Metadata and embedding table
    db.exec(sql="""
        CREATE TABLE IF NOT EXISTS zk_embeddings (
            id INTEGER PRIMARY KEY,
            path TEXT UNIQUE,
            hash TEXT,
            embedding TEXT -- JSON array of floats
        )
    """)
    db.exec(sql="CREATE INDEX IF NOT EXISTS idx_zk_embeddings_hash ON zk_embeddings(hash)")

def _hash_content(content):
    # Use shell to generate a fast hash
    safe_content = content.replace("'", "'\\''")
    res = shell.run(command="echo -n '" + safe_content + "' | shasum -a 256 | awk '{print $1}'")
    return res["output"].strip()

def sync_all_embeddings():
    init_db()
    notes = zk.note_list()
    
    db_paths_res = db.query(sql="SELECT path FROM zk_embeddings")
    db_paths = [row["path"] for row in db_paths_res]
    current_paths = []

    for note in notes:
        path = note["path"]
        current_paths.append(path)
        
        note_data = zk.note_get(note=path)
        content = note_data.get("content", "")
        if not content:
            continue
            
        current_hash = _hash_content(content)
        
        existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
        if len(existing) > 0 and existing[0]["hash"] == current_hash:
            continue
            
        if not DRY_RUN:
            print("Embedding: " + path)
            embed_res = llm.embed(category="local", input=[content])
            if len(embed_res) > 0:
                vector = embed_res[0]
                db.exec(
                    sql="INSERT INTO zk_embeddings (path, hash, embedding) VALUES (?, ?, ?) ON CONFLICT(path) DO UPDATE SET hash=excluded.hash, embedding=excluded.embedding", 
                    params=[path, current_hash, str(vector)]
                )
        else:
            print("DRY_RUN: Would embed and sync: " + path)

    for p in db_paths:
        if p not in current_paths:
            if not DRY_RUN:
                db.exec(sql="DELETE FROM zk_embeddings WHERE path = ?", params=[p])
            else:
                print("DRY_RUN: Would delete embedding for: " + p)

def on_note_change(event, path, root, timestamp):
    if not path.endswith(".md"):
        return
    init_db()
    exists_res = shell.run(command="test -f '" + path.replace("'", "'\\''") + "' && echo 'yes' || echo 'no'")
    exists = exists_res["output"].strip()
    if exists == "no":
        if not DRY_RUN:
            db.exec(sql="DELETE FROM zk_embeddings WHERE path = ?", params=[path])
        else:
            print("DRY_RUN: Would delete embedding for: " + path)
        return
    content = fs.read_file(path=path)
    current_hash = _hash_content(content)
    existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
    if len(existing) > 0 and existing[0]["hash"] == current_hash:
        return
    if not DRY_RUN:
        print("Real-time Embedding: " + path)
        embed_res = llm.embed(category="local", input=[content])
        if len(embed_res) > 0:
            vector = embed_res[0]
            db.exec(
                sql="INSERT INTO zk_embeddings (path, hash, embedding) VALUES (?, ?, ?) ON CONFLICT(path) DO UPDATE SET hash=excluded.hash, embedding=excluded.embedding", 
                params=[path, current_hash, str(vector)]
            )
    else:
        print("DRY_RUN: Would update real-time embedding for: " + path)

def task_maintenance():
    notes = zk.note_list()
    for note in notes:
        path = note["path"]
        name = note["name"]
        
        note_data = zk.note_get(note=path)
        content = note_data.get("content", "")
        
        # Check for empty notes
        if len(content.strip()) == 0:
            notify.send(title="Empty Note Detected", body="The note `" + name + "` is empty.\n\n[Open Note](obsidian://open?file=" + name.replace(" ", "%20") + ")")
            resp = notify.ask(question="What should we do?", options=["Delete", "Keep"])
            if resp["selection"] == "Delete":
                if not DRY_RUN:
                    zk.note_delete(note=path)
                else:
                    print("DRY_RUN: Would delete empty note: " + path)
            continue
            
        # Check dead links
        links = note.get("wikilinks", [])
        for link in links:
            target = zk.note_resolve_wikilink(target=link)
            if target == "":
                # Dead link
                notify.send(title="Dead Link Detected", body="Note `" + name + "` has a dead link to `[[" + link + "]]`.\n\n[Open Note](obsidian://open?file=" + name.replace(" ", "%20") + ")")
                resp = notify.ask(question="Remove the broken link brackets for `[[" + link + "]]`?", options=["Remove", "Keep"])
                if resp["selection"] == "Remove":
                    new_content = content.replace("[[" + link + "]]", link)
                    if not DRY_RUN:
                        zk.note_update(note=path, content=new_content)
                        content = new_content
                    else:
                        print("DRY_RUN: Would fix dead link " + link + " in " + path)

def test_maintenance():
    # Only run for 1 note to avoid spamming the HUD during tests
    task_maintenance()

def main():
    pass

clara.task(main)
clara.task(sync_all_embeddings)
clara.task(on_note_change, trigger=clara.on(fs.on_change, path=VAULT_ROOT, recursive=True))
clara.task(task_maintenance, schedule="0 2 * * 0") # Weekly on Sunday 2AM
clara.task(test_maintenance)
