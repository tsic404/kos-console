// Package consoletc28 checks CONSOLE-TC-28, the restart-and-cleanup journey:
// the delivered input is not replayed, reopening and reinstalling both require a
// new session, and audit stays readable after the component leaves
// (CONSOLE-AC-14, CONSOLE-C-16).
package consoletc28

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestRestartAndReinstallNeedANewSession checks the journey's steps.
func TestRestartAndReinstallNeedANewSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	node := terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

	first, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := first.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := first.Send(ctx, []byte("touch /tmp/marker\n")); err != nil {
		t.Fatalf("send: %v", err)
	}
	if surface.InputCalls() != 1 {
		t.Fatalf("input deliveries = %d, want 1", surface.InputCalls())
	}

	surface.SetFailure("input", 409, "terminal_target_ended", "执行服务已重启", "connect")
	if _, err := first.Send(ctx, []byte("touch /tmp/marker\n")); err == nil {
		t.Fatal("the old session must refuse input after the restart")
	}
	delivered := surface.InputCalls()
	if _, err := first.Send(ctx, []byte("touch /tmp/marker\n")); err == nil {
		t.Fatal("repeated input must stay refused")
	}
	if surface.InputCalls() != delivered {
		t.Fatal("the old input must not be replayed")
	}

	surface.ClearFailure()
	second, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if second.View().SessionID == first.View().SessionID {
		t.Fatal("reopening must create a new session")
	}
	if err := second.Attach(ctx); err != nil {
		t.Fatalf("attach reopened: %v", err)
	}
	if err := second.End(ctx); err != nil {
		t.Fatalf("end reopened: %v", err)
	}

	surface.SetActive(false)
	if _, err := client.Create(ctx, node); err == nil {
		t.Fatal("an uninstalled component must refuse a new session")
	}
	if page, err := client.Audit(ctx, first.View().SessionID, ""); err != nil || len(page.Items) == 0 {
		t.Fatalf("audit after uninstall = %+v, %v, want the retained events", page, err)
	}

	surface.SetActive(true)
	third, err := client.Create(ctx, node)
	if err != nil {
		t.Fatalf("create after reinstall: %v", err)
	}
	if third.SessionID == first.View().SessionID || third.SessionID == second.View().SessionID {
		t.Fatal("reinstall must require a new session")
	}
}
