// Package consoletc17 checks CONSOLE-TC-17: a repeated input sequence returns
// the original receipt without a second delivery, the audit read is metadata
// for the same session, and an unavailable audit refuses input and ends the
// session (CONSOLE-C-15).
package consoletc17

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestDuplicateSequenceIsAnsweredOnce checks one batch is delivered once even
// when the browser repeats its sequence.
func TestDuplicateSequenceIsAnsweredOnce(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	first, err := session.SendSequence(ctx, 1, []byte("echo multi\nline\n"))
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	repeat, err := session.SendSequence(ctx, 1, []byte("echo multi\nline\n"))
	if err != nil {
		t.Fatalf("repeat send: %v", err)
	}
	if !repeat.Duplicate || repeat.Sequence != first.Sequence || repeat.Result != first.Result {
		t.Fatalf("repeat = %+v, want the original receipt marked duplicate", repeat)
	}
	if surface.InputCalls() != 1 {
		t.Fatalf("input deliveries = %d, want the batch delivered once", surface.InputCalls())
	}

	page, err := client.Audit(ctx, session.View().SessionID, "")
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if len(page.Items) == 0 || page.Items[0].SessionID != session.View().SessionID || page.Items[0].Event != "establish" {
		t.Fatalf("audit page = %+v, want the session's establish metadata", page)
	}
}

// TestAuditOutageRefusesInput checks an unavailable audit ends the session and
// refuses a new one (KOSD-C-92).
func TestAuditOutageRefusesInput(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	surface.SetAuditDown(true)
	if _, err := session.Send(ctx, []byte("id\n")); err == nil {
		t.Fatal("input without audit must be refused")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_audit_unavailable" {
		t.Fatalf("input error = %v, want terminal_audit_unavailable", err)
	}
	if session.CanInput() || session.State() != terminal.StateClosed {
		t.Fatalf("session = %q, want closed after the audit outage", session.State())
	}

	if _, err := client.Create(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-b", UID: "uid-node-b"}); err == nil {
		t.Fatal("a new session must be refused while audit is unavailable")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_audit_unavailable" {
		t.Fatalf("create error = %v, want terminal_audit_unavailable", err)
	}
}
