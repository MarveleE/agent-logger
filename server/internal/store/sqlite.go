package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/agentlogger/agentlog/internal/model"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// SQLiteStore persists sessions and logs in a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// Open initializes the database at the given path, applies migrations, and
// returns a ready-to-use store. The directory must already exist.
func Open(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Single writer model: serialize writes via the connection pool.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

func (s *SQLiteStore) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		var version int
		if _, err := fmt.Sscanf(name, "%d_", &version); err != nil || version == 0 {
			return fmt.Errorf("bad migration name %q", name)
		}
		var applied int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version=?`, version).Scan(&applied); err != nil {
			return err
		}
		if applied > 0 {
			continue
		}
		sqlBytes, err := fs.ReadFile(migrationFS, "migrations/"+name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES(?, ?)`, version, nowMS()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// ---------------- Sessions ----------------

func (s *SQLiteStore) CreateSession(ctx context.Context, sess *model.Session) error {
	meta, err := marshalMetadata(sess.Metadata)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO sessions(id, bundle_id, device_id, device_name, device_kind,
			platform, os_version, app_version, app_build, started_at, last_seen_at, metadata)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET last_seen_at=excluded.last_seen_at`,
		sess.ID, sess.BundleID, sess.DeviceID, nullString(sess.DeviceName),
		nullString(sess.DeviceKind), nullString(sess.Platform),
		nullString(sess.OSVersion), nullString(sess.AppVersion), nullString(sess.AppBuild),
		sess.StartedAt, sess.LastSeenAt, nullString(meta))
	return err
}

func (s *SQLiteStore) GetSession(ctx context.Context, id string) (*model.Session, error) {
	row := s.db.QueryRowContext(ctx, selectSessionColumns+" FROM sessions WHERE id=?", id)
	return scanSession(row)
}

// selectSessionColumns is the canonical SELECT clause for sessions; mirrored in scanSession.
const selectSessionColumns = `SELECT id, bundle_id, device_id, device_name, device_kind,
	platform, os_version, app_version, app_build, started_at, last_seen_at, metadata`

func (s *SQLiteStore) UpdateSessionLastSeen(ctx context.Context, id string, ts int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, ts, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteStore) ListSessions(ctx context.Context, opts ListSessionsOpts) ([]model.Session, error) {
	var (
		where []string
		args  []any
	)
	if opts.BundleID != "" {
		where = append(where, "bundle_id=?")
		args = append(args, opts.BundleID)
	}
	if opts.DeviceID != "" {
		where = append(where, "device_id=?")
		args = append(args, opts.DeviceID)
	}
	if opts.Platform != "" {
		where = append(where, "platform=?")
		args = append(args, opts.Platform)
	}
	if opts.ActiveWithin > 0 {
		where = append(where, "last_seen_at >= ?")
		args = append(args, nowMS()-opts.ActiveWithin)
	}
	q := selectSessionColumns + ` FROM sessions`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY started_at DESC"
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sess)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) LatestSession(ctx context.Context, bundleID string) (*model.Session, error) {
	if bundleID == "" {
		return nil, errors.New("bundleID required")
	}
	row := s.db.QueryRowContext(ctx, selectSessionColumns+
		" FROM sessions WHERE bundle_id=? ORDER BY started_at DESC LIMIT 1", bundleID)
	return scanSession(row)
}

// ---------------- Logs ----------------

func (s *SQLiteStore) InsertLogs(ctx context.Context, sessionID string, entries []model.LogEntry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO logs(session_id, seq, ts, level, category, message, metadata, file, line, func)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	inserted := 0
	for i := range entries {
		e := &entries[i]
		meta, err := marshalMetadata(e.Metadata)
		if err != nil {
			return inserted, err
		}
		res, err := stmt.ExecContext(ctx, sessionID, e.Seq, e.Timestamp, int(e.Level),
			nullString(e.Category), e.Message, nullString(meta),
			nullString(e.File), nullInt(e.Line), nullString(e.Function))
		if err != nil {
			return inserted, err
		}
		n, _ := res.RowsAffected()
		inserted += int(n)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, nowMS(), sessionID); err != nil {
		return inserted, err
	}
	return inserted, tx.Commit()
}

func (s *SQLiteStore) QueryLogs(ctx context.Context, opts QueryLogsOpts) ([]model.LogEntry, error) {
	q := `SELECT l.id, l.session_id, l.seq, l.ts, l.level, l.category, l.message,
			l.metadata, l.file, l.line, l.func
		FROM logs l`
	var (
		where []string
		args  []any
	)
	if opts.BundleID != "" {
		q += " INNER JOIN sessions s ON s.id = l.session_id"
		where = append(where, "s.bundle_id=?")
		args = append(args, opts.BundleID)
	}
	if opts.SessionID != "" {
		where = append(where, "l.session_id=?")
		args = append(args, opts.SessionID)
	}
	if opts.MinLevel != nil {
		where = append(where, "l.level >= ?")
		args = append(args, int(*opts.MinLevel))
	}
	if opts.Category != "" {
		where = append(where, "l.category=?")
		args = append(args, opts.Category)
	}
	if opts.Since > 0 {
		where = append(where, "l.ts >= ?")
		args = append(args, nowMS()-opts.Since)
	}
	if opts.From > 0 {
		where = append(where, "l.ts >= ?")
		args = append(args, opts.From)
	}
	if opts.To > 0 {
		where = append(where, "l.ts <= ?")
		args = append(args, opts.To)
	}
	if opts.Grep != "" {
		where = append(where, "l.message LIKE ?")
		args = append(args, "%"+opts.Grep+"%")
	}
	if opts.Cursor > 0 {
		if strings.EqualFold(opts.Order, "asc") {
			where = append(where, "l.id > ?")
		} else {
			where = append(where, "l.id < ?")
		}
		args = append(args, opts.Cursor)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	if strings.EqualFold(opts.Order, "asc") {
		q += " ORDER BY l.id ASC"
	} else {
		q += " ORDER BY l.id DESC"
	}
	if opts.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	return s.queryLogs(ctx, q, args...)
}

func (s *SQLiteStore) SearchLogs(ctx context.Context, opts SearchLogsOpts) ([]model.LogEntry, error) {
	raw := strings.TrimSpace(opts.Query)
	if raw == "" {
		return nil, errors.New("search query required")
	}
	q := `SELECT l.id, l.session_id, l.seq, l.ts, l.level, l.category, l.message,
			l.metadata, l.file, l.line, l.func
		FROM logs_fts f
		INNER JOIN logs l ON l.id = f.rowid`
	var (
		where = []string{"f.message MATCH ?"}
		args  = []any{ftsEscape(raw)}
	)
	if opts.BundleID != "" {
		q += " INNER JOIN sessions s ON s.id = l.session_id"
		where = append(where, "s.bundle_id=?")
		args = append(args, opts.BundleID)
	}
	q += " WHERE " + strings.Join(where, " AND ")
	q += " ORDER BY l.id DESC"
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	q += fmt.Sprintf(" LIMIT %d", limit)
	return s.queryLogs(ctx, q, args...)
}

func (s *SQLiteStore) PruneBefore(ctx context.Context, ts int64) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM logs WHERE ts < ?`, ts)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *SQLiteStore) Reset(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, stmt := range []string{
		`DELETE FROM logs`,
		`DELETE FROM sessions`,
		`DELETE FROM logs_fts`,
		`DELETE FROM sqlite_sequence WHERE name='logs'`,
	} {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ---------------- helpers ----------------

func (s *SQLiteStore) queryLogs(ctx context.Context, q string, args ...any) ([]model.LogEntry, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.LogEntry
	for rows.Next() {
		e, err := scanLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(r rowScanner) (*model.Session, error) {
	var (
		sess                                                              model.Session
		deviceName, deviceKind, platform, osVersion, appVer, appBuild     sql.NullString
		metadata                                                          sql.NullString
	)
	if err := r.Scan(&sess.ID, &sess.BundleID, &sess.DeviceID, &deviceName, &deviceKind,
		&platform, &osVersion, &appVer, &appBuild,
		&sess.StartedAt, &sess.LastSeenAt, &metadata); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sess.DeviceName = deviceName.String
	sess.DeviceKind = deviceKind.String
	sess.Platform = platform.String
	sess.OSVersion = osVersion.String
	sess.AppVersion = appVer.String
	sess.AppBuild = appBuild.String
	if metadata.Valid && metadata.String != "" {
		m, err := unmarshalMetadata(metadata.String)
		if err != nil {
			return nil, err
		}
		sess.Metadata = m
	}
	return &sess, nil
}

func scanLog(r rowScanner) (*model.LogEntry, error) {
	var (
		e                            model.LogEntry
		category, metadata, file, fn sql.NullString
		line                         sql.NullInt64
		levelI                       int
	)
	if err := r.Scan(&e.ID, &e.SessionID, &e.Seq, &e.Timestamp, &levelI, &category,
		&e.Message, &metadata, &file, &line, &fn); err != nil {
		return nil, err
	}
	e.Level = model.Level(levelI)
	e.Category = category.String
	e.File = file.String
	e.Function = fn.String
	if line.Valid {
		e.Line = int(line.Int64)
	}
	if metadata.Valid && metadata.String != "" {
		m, err := unmarshalMetadata(metadata.String)
		if err != nil {
			return nil, err
		}
		e.Metadata = m
	}
	return &e, nil
}

func marshalMetadata(m map[string]string) (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalMetadata(s string) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func nowMS() int64 { return time.Now().UnixMilli() }

// ftsEscape protects a user-supplied query against FTS5's special characters.
// If the input has no whitespace and contains any character outside [A-Za-z0-9_],
// we wrap it in double quotes to force literal phrase matching. Multi-token
// inputs are passed through (callers can use FTS operators on purpose) but
// embedded double quotes are doubled to keep the parser happy.
func ftsEscape(s string) string {
	if strings.ContainsAny(s, " \t\n") {
		return strings.ReplaceAll(s, `"`, `""`)
	}
	for _, r := range s {
		isAlnum := (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_'
		if !isAlnum {
			return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		}
	}
	return s
}

// ErrNotFound is returned when a record is missing.
var ErrNotFound = errors.New("not found")
