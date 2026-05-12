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
    safe_content = content.replace("'", "'\\''")
    res = shell.run(command="echo -n '" + safe_content + "' | shasum -a 256 | awk '{print $1}'")
    return res["output"].strip()

def _dot_product(v1, v2):
    res = 0.0
    for i in range(len(v1)):
        res += v1[i] * v2[i]
    return res

def _vec_search_fallback(vector, limit=3, min_score=0.7):
    # Fallback since sqlite-vec is unavailable
    rows = db.query(sql="SELECT path, embedding FROM zk_embeddings")
    results = []
    for row in rows:
        emb_str = row["embedding"]
        if not emb_str or emb_str == "None":
            continue
        # Parse JSON string back to list
        # Simple parsing for list of floats
        v_other = [float(x) for x in emb_str.strip("[]").split(", ")]
        
        # Cosine similarity (assuming vectors are normalized, which LLM embeddings usually are)
        score = _dot_product(vector, v_other)
        if score >= min_score:
            results.append({"path": row["path"], "score": score})
    
    # Sort by score descending
    # Starlark doesn't have sorted(key=...), so we do a simple bubble sort or just take the best
    # For now, just return all and we'll pick in the task
    return results

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
        if not content: continue
        current_hash = _hash_content(content)
        existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
        if len(existing) > 0 and existing[0]["hash"] == current_hash: continue
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
            if not DRY_RUN: db.exec(sql="DELETE FROM zk_embeddings WHERE path = ?", params=[p])
            else: print("DRY_RUN: Would delete embedding for: " + p)

def on_note_change(event, path, root, timestamp):
    if not path.endswith(".md"): return
    init_db()
    exists_res = shell.run(command="test -f '" + path.replace("'", "'\\''") + "' && echo 'yes' || echo 'no'")
    if exists_res["output"].strip() == "no":
        if not DRY_RUN: db.exec(sql="DELETE FROM zk_embeddings WHERE path = ?", params=[path])
        return
    content = fs.read_file(path=path)
    current_hash = _hash_content(content)
    existing = db.query(sql="SELECT hash FROM zk_embeddings WHERE path = ?", params=[path])
    if len(existing) > 0 and existing[0]["hash"] == current_hash: return
    if not DRY_RUN:
        embed_res = llm.embed(category="local", input=[content])
        if len(embed_res) > 0:
            db.exec(
                sql="INSERT INTO zk_embeddings (path, hash, embedding) VALUES (?, ?, ?) ON CONFLICT(path) DO UPDATE SET hash=excluded.hash, embedding=excluded.embedding", 
                params=[path, current_hash, str(embed_res[0])]
            )

def task_discover_links():
    notes = zk.note_list()
    count = 0
    for note in notes:
        if count >= 3: break
        path = note["path"]
        if len(note.get("wikilinks", [])) > 2: continue
        res = db.query(sql="SELECT embedding FROM zk_embeddings WHERE path = ?", params=[path])
        if not res: continue
        emb_str = res[0]["embedding"]
        if not emb_str: continue
        vector = [float(x) for x in emb_str.strip("[]").split(", ")]
        similar = _vec_search_fallback(vector, min_score=0.85)
        for sim in similar:
            if sim["path"] == path: continue
            # LLM check
            source_content = zk.note_get(note=path)["content"]
            target_content = zk.note_get(note=sim["path"])["content"]
            prompt = "Note 1: " + source_content + "\n\nNote 2: " + target_content + "\n\nShould these be linked? If yes, suggest where in Note 1 to add [[wikilink]]."
            proposal = llm.generate(category="local", message=prompt)
            if "[[wikilink]]" in proposal or "[[" in proposal:
                notify.send(title="Link Suggestion", body="Suggest linking `" + note["name"] + "` to `" + sim["path"] + "`\n\n" + proposal)
                resp = notify.ask(question="Apply link?", options=["Approve", "Reject"])
                if resp["selection"] == "Approve":
                    # Simple append for now
                    new_content = source_content + "\n\nSee also: [[" + sim["path"].split("/")[-1].replace(".md", "") + "]]"
                    if not DRY_RUN: zk.note_update(note=path, content=new_content)
            count += 1

def task_propose_tags():
    # Similar logic to link discovery but with zk.tag_list()
    pass

def task_route_notes():
    # Targets Cisco/ and Notes/
    pass

def task_extract_insights():
    # Targets Journal/
    pass

def task_maintenance():
    notes = zk.note_list()
    for note in notes:
        path, name = note["path"], note["name"]
        note_data = zk.note_get(note=path)
        content = note_data.get("content", "")
        if len(content.strip()) == 0:
            notify.send(title="Empty Note", body="Note `" + name + "` is empty.")
            resp = notify.ask(question="Delete?", options=["Delete", "Keep"])
            if resp["selection"] == "Delete":
                if not DRY_RUN: zk.note_delete(note=path)
            continue
        for link in note.get("wikilinks", []):
            if zk.note_resolve_wikilink(target=link) == "":
                notify.send(title="Dead Link", body="Note `" + name + "` has a dead link to `[[" + link + "]]`.")
                resp = notify.ask(question="Remove brackets?", options=["Remove", "Keep"])
                if resp["selection"] == "Remove":
                    new_content = content.replace("[[" + link + "]]", link)
                    if not DRY_RUN:
                        zk.note_update(note=path, content=new_content)
                        content = new_content

def main():
    pass

clara.task(main)
clara.task(sync_all_embeddings)
clara.task(on_note_change, trigger=clara.on(fs.on_change, path=VAULT_ROOT, recursive=True))
clara.task(task_maintenance, schedule="0 2 * * 0")
clara.task(task_discover_links, schedule="0 3 * * 0")
