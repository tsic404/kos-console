package terminal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

const (
	consoleToken = "console-session"
	extension    = "kos-console"
)

// newServer serves one scripted terminal surface and returns its address.
func newServer(t *testing.T, surface *fixtures.Terminal) string {
	t.Helper()
	server := httptest.NewServer(surface.Handler())
	t.Cleanup(server.Close)
	return server.URL
}

// start builds a client over a scripted terminal surface and returns it with the
// surface it talks to.
func start(t *testing.T, options fixtures.Options) (*terminal.Client, *fixtures.Terminal) {
	t.Helper()
	options.ConsoleToken = consoleToken
	if options.Extension == "" {
		options.Extension = extension
	}
	surface := fixtures.NewTerminal(options)
	client, err := terminal.New(newServer(t, surface), extension, consoleToken)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	return client, surface
}

// nodeTarget is one node the cases connect to.
func nodeTarget() terminal.Target {
	return terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}
}

// TestCreateDeliversTheSessionAndOneToken checks that creation asks for the
// component and the target and answers with the session view and the one
// plaintext token delivery (KOSD-C-90, KOSD-C-91).
func TestCreateDeliversTheSessionAndOneToken(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: true})

	response, err := client.Create(context.Background(), nodeTarget())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if response.SessionID == "" || response.State != terminal.StateConnecting {
		t.Fatalf("view = %+v, want a connecting session with an identifier", response.SessionView)
	}
	if response.Token == "" {
		t.Fatal("the first creation must deliver the one plaintext token")
	}
	if response.Limits.Account != 4 || response.Limits.Target != 4 {
		t.Fatalf("limits = %+v, want the fixed account and target bounds", response.Limits)
	}
	if response.Usage.Account != 1 || response.Usage.Target != 1 {
		t.Fatalf("usage = %+v, want the created session to occupy both counts", response.Usage)
	}
}

// TestCreateRejectsATargetWithoutIdentity checks that the client refuses a
// creation whose target carries no immutable identity, which is what stops a
// same-name replacement from inheriting a session (KOSD-C-95).
func TestCreateRejectsATargetWithoutIdentity(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: true})

	target := nodeTarget()
	target.UID = ""
	_, err := client.Create(context.Background(), target)
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("create error = %v, want an APIError", err)
	}
	if apiError.Code != "invalid_field" {
		t.Fatalf("code = %q, want invalid_field", apiError.Code)
	}
}

// TestInactiveComponentIsRefused checks that a disabled component cannot create
// a session (KOSD-F-16).
func TestInactiveComponentIsRefused(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: false})

	_, err := client.Create(context.Background(), nodeTarget())
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("create error = %v, want an APIError", err)
	}
	if apiError.Code != "capability_missing" {
		t.Fatalf("code = %q, want capability_missing", apiError.Code)
	}
}

// TestLifecycleRoutes checks the attach, input, status, renew, end, audit and
// node routes and the credential each one carries (KOSD-C-90 to KOSD-C-96).
func TestLifecycleRoutes(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	created, err := client.Create(ctx, nodeTarget())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	attached, err := client.Attach(ctx, created.SessionID, created.Token)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if attached.State != terminal.StateConnected {
		t.Fatalf("attach state = %q, want connected", attached.State)
	}

	receipt, err := client.Input(ctx, created.SessionID, created.Token, 1, []byte("uptime\n"))
	if err != nil {
		t.Fatalf("input: %v", err)
	}
	if receipt.Sequence != 1 || receipt.Result != "accepted" {
		t.Fatalf("receipt = %+v, want sequence 1 accepted", receipt)
	}

	status, err := client.Status(ctx, created.SessionID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.State != terminal.StateConnected || status.TargetUID != "uid-node-a" {
		t.Fatalf("status = %+v, want the connected node session", status)
	}

	renewed, err := client.Renew(ctx, created.SessionID)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed.Token == "" || renewed.Token == created.Token {
		t.Fatal("renew must deliver a new token")
	}

	page, err := client.Audit(ctx, created.SessionID, "")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(page.Items) == 0 || page.Items[0].Event != "establish" {
		t.Fatalf("audit page = %+v, want an establish event", page)
	}

	nodes, err := client.Nodes(ctx)
	if err != nil {
		t.Fatalf("nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes = %+v, want the empty inventory this surface was shaped with", nodes)
	}

	ended, err := client.End(ctx, created.SessionID)
	if err != nil {
		t.Fatalf("end: %v", err)
	}
	if ended.State != terminal.StateClosed {
		t.Fatalf("end state = %q, want closed", ended.State)
	}
}

// TestTokenNeverEntersTheURL checks the token is carried only in the
// Authorization header and never in the address (CONSOLE-C-14).
func TestTokenNeverEntersTheURL(t *testing.T) {
	surface := fixtures.NewTerminal(fixtures.Options{Active: true, ConsoleToken: consoleToken, Extension: extension})
	var seen *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Clone(r.Context())
		surface.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	client, err := terminal.New(server.URL, extension, consoleToken)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	created, err := client.Create(context.Background(), nodeTarget())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := client.Attach(context.Background(), created.SessionID, created.Token); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := client.Input(context.Background(), created.SessionID, created.Token, 1, []byte("x")); err != nil {
		t.Fatalf("input: %v", err)
	}

	if seen == nil || !strings.Contains(seen.URL.String(), "/input") {
		t.Fatalf("captured request = %v, want the input route", seen)
	}
	if strings.Contains(seen.URL.String(), created.Token) {
		t.Fatal("the terminal token entered the URL")
	}
	if got := seen.Header.Get("Authorization"); got != "Bearer "+created.Token {
		t.Fatalf("authorization = %q, want the bearer token", got)
	}
}
