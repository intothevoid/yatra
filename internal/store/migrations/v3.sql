-- Persist the details entered during browser based trip creation.
ALTER TABLE trips ADD COLUMN location TEXT NOT NULL DEFAULT '';
ALTER TABLE trips ADD COLUMN notes TEXT NOT NULL DEFAULT '';
