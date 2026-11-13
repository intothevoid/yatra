CREATE TABLE videos (
    id TEXT PRIMARY KEY NOT NULL,
    trip_id TEXT NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    source_file_id TEXT NOT NULL,
    file_name TEXT NOT NULL,
    path TEXT NOT NULL,
    duration REAL NOT NULL,
    width INTEGER NOT NULL,
    height INTEGER NOT NULL,
    selected INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_videos_trip ON videos(trip_id);
