// Package consoletc15 checks CONSOLE-TC-15: the account and target bounds are
// the ones the control plane reports, the fifth session is refused without
// evicting an older one, ending frees a slot, and the idle bound comes from the
// session view unchanged by renewal (CONSOLE-C-13, KOSD-C-94).
package consoletc15

import (
	"context"
	"fmt"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestAccountAndTargetBoundsAreEnforced checks a fifth creation is refused, the
// four live sessions keep their state, and an end frees the slot.
func TestAccountAndTargetBoundsAreEnforced(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true, IdleMinutes: 10})
	ctx := context.Background()
	var sessions []*terminal.Session

	for index := 0; index < 4; index++ {
		target := terminal.Target{
			Type: terminal.TargetNode,
			ID:   fmt.Sprintf("node-%d", index),
			UID:  fmt.Sprintf("uid-node-%d", index),
		}
		session, err := client.Open(ctx, target)
		if err != nil {
			t.Fatalf("open session %d: %v", index+1, err)
		}
		if session.View().Usage.Account != index+1 {
			t.Fatalf("account usage = %d, want %d", session.View().Usage.Account, index+1)
		}
		sessions = append(sessions, session)
	}

	_, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-4", UID: "uid-node-4"})
	apiError, ok := err.(*terminal.APIError)
	if !ok || apiError.Code != "terminal_session_limit" {
		t.Fatalf("fifth session error = %v, want terminal_session_limit", err)
	}
	for _, session := range sessions {
		if session.State() != terminal.StateConnecting {
			t.Fatalf("existing session state = %q, want connecting", session.State())
		}
	}

	if err := sessions[0].End(ctx); err != nil {
		t.Fatalf("end first session: %v", err)
	}
	if _, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-4", UID: "uid-node-4"}); err != nil {
		t.Fatalf("open after release: %v", err)
	}
	if surface.SessionCount() != 5 {
		t.Fatalf("sessions = %d, want four live plus the ended one", surface.SessionCount())
	}
}

// TestIdleBoundComesFromTheSessionView checks renewal does not change the
// session's idle bound and an idle timeout keeps its framework code.
func TestIdleBoundComesFromTheSessionView(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true, IdleMinutes: 10})
	ctx := context.Background()
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if session.View().IdleTimeoutMinutes != 10 {
		t.Fatalf("idle minutes = %d, want 10", session.View().IdleTimeoutMinutes)
	}
	if err := session.Renew(ctx); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if session.View().IdleTimeoutMinutes != 10 {
		t.Fatalf("idle minutes after renewal = %d, want the unchanged 10", session.View().IdleTimeoutMinutes)
	}

	surface.SetFailure("status", 409, "terminal_idle_timeout", "空闲超时", "connect")
	if _, err := session.Status(ctx); err == nil {
		t.Fatal("an idle timeout must be reported")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_idle_timeout" {
		t.Fatalf("idle timeout error = %v, want terminal_idle_timeout", err)
	}
	if session.State() != terminal.StateClosed || session.CanInput() {
		t.Fatalf("idle session = %q, want closed and no input", session.State())
	}
}
