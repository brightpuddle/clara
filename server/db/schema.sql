-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- All ingested documents (one row per file)
CREATE TABLE IF NOT EXISTS documents (
    id          TEXT PRIMARY KEY,   -- absolute file path
    path        TEXT NOT NULL,
    title       TEXT NOT NULL,
    content     TEXT NOT NULL,
    checksum    TEXT NOT NULL,      -- sha256 of content, for change detection
    source_type TEXT NOT NULL DEFAULT 'markdown',
    modified_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Text chunks with embeddings (nomic-embed-text produces 768-dim vectors)
CREATE TABLE IF NOT EXISTS chunks (
    id          BIGSERIAL PRIMARY KEY,
    document_id TEXT        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INT         NOT NULL,
    content     TEXT        NOT NULL,
    embedding   vector(768) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (document_id, chunk_index)
);

-- HNSW index for fast approximate nearest-neighbour search
CREATE INDEX IF NOT EXISTS chunks_embedding_idx
    ON chunks USING hnsw (embedding vector_cosine_ops);

-- AI-generated suggestions waiting for user approval
CREATE TABLE IF NOT EXISTS suggestions (
    id            BIGSERIAL PRIMARY KEY,
    type          TEXT        NOT NULL DEFAULT 'add_backlink',
    -- source: the document that SHOULD contain the new link
    source_doc_id TEXT        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    -- target: the document being linked to
    target_doc_id TEXT        NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    similarity    FLOAT       NOT NULL,
    context       TEXT        NOT NULL, -- explanation shown to user
    status        TEXT        NOT NULL DEFAULT 'pending', -- pending|approved|rejected|applied
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- prevent duplicate suggestions for the same pair
    UNIQUE (source_doc_id, target_doc_id)
);
