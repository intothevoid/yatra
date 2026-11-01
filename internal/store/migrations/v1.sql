-- Migration v1: Initial schema
-- Based on spec §9 (Persistent state, database and job engine)

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS trips (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_path TEXT,
    notes_hash TEXT,
    status TEXT NOT NULL DEFAULT 'IMPORTED',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS source_files (
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    mime_type TEXT,
    file_size INTEGER,
    sha256 TEXT,
    capture_time TEXT,
    scan_error TEXT,
    PRIMARY KEY (trip_id, relative_path)
);

CREATE TABLE IF NOT EXISTS photos (
    id TEXT PRIMARY KEY,
    source_file_id TEXT NOT NULL,
    exif TEXT,
    thumbnail_path TEXT,
    preview_path TEXT,
    export_path TEXT,
    vlm_result TEXT,
    vlm_schema_version INTEGER,
    status TEXT NOT NULL DEFAULT 'unknown',
    ranking_reasons TEXT,
    user_override INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS photo_groups (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    type TEXT NOT NULL,  -- "duplicate" or "similar"
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS photo_group_members (
    group_id TEXT NOT NULL REFERENCES photo_groups(id) ON DELETE CASCADE,
    photo_id TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    PRIMARY KEY (group_id, photo_id)
);

CREATE TABLE IF NOT EXISTS experiences (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    note_spans TEXT,  -- JSON: [{line, start, end, text}]
    location TEXT,
    foods TEXT,       -- JSON: ["item1", "item2"]
    sentiment TEXT,
    uncertainty TEXT, -- JSON: ["claim1", "claim2"]
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS photo_matches (
    experience_id TEXT NOT NULL REFERENCES experiences(id) ON DELETE CASCADE,
    photo_id TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE,
    score REAL,
    justification TEXT,
    user_override INTEGER DEFAULT 0,
    PRIMARY KEY (experience_id, photo_id)
);

CREATE TABLE IF NOT EXISTS style_profiles (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    data TEXT NOT NULL,  -- JSON: paragraph length, tone, etc.
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS articles (
    id TEXT PRIMARY KEY,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    current_revision TEXT,
    approved_revision TEXT,
    status TEXT NOT NULL DEFAULT 'EDITING',
    title TEXT
);

CREATE TABLE IF NOT EXISTS article_revisions (
    id TEXT PRIMARY KEY,
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    author TEXT NOT NULL,  -- "system" or "user"
    reason TEXT,
    document TEXT NOT NULL,  -- canonical JSON
    provenance TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    target_id TEXT,
    state TEXT NOT NULL DEFAULT 'queued',
    step INTEGER DEFAULT 0,
    attempt_count INTEGER DEFAULT 0,
    leased_by TEXT,
    lease_expiry TEXT,
    error TEXT,
    cancelled INTEGER DEFAULT 0,
    idempotency_key TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS ghost_exports (
    id TEXT PRIMARY KEY,
    article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
    revision_id TEXT NOT NULL REFERENCES article_revisions(id) ON DELETE CASCADE,
    remote_post_id TEXT,
    remote_updated_at TEXT,
    exported_urls TEXT,  -- JSON: [{hash, url}]
    last_result TEXT,
    last_error TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Indexes for common queries
CREATE INDEX IF NOT EXISTS idx_photos_source_file ON photos(source_file_id);
CREATE INDEX IF NOT EXISTS idx_photos_status ON photos(status);
CREATE INDEX IF NOT EXISTS idx_experiences_trip ON experiences(trip_id);
CREATE INDEX IF NOT EXISTS idx_photo_matches_experience ON photo_matches(experience_id);
CREATE INDEX IF NOT EXISTS idx_photo_matches_photo ON photo_matches(photo_id);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs(state);
CREATE INDEX IF NOT EXISTS idx_jobs_type ON jobs(type);
CREATE INDEX IF NOT EXISTS idx_article_revisions_article ON article_revisions(article_id);
