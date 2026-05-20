package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentlogger/agentlog/internal/api"
	"github.com/agentlogger/agentlog/internal/model"
	"github.com/agentlogger/agentlog/internal/store"
)

// ---- harness ----

// testEnv bundles a temp SQLite store + a live httptest server for the CLI to
// hit via --endpoint.
type testEnv struct {
	t        *testing.T
	store    *store.SQLiteStore
	server   *httptest.Server
	endpoint string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	srv := api.New(st, "127.0.0.1:0")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	return &testEnv{
		t:        t,
		store:    st,
		server:   ts,
		endpoint: ts.URL,
	}
}

// run invokes the agentlog CLI in-process with the given args, returning
// captured stdout, stderr, and the error from RunE.
func (e *testEnv) run(args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	root := newRoot()
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	full := append([]string{"--endpoint", e.endpoint}, args...)
	root.SetArgs(full)
	err = root.Execute()
	return stdoutBuf.String(), stderrBuf.String(), err
}

// runWithCtx runs with an explicit context (for tail which polls).
func (e *testEnv) runWithCtx(ctx context.Context, args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	root := newRoot()
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	full := append([]string{"--endpoint", e.endpoint}, args...)
	root.SetArgs(full)
	err = root.ExecuteContext(ctx)
	return stdoutBuf.String(), stderrBuf.String(), err
}

// seed inserts a small canonical fixture and returns its session id.
func (e *testEnv) seed(bundleID string) string {
	e.t.Helper()
	now := time.Now().UnixMilli()
	sess := &model.Session{
		ID:         "s-" + bundleID,
		BundleID:   bundleID,
		DeviceID:   "DEV-1",
		DeviceName: "iPhone Sim",
		DeviceKind: "simulator",
		Platform:   "ios",
		AppVersion: "1.0.0",
		AppBuild:   "42",
		StartedAt:  now - 1000,
		LastSeenAt: now,
	}
	if err := e.store.CreateSession(context.Background(), sess); err != nil {
		e.t.Fatalf("CreateSession: %v", err)
	}
	entries := []model.LogEntry{
		{Seq: 1, Timestamp: now - 30, Level: model.LevelInfo, Category: "Network",
			Message: "GET /users started"},
		{Seq: 2, Timestamp: now - 20, Level: model.LevelDebug, Message: "parsing response"},
		{Seq: 3, Timestamp: now - 10, Level: model.LevelError, Category: "Network",
			Message: "decoding failed: missing key"},
		{Seq: 4, Timestamp: now, Level: model.LevelWarning, Message: "cache miss for users-page-1"},
	}
	if _, err := e.store.InsertLogs(context.Background(), sess.ID, entries); err != nil {
		e.t.Fatalf("InsertLogs: %v", err)
	}
	return sess.ID
}

// addSession is a small helper for tests that need additional sessions.
func (e *testEnv) addSession(id, bundleID string, lastSeenOffset time.Duration) {
	e.t.Helper()
	now := time.Now().UnixMilli()
	sess := &model.Session{
		ID: id, BundleID: bundleID, DeviceID: "DEV-2",
		StartedAt:  now - 5000,
		LastSeenAt: now + lastSeenOffset.Milliseconds(),
	}
	if err := e.store.CreateSession(context.Background(), sess); err != nil {
		e.t.Fatalf("CreateSession: %v", err)
	}
}

// ---- version / root ----

func TestVersionPrintsVersion(t *testing.T) {
	e := newTestEnv(t)
	out, _, err := e.run("version")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("expected non-empty version output, got %q", out)
	}
}

func TestRootHelpListsTopLevelCommands(t *testing.T) {
	e := newTestEnv(t)
	out, _, err := e.run("--help")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, want := range []string{"sessions", "logs", "tail", "search", "start", "stop", "status", "instances", "db", "version"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q\noutput:\n%s", want, out)
		}
	}
}

// ---- server status ----

func TestServerStatus_StoppedWhenNoPID(t *testing.T) {
	e := newTestEnv(t)
	dataDir := t.TempDir()
	out, _, err := e.run("status", "--data-dir", dataDir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "stopped") {
		t.Fatalf("expected 'stopped' in output, got: %s", out)
	}
}

func TestServerStatus_RunningWhenPIDLive(t *testing.T) {
	e := newTestEnv(t)
	dataDir := t.TempDir()
	pidPath := filepath.Join(dataDir, "agentlog.pid")
	if err := writeFile(pidPath, fmt.Sprintf("%d", currentPID())); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("status", "--data-dir", dataDir)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "running") {
		t.Fatalf("expected 'running' in output, got: %s", out)
	}
	if !strings.Contains(out, fmt.Sprintf("pid=%d", currentPID())) {
		t.Errorf("expected pid line, got: %s", out)
	}
}

// ---- sessions ----

func TestSessionsList_EmptyReportsNoSessions(t *testing.T) {
	e := newTestEnv(t)
	_, stderr, err := e.run("sessions", "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stderr, "no sessions") {
		t.Fatalf("expected 'no sessions' on stderr, got: %s", stderr)
	}
}

func TestSessionsList_SeededShowsBundle(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("sessions", "list")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "com.example.app") {
		t.Fatalf("missing bundle in output: %s", out)
	}
}

func TestSessionsList_JSONOneLinePerSession(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	e.addSession("s-other", "com.example.other", 0)
	out, _, err := e.run("sessions", "list", "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d: %s", len(lines), out)
	}
	for _, line := range lines {
		var sess model.Session
		if err := json.Unmarshal([]byte(line), &sess); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
	}
}

func TestSessionsList_FilterByPlatform(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.ios")
	// Add a non-iOS session
	if err := e.store.CreateSession(context.Background(), &model.Session{
		ID: "s-web", BundleID: "com.example.web", DeviceID: "WEB",
		Platform:   "web",
		StartedAt:  time.Now().UnixMilli(),
		LastSeenAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("sessions", "list", "--platform", "ios", "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "com.example.ios") {
		t.Errorf("missing iOS session: %s", out)
	}
	if strings.Contains(out, "com.example.web") {
		t.Errorf("web session leaked into --platform=ios: %s", out)
	}
}

func TestSession_RoundTripsPlatformAndAppBuild(t *testing.T) {
	e := newTestEnv(t)
	id := e.seed("com.example.app")
	out, _, err := e.run("sessions", "show", id, "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var sess model.Session
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &sess); err != nil {
		t.Fatal(err)
	}
	if sess.Platform != "ios" {
		t.Errorf("platform=%q, want ios", sess.Platform)
	}
	if sess.AppBuild != "42" {
		t.Errorf("appBuild=%q, want 42", sess.AppBuild)
	}
	if sess.AppVersion != "1.0.0" {
		t.Errorf("appVersion=%q, want 1.0.0", sess.AppVersion)
	}
}

func TestSessionsList_FilterByBundle(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	e.addSession("s-other", "com.example.other", 0)
	out, _, err := e.run("sessions", "list", "--bundle", "com.example.app")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(out, "com.example.other") {
		t.Fatalf("filter leaked other bundle: %s", out)
	}
	if !strings.Contains(out, "com.example.app") {
		t.Fatalf("missing target bundle: %s", out)
	}
}

func TestSessionsList_ActiveFiltersOldSessions(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.fresh.app") // last_seen = now
	// Stale session — last_seen well in the past
	stale := &model.Session{
		ID: "s-stale", BundleID: "com.stale.app", DeviceID: "X",
		StartedAt:  time.Now().Add(-1 * time.Hour).UnixMilli(),
		LastSeenAt: time.Now().Add(-1 * time.Hour).UnixMilli(),
	}
	if err := e.store.CreateSession(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("sessions", "list", "--active", "--active-within", "30s")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "com.fresh.app") {
		t.Errorf("active session missing: %s", out)
	}
	if strings.Contains(out, "com.stale.app") {
		t.Errorf("stale session leaked: %s", out)
	}
}

func TestSessionsLatest_RequiresBundle(t *testing.T) {
	e := newTestEnv(t)
	_, _, err := e.run("sessions", "latest")
	if err == nil || !strings.Contains(err.Error(), "--bundle") {
		t.Fatalf("expected --bundle required error, got: %v", err)
	}
}

func TestSessionsLatest_HappyPath(t *testing.T) {
	e := newTestEnv(t)
	id := e.seed("com.example.app")
	out, _, err := e.run("sessions", "latest", "--bundle", "com.example.app", "--json")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var sess model.Session
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &sess); err != nil {
		t.Fatalf("bad JSON: %v\noutput: %s", err, out)
	}
	if sess.ID != id {
		t.Fatalf("expected session %q got %q", id, sess.ID)
	}
}

func TestSessionsLatest_NotFoundReturnsError(t *testing.T) {
	e := newTestEnv(t)
	_, _, err := e.run("sessions", "latest", "--bundle", "com.missing.app")
	if err == nil {
		t.Fatalf("expected error for missing bundle")
	}
}

func TestSessionsShow_HappyAndMissing(t *testing.T) {
	e := newTestEnv(t)
	id := e.seed("com.example.app")
	out, _, err := e.run("sessions", "show", id)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, id) {
		t.Fatalf("missing session id: %s", out)
	}

	_, _, err = e.run("sessions", "show", "no-such-id")
	if err == nil {
		t.Fatalf("expected error for unknown id")
	}
}

// ---- logs ----

func TestLogs_DefaultText(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "decoding failed") {
		t.Errorf("missing error msg: %s", out)
	}
	if !strings.Contains(out, "[ERROR  ]") {
		t.Errorf("missing level prefix: %s", out)
	}
	if !strings.Contains(out, "[Network]") {
		t.Errorf("missing category: %s", out)
	}
	if !strings.Contains(out, "[com.example.app@iPhone Sim]") {
		t.Errorf("missing bundle@device identifier prefix: %s", out)
	}
}

func TestLogs_FilterByLevel(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--level", "error")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "decoding failed") {
		t.Errorf("expected error log, got: %s", out)
	}
	if strings.Contains(out, "parsing response") {
		t.Errorf("debug entry leaked: %s", out)
	}
}

func TestLogs_FilterByCategory(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--category", "Network")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.Contains(out, "parsing response") {
		t.Errorf("non-Network entry leaked: %s", out)
	}
	if !strings.Contains(out, "[Network]") {
		t.Errorf("missing Network category: %s", out)
	}
}

func TestLogs_FilterBySince(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--since", "5m")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "decoding failed") {
		t.Errorf("expected recent entries: %s", out)
	}
}

func TestLogs_FilterByGrep(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--grep", "cache")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "cache miss") {
		t.Errorf("expected match: %s", out)
	}
	if strings.Contains(out, "decoding failed") {
		t.Errorf("non-match leaked: %s", out)
	}
}

func TestLogs_OrderAsc(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--order", "asc")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 || !strings.Contains(lines[0], "GET /users") {
		t.Fatalf("expected oldest first, got: %s", out)
	}
}

func TestLogs_Limit(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--limit", "2")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 entries, got %d: %s", len(lines), out)
	}
}

func TestLogs_JSON(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("logs", "--bundle", "com.example.app", "--json", "--limit", "10")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var entry model.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("invalid JSON: %v\nline: %s", err, line)
		}
	}
}

// ---- tail ----

func TestTail_PrintsSeedAndExits(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()
	out, stderr, _ := e.runWithCtx(ctx, "tail", "--bundle", "com.example.app", "--interval", "200ms")
	if !strings.Contains(out, "decoding failed") {
		t.Errorf("expected seed in output, got:\nstdout:\n%s\nstderr:\n%s", out, stderr)
	}
	if !strings.Contains(stderr, "tailing session") {
		t.Errorf("expected banner on stderr, got:\n%s", stderr)
	}
}

// ---- search ----

func TestSearch_FTSHit(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("search", "decoding", "--bundle", "com.example.app")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(out, "decoding failed") {
		t.Errorf("missing hit: %s", out)
	}
}

func TestSearch_NoMatchReturnsEmpty(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	out, _, err := e.run("search", "completely-not-present", "--bundle", "com.example.app")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected empty output, got: %s", out)
	}
}

func TestSearch_RequiresArg(t *testing.T) {
	e := newTestEnv(t)
	_, _, err := e.run("search")
	if err == nil {
		t.Fatalf("expected arg required error")
	}
}

// ---- db ----

func TestDB_PruneRequiresBefore(t *testing.T) {
	e := newTestEnv(t)
	_, _, err := e.run("db", "prune")
	if err == nil || !strings.Contains(err.Error(), "--before") {
		t.Fatalf("expected --before required, got: %v", err)
	}
}

func TestDB_PruneDeletesOldEntries(t *testing.T) {
	e := newTestEnv(t)
	sessID := "s-prune"
	if err := e.store.CreateSession(context.Background(), &model.Session{
		ID: sessID, BundleID: "b", DeviceID: "d",
		StartedAt: time.Now().UnixMilli(), LastSeenAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour).UnixMilli()
	recent := time.Now().UnixMilli()
	if _, err := e.store.InsertLogs(context.Background(), sessID, []model.LogEntry{
		{Seq: 1, Timestamp: old, Level: model.LevelInfo, Message: "ancient"},
		{Seq: 2, Timestamp: recent, Level: model.LevelInfo, Message: "fresh"},
	}); err != nil {
		t.Fatal(err)
	}
	_, stderr, err := e.run("db", "prune", "--before", "24h")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stderr, "pruned 1") {
		t.Errorf("expected 'pruned 1', got: %s", stderr)
	}
	// Verify only 'fresh' remains
	out, _, err := e.run("logs", "--session", sessID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fresh") || strings.Contains(out, "ancient") {
		t.Errorf("unexpected post-prune state: %s", out)
	}
}

func TestDB_ResetClearsEverything(t *testing.T) {
	e := newTestEnv(t)
	e.seed("com.example.app")
	_, stderr, err := e.run("db", "reset", "--yes")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(stderr, "database reset") {
		t.Errorf("expected reset confirmation, got: %s", stderr)
	}
	// Sessions should be empty now
	_, listStderr, err := e.run("sessions", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listStderr, "no sessions") {
		t.Errorf("expected no sessions after reset")
	}
}

// ---- server-down error path ----

func TestServerDown_FriendlyMessage(t *testing.T) {
	e := newTestEnv(t)
	// Point at an unrouted port
	root := newRoot()
	stdoutBuf := &bytes.Buffer{}
	stderrBuf := &bytes.Buffer{}
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.SetArgs([]string{"--endpoint", "http://127.0.0.1:1", "sessions", "list"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected connection error")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("expected friendly 'not running' message, got: %v", err)
	}
	_ = e // keep harness referenced to share the import block
}

// ---- helpers ----

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func currentPID() int {
	return os.Getpid()
}
