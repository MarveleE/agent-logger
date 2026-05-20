package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/agentlogger/agentlog/internal/model"
	"github.com/agentlogger/agentlog/internal/store"
)

const maxBatchSize = 1024 // entries per ingest call

// ----- Health -----

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"ts": time.Now().UnixMilli(),
	})
}

// ----- Sessions -----

type createSessionRequest struct {
	ID         string            `json:"id"` // optional — server will generate if omitted
	BundleID   string            `json:"bundleId"`
	DeviceID   string            `json:"deviceId"`
	DeviceName string            `json:"deviceName,omitempty"`
	DeviceKind string            `json:"deviceKind,omitempty"`
	Platform   string            `json:"platform,omitempty"`
	OSVersion  string            `json:"osVersion,omitempty"`
	AppVersion string            `json:"appVersion,omitempty"`
	AppBuild   string            `json:"appBuild,omitempty"`
	StartedAt  int64             `json:"startedAt,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req createSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.BundleID == "" || req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "bundleId and deviceId are required")
		return
	}
	now := time.Now().UnixMilli()
	if req.ID == "" {
		req.ID = uuid.NewString()
	}
	if req.StartedAt == 0 {
		req.StartedAt = now
	}
	sess := &model.Session{
		ID:         req.ID,
		BundleID:   req.BundleID,
		DeviceID:   req.DeviceID,
		DeviceName: req.DeviceName,
		DeviceKind: req.DeviceKind,
		Platform:   req.Platform,
		OSVersion:  req.OSVersion,
		AppVersion: req.AppVersion,
		AppBuild:   req.AppBuild,
		StartedAt:  req.StartedAt,
		LastSeenAt: now,
		Metadata:   req.Metadata,
	}
	if err := s.store.CreateSession(r.Context(), sess); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sess)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := store.ListSessionsOpts{
		BundleID: q.Get("bundle"),
		DeviceID: q.Get("device"),
		Platform: q.Get("platform"),
		Limit:    intParam(q.Get("limit"), 50),
	}
	if active, _ := strconv.ParseBool(q.Get("active")); active {
		opts.ActiveWithin = parseDurationMs(q.Get("activeWithin"), 5*60*1000)
	}
	out, err := s.store.ListSessions(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if out == nil {
		out = []model.Session{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (s *Server) handleLatestSession(w http.ResponseWriter, r *http.Request) {
	bundle := r.URL.Query().Get("bundle")
	if bundle == "" {
		writeError(w, http.StatusBadRequest, "bundle query param required")
		return
	}
	sess, err := s.store.LatestSession(r.Context(), bundle)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no session found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	sess, err := s.store.GetSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.store.UpdateSessionLastSeen(r.Context(), id, time.Now().UnixMilli()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- Logs -----

func (s *Server) handleIngestLogs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var batch model.LogBatch
	if err := decodeJSON(r, &batch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(batch.Batch) == 0 {
		writeJSON(w, http.StatusAccepted, map[string]any{"inserted": 0})
		return
	}
	if len(batch.Batch) > maxBatchSize {
		writeError(w, http.StatusRequestEntityTooLarge, "batch too large")
		return
	}
	inserted, err := s.store.InsertLogs(r.Context(), id, batch.Batch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"inserted": inserted,
		"received": len(batch.Batch),
	})
}

func (s *Server) handleQueryLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := store.QueryLogsOpts{
		SessionID: q.Get("session"),
		BundleID:  q.Get("bundle"),
		Category:  q.Get("category"),
		Grep:      q.Get("grep"),
		Limit:     intParam(q.Get("limit"), 200),
		Order:     q.Get("order"),
		Cursor:    int64Param(q.Get("cursor"), 0),
	}
	if lv := q.Get("level"); lv != "" {
		if parsed, ok := model.ParseLevel(lv); ok {
			opts.MinLevel = &parsed
		} else {
			writeError(w, http.StatusBadRequest, "invalid level")
			return
		}
	}
	if since := q.Get("since"); since != "" {
		opts.Since = parseDurationMs(since, 0)
	}
	if from := q.Get("from"); from != "" {
		opts.From = int64Param(from, 0)
	}
	if to := q.Get("to"); to != "" {
		opts.To = int64Param(to, 0)
	}
	logs, err := s.store.QueryLogs(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []model.LogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

func (s *Server) handleSearchLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opts := store.SearchLogsOpts{
		Query:    q.Get("q"),
		BundleID: q.Get("bundle"),
		Limit:    intParam(q.Get("limit"), 100),
	}
	if opts.Query == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	logs, err := s.store.SearchLogs(r.Context(), opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if logs == nil {
		logs = []model.LogEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs})
}

// ----- Maintenance -----

type pruneRequest struct {
	Before string `json:"before"` // e.g. "7d", "12h", or unix ms
}

func (s *Server) handlePrune(w http.ResponseWriter, r *http.Request) {
	var req pruneRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Before == "" {
		writeError(w, http.StatusBadRequest, "before is required")
		return
	}
	cutoff := parseCutoff(req.Before)
	if cutoff == 0 {
		writeError(w, http.StatusBadRequest, "invalid before value")
		return
	}
	n, err := s.store.PruneBefore(r.Context(), cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pruned": n, "before": cutoff})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Reset(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ----- helpers -----

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func intParam(s string, def int) int {
	if s == "" {
		return def
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func int64Param(s string, def int64) int64 {
	if s == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}

// parseDurationMs accepts go-style duration strings (e.g. "5m", "30s", "1h") or
// raw integers (interpreted as milliseconds).
func parseDurationMs(s string, def int64) int64 {
	if s == "" {
		return def
	}
	if d, err := time.ParseDuration(s); err == nil {
		return d.Milliseconds()
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	return def
}

func parseCutoff(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if d, err := time.ParseDuration(s); err == nil {
		return time.Now().Add(-d).UnixMilli()
	}
	// Also accept "Nd" (days)
	if strings.HasSuffix(s, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil {
			return time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
		}
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v
	}
	return 0
}

// ensureContext silences linter when ctx is otherwise unused.
var _ = context.TODO
