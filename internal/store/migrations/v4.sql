-- Persist the photo selected as each trip's Ghost feature image.
ALTER TABLE trips ADD COLUMN feature_photo_id TEXT NOT NULL DEFAULT '';
