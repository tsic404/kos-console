// Package consoletc19 checks CONSOLE-TC-19: refreshing the page state does not
// end or reset the session, closing ends the token, and a repeated close has no
// second effect (CONSOLE-C-11).
package consoletc19

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestRefreshKeepsTheSessionAndCloseIsIdempotent checks the two observable
// halves of the case.
func TestRefreshKeepsTheSessionAndCloseIsIdempotent(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true, IdleMinutes: 10})
	ctx := context.Background()
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	view, err := session.Status(ctx)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if view.State != terminal.StateConnected || view.IdleTimeoutMinutes != 10 {
		t.Fatalf("refreshed view = %+v, want the live session with the unchanged idle bound", view)
	}
	if surface.EndCalls() != 0 {
		t.Fatal("refreshing must not end the session")
	}

	if err := session.End(ctx); err != nil {
		t.Fatalf("end: %v", err)
	}
	if err := session.End(ctx); err != nil {
		t.Fatalf("repeated end: %v", err)
	}
	if surface.EndCalls() != 1 {
		t.Fatalf("end requests = %d, want one", surface.EndCalls())
	}
	if session.CanInput() {
		t.Fatal("a closed session must not accept input")
	}
	if _, err := session.Send(ctx, []byte("id\n")); !isEnded(err) {
		t.Fatalf("send after close = %v, want a state-machine refusal", err)
	}
	if surface.InputCalls() != 0 {
		t.Fatal("a closed session must not deliver input to the target")
	}
}

// isEnded reports whether the session answered that it is no longer live.
func isEnded(err error) bool {
	sessionError, ok := err.(*terminal.SessionError)
	return ok && (sessionError.Code == terminal.CodeEnded || sessionError.Code == terminal.CodeNotConnected)
}
