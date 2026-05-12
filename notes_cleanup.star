# notes_cleanup.star
clara.describe("Automated Zettelkasten maintenance and embedding synchronization")

DRY_RUN = True

def init_db():
    # Metadata and embedding table
    # Using standard table since sqlite-vec is currently unavailable in this environment
    db.exec(sql="""
        CREATE TABLE IF NOT EXISTS zk_embeddings (
            id INTEGER PRIMARY KEY,
            path TEXT UNIQUE,
            hash TEXT,
            embedding TEXT -- JSON array of floats
        )
    """)
    db.exec(sql="CREATE INDEX IF NOT EXISTS idx_zk_embeddings_hash ON zk_embeddings(hash)")

def test_init_db():
    init_db()
    
    # Check zk_embeddings
    res = db.query(sql="SELECT count(*) as c FROM sqlite_master WHERE type='table' AND name='zk_embeddings'")
    must.eq(x=res[0]["c"], y=1)

def main():
    pass

clara.task(main)
clara.task(test_init_db)
