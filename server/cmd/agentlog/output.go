package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/agentlogger/agentlog/internal/client"
	"github.com/agentlogger/agentlog/internal/model"
)

// formatLogText renders a single log entry as a one-line human-readable string.
//
// With session info resolved:
//
//	[2026-05-19 17:45:12.345] [ERROR  ] [com.foo.bar@iPhone Sim] [Network] message…
//
// Without (lookup miss or no SessionID):
//
//	[2026-05-19 17:45:12.345] [ERROR  ] [Network] message…
func formatLogText(e *model.LogEntry, sess *model.Session) string {
	ts := time.UnixMilli(e.Timestamp).Format("2006-01-02 15:04:05.000")
	level := strings.ToUpper(e.Level.String())
	var who string
	if label := sessionLabel(sess); label != "" {
		who = " [" + label + "]"
	}
	var cat string
	if e.Category != "" {
		cat = " [" + e.Category + "]"
	}
	return fmt.Sprintf("[%s] [%-7s]%s%s %s", ts, level, who, cat, e.Message)
}

// sessionLabel returns a short bundle@device identifier for inline display,
// or one half if the other is missing. Empty if the session is nil or
// carries neither field.
func sessionLabel(s *model.Session) string {
	if s == nil {
		return ""
	}
	device := s.DeviceName
	if device == "" {
		device = s.DeviceID
	}
	switch {
	case s.BundleID != "" && device != "":
		return s.BundleID + "@" + device
	case s.BundleID != "":
		return s.BundleID
	case device != "":
		return device
	}
	return ""
}

// formatSessionText renders a session summary.
func formatSessionText(s *model.Session) string {
	started := time.UnixMilli(s.StartedAt).Format("2006-01-02 15:04:05")
	last := time.UnixMilli(s.LastSeenAt).Format("15:04:05")
	device := s.DeviceID
	if s.DeviceName != "" {
		device = s.DeviceName
	}
	return fmt.Sprintf("%s  %s  %s  %s  started=%s lastSeen=%s",
		s.ID, s.BundleID, device, defaultStr(s.DeviceKind, "?"), started, last)
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// writeNDJSON encodes v as a single line of JSON followed by newline.
func writeNDJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(v)
}

// sessionCache memoises GetSession lookups for the duration of one command
// run. Negative lookups are cached as nil so a missing session id isn't
// refetched on every poll tick.
type sessionCache struct {
	c    *client.Client
	sess map[string]*model.Session
}

func newSessionCache(c *client.Client) *sessionCache {
	return &sessionCache{c: c, sess: map[string]*model.Session{}}
}

// get returns the cached session for id, fetching once on first miss. Safe
// for nil receivers and empty ids — both return nil silently.
func (sc *sessionCache) get(ctx context.Context, id string) *model.Session {
	if sc == nil || id == "" {
		return nil
	}
	if s, ok := sc.sess[id]; ok {
		return s
	}
	s, err := sc.c.GetSession(ctx, id)
	if err != nil {
		sc.sess[id] = nil
		return nil
	}
	sc.sess[id] = s
	return s
}
