// Package hostclient is the web server's HTTP client for the session host
// (design §6): loopback only, bearer token read from the host's token file,
// provider-neutral DTOs. It deliberately knows nothing about Claude - it
// forwards the host's JSON shapes as they are.
package hostclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gpayer/mcp-task-manager/internal/sessionapi"
)

const defaultTimeout = 30 * time.Second

// Client talks to the session host's API. The token file is re-read on every
// request: the host owns it and may rotate or recreate it.
type Client struct {
	base      string
	tokenFile string
	hc        *http.Client
	// Streams outlive any per-request deadline, so they use their own
	// client without a Timeout.
	streamHC *http.Client
}

// New builds a client for a host address ("host:port" or a full URL) and the
// host's token file.
func New(addr, tokenFile string) *Client {
	return NewWithClient(addr, tokenFile, nil)
}

// NewWithClient overrides the HTTP client; a nil one gets the default with a
// timeout. Tests pass the httptest server's client.
func NewWithClient(addr, tokenFile string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return &Client{
		base:      strings.TrimSuffix(addr, "/"),
		tokenFile: tokenFile,
		hc:        hc,
		streamHC:  &http.Client{},
	}
}

// Health is GET /healthz, the one route that needs no token.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return h, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return h, transportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return h, decodeError(resp)
	}
	err = json.NewDecoder(resp.Body).Decode(&h)
	return h, err
}

// Create starts a session; the host answers 201 with {"id","status"}.
func (c *Client) Create(ctx context.Context, spec sessionapi.Spec) (Created, error) {
	var out Created
	body, err := json.Marshal(spec)
	if err != nil {
		return out, err
	}
	resp, err := c.do(ctx, http.MethodPost, "/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Created{}, &sessionapi.Error{Code: "internal", Message: "the host's answer could not be read: " + err.Error()}
	}
	return out, nil
}

// List returns every session the host knows; filtering by workspace is the
// caller's job (the web server filters for its own project root).
func (c *Client) List(ctx context.Context) ([]Meta, error) {
	resp, err := c.do(ctx, http.MethodGet, "/v1/sessions", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var payload struct {
		Sessions []Meta `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, &sessionapi.Error{Code: "internal", Message: "the host's answer could not be read: " + err.Error()}
	}
	return payload.Sessions, nil
}

// Get returns the session's meta plus its unanswered question/permission
// requests (the {"meta","pending_requests"} envelope).
func (c *Client) Get(ctx context.Context, id string) (View, error) {
	var view View
	resp, err := c.do(ctx, http.MethodGet, "/v1/sessions/"+id, nil)
	if err != nil {
		return view, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		return View{}, &sessionapi.Error{Code: "internal", Message: "the host's answer could not be read: " + err.Error()}
	}
	return view, nil
}

// Events opens the session's SSE stream, forwarding lastID as Last-Event-ID
// so a reconnecting browser resumes where it left off. The caller owns the
// returned body and must close it.
func (c *Client) Events(ctx context.Context, id, lastID string) (io.ReadCloser, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/sessions/"+id+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if lastID != "" {
		req.Header.Set("Last-Event-ID", lastID)
	}
	resp, err := c.streamHC.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp.Body, nil
}

// Send delivers one follow-up user message.
func (c *Client) Send(ctx context.Context, id, text string) error {
	body, err := json.Marshal(sessionapi.Message{Text: text})
	if err != nil {
		return err
	}
	return c.postNoBody(ctx, "/v1/sessions/"+id+"/messages", body)
}

// Answer responds to a pending question/permission request.
func (c *Client) Answer(ctx context.Context, id string, a sessionapi.Answer) error {
	body, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return c.postNoBody(ctx, "/v1/sessions/"+id+"/answers", body)
}

// Stop terminates the session.
func (c *Client) Stop(ctx context.Context, id string) error {
	return c.postNoBody(ctx, "/v1/sessions/"+id+"/stop", nil)
}

func (c *Client) postNoBody(ctx context.Context, path string, body []byte) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	resp, err := c.do(ctx, http.MethodPost, path, reader)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// do sends one authenticated request. Transport failures are
// host_unavailable; a non-2xx answer passes the host's structured error
// through so the browser sees the same code and message the host wrote.
func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	token, err := c.token()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, transportError(err)
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp, nil
}

func (c *Client) token() (string, error) {
	data, err := os.ReadFile(c.tokenFile)
	if err != nil {
		return "", &sessionapi.Error{Code: sessionapi.ErrCodeHostUnavailable,
			Message: "cannot read the session host token file: " + err.Error()}
	}
	return strings.TrimSpace(string(data)), nil
}

// transportError maps every network-level failure to host_unavailable; a
// caller's cancelled context is not a host failure and passes through.
func transportError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &sessionapi.Error{Code: sessionapi.ErrCodeHostUnavailable,
		Message: "the session host is unreachable: " + err.Error()}
}

// decodeError parses the host's {"error":{"code","message"}} envelope; an
// unreadable body becomes a plain internal error.
func decodeError(resp *http.Response) error {
	var payload struct {
		Error *sessionapi.Error `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err == nil &&
		payload.Error != nil && payload.Error.Code != "" {
		return payload.Error
	}
	return &sessionapi.Error{Code: "internal", Message: "the session host answered " + resp.Status}
}

// Health is GET /healthz's answer: liveness, version, and whether the host
// serves sessions at all.
type Health struct {
	OK              bool   `json:"ok"`
	Version         string `json:"version"`
	SessionsEnabled bool   `json:"sessions_enabled"`
}

// Created is the host's answer to POST /v1/sessions: 201 with the new
// session's id and starting status.
type Created struct {
	ID     string            `json:"id"`
	Status sessionapi.Status `json:"status"`
}

// Meta mirrors the host's session record (the meta.json shape, design §5).
// The host's own Meta type lives in internal/sessionhost, which this
// package must not import, so the wire shape is mirrored here.
type Meta struct {
	ID            string            `json:"id"`
	Title         string            `json:"title"`
	Status        sessionapi.Status `json:"status"`
	WorkspacePath string            `json:"workspace_path"`
	TaskID        string            `json:"task_id,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
	LastSeq       int64             `json:"last_seq"`
}

// View mirrors the host's GET /v1/sessions/{id} answer: the meta plus the
// unanswered question/permission requests ({"meta","pending_requests"}).
type View struct {
	Meta            Meta               `json:"meta"`
	PendingRequests []sessionapi.Event `json:"pending_requests"`
}
