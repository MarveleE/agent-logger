package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/agentlogger/agentlog/internal/model"
)

// formatLogText renders a single log entry as a one-line human-readable string.
//
//	[2026-05-19 17:45:12.345] [ERROR  ] [Network] message…
func formatLogText(e *model.LogEntry) string {
	ts := time.UnixMilli(e.Timestamp).Format("2006-01-02 15:04:05.000")
	level := strings.ToUpper(e.Level.String())
	var cat string
	if e.Category != "" {
		cat = " [" + e.Category + "]"
	}
	return fmt.Sprintf("[%s] [%-7s]%s %s", ts, level, cat, e.Message)
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
