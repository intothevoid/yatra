-- Migration v2: Add progress tracking to jobs

ALTER TABLE jobs ADD COLUMN progress INTEGER DEFAULT 0;
ALTER TABLE jobs ADD COLUMN current_step TEXT;
ALTER TABLE jobs ADD COLUMN total_items INTEGER DEFAULT 0;
ALTER TABLE jobs ADD COLUMN completed_items INTEGER DEFAULT 0;
