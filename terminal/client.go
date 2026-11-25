// Package terminal is the author-side exit of the control plane's terminal
// session surface (KOSD-C-90 to KOSD-C-96). One call builds one request and
// decodes one answer: the client keeps no state, never retries, and never
// pages on its own. A failure reaches the caller as the envelope the control
// plane sent, field for field.
package terminal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Target identifies one session's target (KOSD-C-95). A node carries empty
// namespace and container strings.
type Target struct {
	// Type is node or pod.
	Type string `json:"targetType"`
	// ID is the object's metadata.name.
	ID string `json:"targetId"`
	// UID is the object's metadata.uid.
	UID string `json:"targetUid"`
	// Namespace scopes a Pod; empty for a node.
	Namespace string `json:"namespace"`
	// Container is the selected Pod container; empty for a node.
	Container string `json:"container"`
}

// Target types (KOSD-C-95).
const (
	TargetNode = "node"
	TargetPod  = "pod"
)

// Limits and Usage are the per-account and per-target session bounds and the
// occupancy one response reports (KOSD-C-90, KOSD-C-94).
type (
	Limits struct {
		Account int `json:"account"`
		Target  int `json:"target"`
	}
	Usage struct {
		Account int `json:"account"`
		Target  int `json:"target"`
	}
)

// SessionView is one session as the control plane reports it (KOSD-C-90).
type SessionView struct {
	SessionID          string    `json:"sessionId"`
	TargetType         string    `json:"targetType"`
	TargetID           string    `json:"targetId"`
	TargetUID          string    `json:"targetUid"`
	Namespace          string    `json:"namespace"`
	Container          string    `json:"container"`
	ExecutionIdentity  string    `json:"executionIdentity"`
	State              string    `json:"state"`
	Reason             string    `json:"reason,omitempty"`
	ExpiresAt          time.Time `json:"expiresAt"`
	IdleTimeoutMinutes int       `json:"idleTimeoutMinutes"`
	Limits             Limits    `json:"limits"`
	Usage              Usage     `json:"usage"`
}

// Session states (KOSD-C-90). Only Connected accepts input.
const (
	StateConnecting = "connecting"
	StateConnected  = "connected"
	StateClosed     = "closed"
	StateFailed     = "failed"
)

// CreateRequest is one session creation (KOSD-C-90, KOSD-C-95). The target
// fields are flat, as the control plane accepts them; a node submits an empty
// namespace and container.
type CreateRequest struct {
	Extension  string `json:"extension"`
	TargetType string `json:"targetType"`
	TargetID   string `json:"targetId"`
	TargetUID  string `json:"targetUid"`
	Namespace  string `json:"namespace"`
	Container  string `json:"container"`
}

// createResponse carries the session view and the one plaintext token
// delivery. A repeated creation answers the existing session without a second
// token, so Token is empty on that path (KOSD-C-91).
type CreateResponse struct {
	SessionView
	Token string `json:"token,omitempty"`
}

// inputRequest is one input batch (KOSD-C-92).
type InputRequest struct {
	Sequence uint64 `json:"sequence"`
	Data     string `json:"data"`
}

// inputReceipt is the executor's answer to one batch. Result is the receipt
// only; the caller never treats it as a claim that a command ran (KOSD-C-92).
type InputReceipt struct {
	Sequence  uint64 `json:"sequence"`
	Result    string `json:"result"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Output    string `json:"output,omitempty"`
}

// auditEvent is one terminal audit record (KOSD-C-96): metadata only.
type AuditEvent struct {
	EventID           int64     `json:"eventId"`
	Actor             string    `json:"actor"`
	Extension         string    `json:"extension"`
	SessionID         string    `json:"sessionId,omitempty"`
	TargetType        string    `json:"targetType"`
	TargetID          string    `json:"targetId"`
	TargetUID         string    `json:"targetUid"`
	Namespace         string    `json:"namespace"`
	Container         string    `json:"container"`
	ExecutionIdentity string    `json:"executionIdentity"`
	Event             string    `json:"event"`
	Result            string    `json:"result"`
	Sequence          *uint64   `json:"sequence,omitempty"`
	Receipt           string    `json:"receipt,omitempty"`
	OccurredAt        time.Time `json:"occurredAt"`
}

// auditPage is one page of a session's audit events (KOSD-C-96).
type AuditPage struct {
	Items      []AuditEvent `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
}

// NodeTarget is one node inventory item (KOSD-C-95).
type NodeTarget struct {
	TargetType          string    `json:"targetType"`
	TargetID            string    `json:"targetId"`
	TargetUID           string    `json:"targetUid"`
	Namespace           string    `json:"namespace"`
	Container           string    `json:"container"`
	ExecutionIdentities []string  `json:"executionIdentities"`
	ObservedAt          time.Time `json:"observedAt"`
	Stale               bool      `json:"stale"`
}

// Client calls one control plane's terminal surface. It holds no state between
// calls and is safe for concurrent use.
type Client struct {
	baseURL   string
	extension string
	console   string
	http      *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client a call uses; nil keeps the default.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		if httpClient != nil {
			client.http = httpClient
		}
	}
}

// New builds a client for the control plane at baseURL, acting for the terminal
// component named extension. consoleToken is the console session the calls that
// need one are made with; the token-only routes carry their own terminal token
// instead.
func New(baseURL, extension, consoleToken string, options ...Option) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("terminal client: base URL %q: %w", baseURL, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("terminal client: base URL %q needs a scheme and a host", baseURL)
	}
	client := &Client{
		baseURL:   strings.TrimRight(parsed.String(), "/"),
		extension: extension,
		console:   consoleToken,
		http:      http.DefaultClient,
	}
	for _, option := range options {
		option(client)
	}
	return client, nil
}

// Create requests one terminal session (KOSD-C-90). The control plane decides
// whether the account may connect and answers with the session's view and, on
// the first creation, its one-time token.
func (c *Client) Create(ctx context.Context, target Target) (CreateResponse, error) {
	var response CreateResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/terminal/sessions", nil, CreateRequest{
		Extension:  c.extension,
		TargetType: target.Type,
		TargetID:   target.ID,
		TargetUID:  target.UID,
		Namespace:  target.Namespace,
		Container:  target.Container,
	}, c.console, &response); err != nil {
		return CreateResponse{}, err
	}
	return response, nil
}

// Status reads one session (KOSD-C-90).
func (c *Client) Status(ctx context.Context, sessionID string) (SessionView, error) {
	var view SessionView
	if err := c.do(ctx, http.MethodGet, sessionPath(sessionID), nil, nil, c.console, &view); err != nil {
		return SessionView{}, err
	}
	return view, nil
}

// Renew issues a new token for one session (KOSD-C-91). Renewal re-checks the
// session's scope and current authorization and does not reset the idle bound
// (KOSD-C-94).
func (c *Client) Renew(ctx context.Context, sessionID string) (CreateResponse, error) {
	var response CreateResponse
	if err := c.do(ctx, http.MethodPost, renewPath(sessionID), nil, nil, c.console, &response); err != nil {
		return CreateResponse{}, err
	}
	return response, nil
}

// Attach connects one session with a terminal token (KOSD-C-91). The token is
// the only credential on this path; no console session is consulted.
func (c *Client) Attach(ctx context.Context, sessionID, token string) (SessionView, error) {
	var view SessionView
	if err := c.do(ctx, http.MethodPost, attachPath(sessionID), nil, nil, token, &view); err != nil {
		return SessionView{}, err
	}
	return view, nil
}

// Input delivers one input batch with a terminal token (KOSD-C-92).
func (c *Client) Input(ctx context.Context, sessionID, token string, sequence uint64, data []byte) (InputReceipt, error) {
	request := InputRequest{Sequence: sequence, Data: base64.StdEncoding.EncodeToString(data)}
	var receipt InputReceipt
	if err := c.do(ctx, http.MethodPost, inputPath(sessionID), nil, request, token, &receipt); err != nil {
		return InputReceipt{}, err
	}
	return receipt, nil
}

// End ends one session (KOSD-C-90). A repeated end has no second effect.
func (c *Client) End(ctx context.Context, sessionID string) (SessionView, error) {
	var view SessionView
	if err := c.do(ctx, http.MethodDelete, sessionPath(sessionID), nil, nil, c.console, &view); err != nil {
		return SessionView{}, err
	}
	return view, nil
}

// Audit reads one page of a session's audit events (KOSD-C-96).
func (c *Client) Audit(ctx context.Context, sessionID, cursor string) (AuditPage, error) {
	query := url.Values{}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	var page AuditPage
	if err := c.do(ctx, http.MethodGet, auditPath(sessionID), query, nil, c.console, &page); err != nil {
		return AuditPage{}, err
	}
	return page, nil
}

// Nodes reads the node inventory the component may offer (KOSD-C-95). Only the
// global Owner may read it, which the control plane enforces.
func (c *Client) Nodes(ctx context.Context) ([]NodeTarget, error) {
	query := url.Values{}
	query.Set("extension", c.extension)
	var page struct {
		Items []NodeTarget `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/terminal/nodes", query, nil, c.console, &page); err != nil {
		return nil, err
	}
	return page.Items, nil
}

// do sends exactly one request and decodes its answer; a failure is decoded
// rather than retried.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, credential string, into any) error {
	target := c.baseURL + path
	if encoded := query.Encode(); encoded != "" {
		target += "?" + encoded
	}
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(encoded)
	}
	token := credential
	request, err := http.NewRequestWithContext(ctx, method, target, payload)
	if err != nil {
		return err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode > 299 {
		return newAPIError(response.StatusCode, answer)
	}
	if into == nil || len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	return json.Unmarshal(answer, into)
}

// sessionPath addresses one session.
func sessionPath(sessionID string) string {
	return "/api/v1/terminal/sessions/" + url.PathEscape(sessionID)
}

// renewPath addresses one session's renewal.
func renewPath(sessionID string) string {
	return sessionPath(sessionID) + "/renew"
}

// attachPath addresses one session's attach.
func attachPath(sessionID string) string {
	return sessionPath(sessionID) + "/attach"
}

// inputPath addresses one session's input.
func inputPath(sessionID string) string {
	return sessionPath(sessionID) + "/input"
}

// auditPath addresses one session's audit read.
func auditPath(sessionID string) string {
	return sessionPath(sessionID) + "/audit"
}
