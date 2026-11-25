// Package consoletc12 checks CONSOLE-TC-12: forged, foreign, expired and
// component tokens are refused with the failure KOSD-C-91 names, and a revoked
// account cannot keep sending input on a connected session (CONSOLE-C-10).
package consoletc12

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// node is the target every call in this case uses.
var node = terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

// TestForeignTokensAreRefused checks a forged token, another session's token and
// a missing console login are each refused without a session view.
func TestForeignTokensAreRefused(t *testing.T) {
	client, _ := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	first, err := client.Create(ctx, node)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := client.Create(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-b", UID: "uid-node-b"})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}

	if code := attachFailure(t, client, first.SessionID, "forged-token"); code != "terminal_unauthenticated" {
		t.Fatalf("forged token code = %q, want terminal_unauthenticated", code)
	}
	if code := attachFailure(t, client, second.SessionID, first.Token); code != "terminal_forbidden" {
		t.Fatalf("foreign session token code = %q, want terminal_forbidden", code)
	}

	expired, _ := fixtures.StartAs(t, fixtures.Options{Active: true}, "expired-console-session")
	if _, err := expired.Create(ctx, node); err == nil {
		t.Fatal("an expired console login must not establish a session")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_unauthenticated" {
		t.Fatalf("expired login error = %v, want terminal_unauthenticated", err)
	}

	window, _ := fixtures.StartAs(t, fixtures.Options{Active: true}, first.Token)
	if _, err := window.Create(ctx, node); err == nil {
		t.Fatal("a terminal token must not stand in for a console or window session")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_unauthenticated" {
		t.Fatalf("terminal token as console token error = %v, want terminal_unauthenticated", err)
	}
}

// TestRevokedPermissionEndsTheSession checks revocation surfaces as
// terminal_forbidden and stops every later input.
func TestRevokedPermissionEndsTheSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	session, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	surface.SetFailure("input", 403, "terminal_forbidden", "权限已撤销", "authorize")
	if _, err := session.Send(ctx, []byte("id\n")); err == nil {
		t.Fatal("a revoked account must not keep sending input")
	}
	if session.CanInput() {
		t.Fatal("the session must leave the live set after revocation")
	}
	if _, err := session.Send(ctx, []byte("id\n")); !isSessionError(err, terminal.CodeEnded) {
		t.Fatalf("later send error = %v, want %s", err, terminal.CodeEnded)
	}
}

// attachFailure attaches with one token and returns the refusal code.
func attachFailure(t *testing.T, client *terminal.Client, sessionID, token string) string {
	t.Helper()
	_, err := client.Attach(context.Background(), sessionID, token)
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("attach %s error = %v, want an APIError", sessionID, err)
	}
	return apiError.Code
}

// isSessionError reports whether err is the given state-machine refusal.
func isSessionError(err error, code string) bool {
	sessionError, ok := err.(*terminal.SessionError)
	return ok && sessionError.Code == code
}
