-- AgentLogger initial schema (v1)

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    bundle_id    TEXT NOT NULL,
    device_id    TEXT NOT NULL,
    device_name  TEXT,
    device_kind  TEXT,
    os_version   TEXT,
    app_version  TEXT,
    started_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    metadata     TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_bundle ON sessions(bundle_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(last_seen_at DESC);

CREATE TABLE IF NOT EXISTS logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    ts         INTEGER NOT NULL,
    level      INTEGER NOT NULL,
    category   TEXT,
    message    TEXT NOT NULL,
    metadata   TEXT,
    file       TEXT,
    line       INTEGER,
    func       TEXT,
    UNIQUE(session_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_logs_session_seq ON logs(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_logs_session_ts  ON logs(session_id, ts);
CREATE INDEX IF NOT EXISTS idx_logs_ts          ON logs(ts);
CREATE INDEX IF NOT EXISTS idx_logs_level       ON logs(level);

CREATE VIRTUAL TABLE IF NOT EXISTS logs_fts USING fts5(message, content='logs', content_rowid='id');

CREATE TRIGGER IF NOT EXISTS logs_ai AFTER INSERT ON logs BEGIN
    INSERT INTO logs_fts(rowid, message) VALUES (new.id, new.message);
END;
CREATE TRIGGER IF NOT EXISTS logs_ad AFTER DELETE ON logs BEGIN
    INSERT INTO logs_fts(logs_fts, rowid, message) VALUES('delete', old.id, old.message);
END;
CREATE TRIGGER IF NOT EXISTS logs_au AFTER UPDATE ON logs BEGIN
    INSERT INTO logs_fts(logs_fts, rowid, message) VALUES('delete', old.id, old.message);
    INSERT INTO logs_fts(rowid, message) VALUES (new.id, new.message);
END;
