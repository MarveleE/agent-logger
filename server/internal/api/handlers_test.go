package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentlogger/agentlog/internal/api"
	"github.com/agentlogger/agentlog/internal/model"
	"github.com/agentlogger/agentlog/internal/store"
)

// newTestServer spins up the full HTTP stack against a temp SQLite file.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	srv := api.New(st, "127.0.0.1:0")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func mustPost(t *testing.T, ts *httptest.Server, path string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+path, "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func mustGet(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func drain(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp := mustGet(t, ts, "/v1/health")
	defer drain(resp)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestSessionLifecycle(t *testing.T) {
	ts := newTestServer(t)

	// create
	resp := mustPost(t, ts, "/v1/sessions", map[string]any{
		"id":         "session-1",
		"bundleId":   "com.example.app",
		"deviceId":   "DEV-1",
		"deviceName": "iPhone 16 Simulator",
		"deviceKind": "simulator",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var created model.Session
	decode(t, resp, &created)
	if created.ID != "session-1" {
		t.Fatalf("id=%q", created.ID)
	}

	// latest
	resp = mustGet(t, ts, "/v1/sessions/latest?bundle=com.example.app")
	if resp.StatusCode != 200 {
		t.Fatalf("latest status=%d", resp.StatusCode)
	}
	var latest model.Session
	decode(t, resp, &latest)
	if latest.ID != "session-1" {
		t.Fatalf("latest id=%q", latest.ID)
	}

	// list
	resp = mustGet(t, ts, "/v1/sessions?bundle=com.example.app")
	var list struct {
		Sessions []model.Session `json:"sessions"`
	}
	decode(t, resp, &list)
	if len(list.Sessions) != 1 {
		t.Fatalf("got %d sessions", len(list.Sessions))
	}

	// 404 on missing
	resp = mustGet(t, ts, "/v1/sessions/latest?bundle=nope")
	if resp.StatusCode != 404 {
		t.Fatalf("expected 404 got %d", resp.StatusCode)
	}
	drain(resp)
}

func TestLogIngestAndQuery(t *testing.T) {
	ts := newTestServer(t)

	// session
	mustPost(t, ts, "/v1/sessions", map[string]any{
		"id":       "s1",
		"bundleId": "com.example.app",
		"deviceId": "DEV",
	}).Body.Close()

	now := time.Now().UnixMilli()
	batch := model.LogBatch{Batch: []model.LogEntry{
		{Seq: 1, Timestamp: now, Level: model.LevelInfo, Message: "hello world"},
		{Seq: 2, Timestamp: now + 10, Level: model.LevelError, Message: "boom"},
		{Seq: 3, Timestamp: now + 20, Level: model.LevelDebug, Message: "trace data"},
	}}
	resp := mustPost(t, ts, "/v1/sessions/s1/logs", batch)
	if resp.StatusCode != 202 {
		t.Fatalf("ingest status=%d", resp.StatusCode)
	}
	var ingest struct {
		Inserted int `json:"inserted"`
		Received int `json:"received"`
	}
	decode(t, resp, &ingest)
	if ingest.Inserted != 3 || ingest.Received != 3 {
		t.Fatalf("ingest=%+v", ingest)
	}

	// idempotency: re-send with same seq -> 0 inserted
	resp = mustPost(t, ts, "/v1/sessions/s1/logs", batch)
	decode(t, resp, &ingest)
	if ingest.Inserted != 0 {
		t.Fatalf("expected 0 re-inserts, got %d", ingest.Inserted)
	}

	// query all
	resp = mustGet(t, ts, "/v1/logs?session=s1")
	var q struct {
		Logs []model.LogEntry `json:"logs"`
	}
	decode(t, resp, &q)
	if len(q.Logs) != 3 {
		t.Fatalf("got %d logs", len(q.Logs))
	}

	// filter: level>=error
	resp = mustGet(t, ts, "/v1/logs?session=s1&level=error")
	decode(t, resp, &q)
	if len(q.Logs) != 1 || q.Logs[0].Message != "boom" {
		t.Fatalf("level filter: %+v", q.Logs)
	}

	// search via FTS
	resp = mustGet(t, ts, "/v1/logs/search?q=hello&bundle=com.example.app")
	decode(t, resp, &q)
	if len(q.Logs) != 1 || q.Logs[0].Message != "hello world" {
		t.Fatalf("search: %+v", q.Logs)
	}

	// grep filter
	resp = mustGet(t, ts, "/v1/logs?session=s1&grep=trace")
	decode(t, resp, &q)
	if len(q.Logs) != 1 || q.Logs[0].Message != "trace data" {
		t.Fatalf("grep: %+v", q.Logs)
	}
}

func TestHeartbeatAndActive(t *testing.T) {
	ts := newTestServer(t)
	mustPost(t, ts, "/v1/sessions", map[string]any{
		"id":       "s1",
		"bundleId": "com.example.app",
		"deviceId": "DEV",
	}).Body.Close()

	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/v1/sessions/s1/heartbeat", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	drain(resp)
	if resp.StatusCode != 204 {
		t.Fatalf("status=%d", resp.StatusCode)
	}

	resp = mustGet(t, ts, "/v1/sessions?active=true&activeWithin=10s")
	defer drain(resp)
	var list struct {
		Sessions []model.Session `json:"sessions"`
	}
	decode(t, resp, &list)
	if len(list.Sessions) != 1 {
		t.Fatalf("active=%d", len(list.Sessions))
	}
}

func TestPruneAndReset(t *testing.T) {
	ts := newTestServer(t)
	mustPost(t, ts, "/v1/sessions", map[string]any{
		"id":       "s1",
		"bundleId": "b",
		"deviceId": "d",
	}).Body.Close()

	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	now := time.Now().UnixMilli()
	mustPost(t, ts, "/v1/sessions/s1/logs", model.LogBatch{Batch: []model.LogEntry{
		{Seq: 1, Timestamp: old, Level: model.LevelInfo, Message: "ancient"},
		{Seq: 2, Timestamp: now, Level: model.LevelInfo, Message: "fresh"},
	}}).Body.Close()

	// prune older than 24h
	resp := mustPost(t, ts, "/v1/db/prune", map[string]any{"before": "24h"})
	var pr struct{ Pruned int `json:"pruned"` }
	decode(t, resp, &pr)
	if pr.Pruned != 1 {
		t.Fatalf("pruned=%d", pr.Pruned)
	}

	resp = mustGet(t, ts, "/v1/logs?session=s1")
	var q struct{ Logs []model.LogEntry `json:"logs"` }
	decode(t, resp, &q)
	if len(q.Logs) != 1 || q.Logs[0].Message != "fresh" {
		t.Fatalf("after prune: %+v", q.Logs)
	}

	// reset everything
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/db/reset", nil)
	resp, _ = http.DefaultClient.Do(req)
	drain(resp)
	if resp.StatusCode != 204 {
		t.Fatalf("reset status=%d", resp.StatusCode)
	}

	resp = mustGet(t, ts, "/v1/sessions")
	decode(t, resp, &struct{ Sessions []model.Session `json:"sessions"` }{})
}

func TestInternalShutdown_LoopbackAllowed(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Post(ts.URL+"/v1/internal/shutdown", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer drain(resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestInternalShutdown_XFFCannotSpoofLoopback(t *testing.T) {
	ts := newTestServer(t)
	// httptest.Server binds to 127.0.0.1 so any peer is loopback — what we
	// really want to verify is that an X-Forwarded-For header doesn't change
	// the decision. Construct a request with a spoofed-looking header and
	// confirm shutdown still works (because we ignore the header on this route).
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/internal/shutdown", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.42") // pretend a public IP
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer drain(resp)
	// Because /v1/internal/* is outside the RealIP middleware, the spoof has no effect.
	// The peer is still 127.0.0.1, so shutdown is allowed.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("XFF spoof leaked into loopback check; status=%d", resp.StatusCode)
	}
}

func TestBatchTooLarge(t *testing.T) {
	ts := newTestServer(t)
	mustPost(t, ts, "/v1/sessions", map[string]any{
		"id": "s1", "bundleId": "b", "deviceId": "d",
	}).Body.Close()

	huge := make([]model.LogEntry, 2000)
	for i := range huge {
		huge[i] = model.LogEntry{Seq: int64(i), Timestamp: time.Now().UnixMilli(), Level: model.LevelInfo, Message: "x"}
	}
	resp := mustPost(t, ts, "/v1/sessions/s1/logs", model.LogBatch{Batch: huge})
	defer drain(resp)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

// keep imports honest if any helper is later dropped
var _ = context.Background
