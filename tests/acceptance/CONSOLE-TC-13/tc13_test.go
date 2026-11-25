// Package consoletc13 checks CONSOLE-TC-13: an unreachable node and an ended Pod
// container are reported as the distinct failures KOSD-C-12 names, never as a
// connected session, and one failed session leaves the other's state intact
// (CONSOLE-C-12).
package consoletc13

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestUnreachableNodeIsAConnectFailure checks a node failure happens before any
// session exists and keeps its own code.
func TestUnreachableNodeIsAConnectFailure(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	surface.SetFailure("create", 503, "terminal_node_unreachable", "节点不可达", "connect")

	session, err := client.Open(context.Background(), terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err == nil || session != nil {
		t.Fatal("an unreachable node must not yield a session")
	}
	apiError, ok := err.(*terminal.APIError)
	if !ok || apiError.Code != "terminal_node_unreachable" {
		t.Fatalf("error = %v, want terminal_node_unreachable", err)
	}
	if surface.SessionCount() != 0 {
		t.Fatal("a failed connect must not create a session")
	}
}

// TestEndedContainerFailsOnlyItsSession checks a container that exits ends its
// own session while the other session stays connected.
func TestEndedContainerFailsOnlyItsSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	pod := terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-shop-0", Namespace: "home", Container: "web"}

	doomed, err := client.Open(ctx, pod)
	if err != nil {
		t.Fatalf("open first: %v", err)
	}
	if err := doomed.Attach(ctx); err != nil {
		t.Fatalf("attach first: %v", err)
	}
	other, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open second: %v", err)
	}
	if err := other.Attach(ctx); err != nil {
		t.Fatalf("attach second: %v", err)
	}

	surface.SetFailure("input", 409, "terminal_target_ended", "容器已退出", "connect")
	if _, err := doomed.Send(ctx, []byte("ps\n")); err == nil {
		t.Fatal("an ended container must refuse input")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "terminal_target_ended" {
		t.Fatalf("error = %v, want terminal_target_ended", err)
	}
	if doomed.CanInput() {
		t.Fatal("the ended session must stop accepting input")
	}
	if doomed.State() != terminal.StateFailed {
		t.Fatalf("ended state = %q, want failed", doomed.State())
	}
	if doomed.View().Reason != "terminal_target_ended" {
		t.Fatalf("ended reason = %q, want terminal_target_ended", doomed.View().Reason)
	}
	if !other.CanInput() {
		t.Fatal("the other session must keep its connected state")
	}
	if other.State() != terminal.StateConnected {
		t.Fatalf("other state = %q, want connected", other.State())
	}
}
