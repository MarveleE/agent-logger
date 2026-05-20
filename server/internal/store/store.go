package store

import (
	"context"

	"github.com/agentlogger/agentlog/internal/model"
)

// Store is the persistence interface. The HTTP layer talks to this, not to SQL
// directly, so we can swap implementations (e.g., to a different embedded DB)
// without touching the API surface.
type Store interface {
	Close() error

	CreateSession(ctx context.Context, s *model.Session) error
	GetSession(ctx context.Context, id string) (*model.Session, error)
	UpdateSessionLastSeen(ctx context.Context, id string, ts int64) error
	ListSessions(ctx context.Context, opts ListSessionsOpts) ([]model.Session, error)
	LatestSession(ctx context.Context, bundleID string) (*model.Session, error)

	// InsertLogs is idempotent on (session_id, seq); returns the count actually inserted.
	InsertLogs(ctx context.Context, sessionID string, entries []model.LogEntry) (int, error)
	QueryLogs(ctx context.Context, opts QueryLogsOpts) ([]model.LogEntry, error)
	SearchLogs(ctx context.Context, opts SearchLogsOpts) ([]model.LogEntry, error)

	PruneBefore(ctx context.Context, ts int64) (int, error)
	Reset(ctx context.Context) error
}

type ListSessionsOpts struct {
	BundleID     string
	DeviceID     string
	Platform     string
	ActiveWithin int64 // ms relative to now; 0 = no filter
	Limit        int
}

type QueryLogsOpts struct {
	SessionID string
	BundleID  string
	MinLevel  *model.Level
	Category  string
	Since     int64 // ms relative to now; mutually exclusive with From/To
	From      int64 // unix ms
	To        int64 // unix ms
	Grep      string
	Limit     int
	Order     string // "asc" | "desc"
	Cursor    int64  // last id seen (exclusive)
}

type SearchLogsOpts struct {
	Query    string
	BundleID string
	Limit    int
}
