// Package consoletc18 checks CONSOLE-TC-18: after a disconnect or executor
// restart the old session refuses input, the delivered batch is not replayed,
// the old token cannot attach, and continuing requires a new session
// (CONSOLE-C-11).
package consoletc18

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestDisconnectDoesNotReplayInput checks one delivered batch stays delivered
// once while the old session and token are refused afterwards.
func TestDisconnectDoesNotReplayInput(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := session.Send(ctx, []byte("touch /tmp/marker\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if surface.InputCalls() != 1 {
		t.Fatalf("input deliveries = %d, want 1", surface.InputCalls())
	}

	surface.SetFailure("input", 409, "terminal_target_ended", "执行服务已重启", "connect")
	if _, err := session.Send(ctx, []byte("touch /tmp/marker\n")); err == nil {
		t.Fatal("the old session must refuse input after the restart")
	}
	delivered := surface.InputCalls()
	if _, err := session.Send(ctx, []byte("touch /tmp/marker\n")); err == nil {
		t.Fatal("repeated input on the old session must stay refused")
	}
	if surface.InputCalls() != delivered {
		t.Fatal("the old batch must not be re-delivered after the restart")
	}
	if session.View().Reason == "" {
		t.Fatal("the failed session must carry the reason it stopped")
	}

	if _, err := client.Attach(ctx, session.View().SessionID, "any-token"); err == nil {
		t.Fatal("the old session must not be attachable after the restart")
	}
	fresh, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open after recovery: %v", err)
	}
	if fresh.View().SessionID == session.View().SessionID {
		t.Fatal("continuing must create a new session, not revive the old one")
	}
	if fresh.State() != terminal.StateConnecting {
		t.Fatalf("new session state = %q, want connecting until confirmed", fresh.State())
	}
}
