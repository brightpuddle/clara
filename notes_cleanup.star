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
    
    # Get all current paths in db
    db_paths_res = db.query(sql="SELECT path FROM zk_embeddings")
    db_paths = [row["path"] for row in db_paths_res]
    current_paths = []

    for note in notes:
        path = note["path"]
        current_paths.append(path)
        
        # Read full content to hash
        note_data = zk.note_get(note=path)
        content = note_data.get("content", "")
        if not content:
            continue
            
        current_hash = _hash_content(content)
        
        # Check if exists and matches
        existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
        if len(existing) > 0 and existing[0]["hash"] == current_hash:
            continue # unchanged
            
        # Needs update
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

    # Clean up deleted notes
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
    
    # Safely check existence via shell
    exists_res = shell.run(command="test -f '" + path.replace("'", "'\\''") + "' && echo 'yes' || echo 'no'")
    exists = exists_res["output"].strip()
    
    if exists == "no":
        if not DRY_RUN:
            db.exec(sql="DELETE FROM zk_embeddings WHERE path = ?", params=[path])
        else:
            print("DRY_RUN: Would delete embedding for: " + path)
        return

    # Note: fs.read_file uses absolute paths
    content = fs.read_file(path=path)
    current_hash = _hash_content(content)
    
    existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
    if len(existing) > 0 and existing[0]["hash"] == current_hash:
        return # unchanged

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

def test_sync_all_embeddings():
    sync_all_embeddings()
    print("sync_all_embeddings finished")

def test_init_db():
    init_db()
    res = db.query(sql="SELECT count(*) as c FROM sqlite_master WHERE type='table' AND name='zk_embeddings'")
    must.eq(x=res[0]["c"], y=1)

def main():
    pass

clara.task(main)
clara.task(sync_all_embeddings)
clara.task(on_note_change, trigger=clara.on(fs.on_change, path=VAULT_ROOT, recursive=True))
clara.task(test_init_db)
clara.task(test_sync_all_embeddings)
