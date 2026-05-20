-- v2: platform + app_build (additive, optional)

ALTER TABLE sessions ADD COLUMN platform  TEXT;
ALTER TABLE sessions ADD COLUMN app_build TEXT;

CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
