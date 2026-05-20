// Package client wraps the agentlog HTTP API for use by the CLI.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/agentlogger/agentlog/internal/model"
)

const DefaultEndpoint = "http://127.0.0.1:8765"

// Client is a thin HTTP wrapper around the agentlog server API.
type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	if base == "" {
		base = DefaultEndpoint
	}
	return &Client{
		base: base,
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// ErrServerDown indicates the local server is not reachable.
var ErrServerDown = errors.New("agentlog server is not running (start it with 'agentlog server start' or 'make server-start')")

func (c *Client) Health(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/health", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrServerDown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

type ListSessionsOpts struct {
	Bundle       string
	Device       string
	Platform     string
	Active       bool
	ActiveWithin string
	Limit        int
}

func (c *Client) ListSessions(ctx context.Context, o ListSessionsOpts) ([]model.Session, error) {
	q := url.Values{}
	if o.Bundle != "" {
		q.Set("bundle", o.Bundle)
	}
	if o.Device != "" {
		q.Set("device", o.Device)
	}
	if o.Platform != "" {
		q.Set("platform", o.Platform)
	}
	if o.Active {
		q.Set("active", "true")
		if o.ActiveWithin != "" {
			q.Set("activeWithin", o.ActiveWithin)
		}
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	var out struct {
		Sessions []model.Session `json:"sessions"`
	}
	if err := c.getJSON(ctx, "/v1/sessions?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Sessions, nil
}

func (c *Client) LatestSession(ctx context.Context, bundle string) (*model.Session, error) {
	if bundle == "" {
		return nil, errors.New("bundle is required")
	}
	var sess model.Session
	if err := c.getJSON(ctx, "/v1/sessions/latest?bundle="+url.QueryEscape(bundle), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (c *Client) GetSession(ctx context.Context, id string) (*model.Session, error) {
	var sess model.Session
	if err := c.getJSON(ctx, "/v1/sessions/"+url.PathEscape(id), &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

type QueryLogsOpts struct {
	Session  string
	Bundle   string
	Level    string
	Category string
	Since    string
	From     int64
	To       int64
	Grep     string
	Limit    int
	Order    string // asc | desc
	Cursor   int64
}

func (c *Client) QueryLogs(ctx context.Context, o QueryLogsOpts) ([]model.LogEntry, error) {
	q := url.Values{}
	if o.Session != "" {
		q.Set("session", o.Session)
	}
	if o.Bundle != "" {
		q.Set("bundle", o.Bundle)
	}
	if o.Level != "" {
		q.Set("level", o.Level)
	}
	if o.Category != "" {
		q.Set("category", o.Category)
	}
	if o.Since != "" {
		q.Set("since", o.Since)
	}
	if o.From > 0 {
		q.Set("from", strconv.FormatInt(o.From, 10))
	}
	if o.To > 0 {
		q.Set("to", strconv.FormatInt(o.To, 10))
	}
	if o.Grep != "" {
		q.Set("grep", o.Grep)
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Order != "" {
		q.Set("order", o.Order)
	}
	if o.Cursor > 0 {
		q.Set("cursor", strconv.FormatInt(o.Cursor, 10))
	}
	var out struct {
		Logs []model.LogEntry `json:"logs"`
	}
	if err := c.getJSON(ctx, "/v1/logs?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Logs, nil
}

func (c *Client) SearchLogs(ctx context.Context, query, bundle string, limit int) ([]model.LogEntry, error) {
	q := url.Values{}
	q.Set("q", query)
	if bundle != "" {
		q.Set("bundle", bundle)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Logs []model.LogEntry `json:"logs"`
	}
	if err := c.getJSON(ctx, "/v1/logs/search?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out.Logs, nil
}

func (c *Client) Prune(ctx context.Context, before string) (int, error) {
	body, _ := json.Marshal(map[string]string{"before": before})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/db/prune", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, ErrServerDown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, decodeError(resp)
	}
	var out struct {
		Pruned int `json:"pruned"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.Pruned, nil
}

func (c *Client) Reset(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/db/reset", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrServerDown
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return decodeError(resp)
	}
	return nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrServerDown
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return decodeError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// ErrNotFound is returned for 404 responses.
var ErrNotFound = errors.New("not found")

func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error != "" {
		return fmt.Errorf("server: %s (%d)", e.Error, resp.StatusCode)
	}
	return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
}
