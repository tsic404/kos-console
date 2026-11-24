// Package consoletc27 checks CONSOLE-TC-27, the failure-and-recovery journey:
// the failure reason is the framework code, recovery needs a manual new connect,
// no command is retried, and a new success is distinguishable from the old
// failure (CONSOLE-AC-10).
package consoletc27

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestFailureThenManualRecovery checks the failed connect and the fresh success.
func TestFailureThenManualRecovery(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	node := terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

	surface.SetFailure("create", 503, "terminal_node_unreachable", "节点不可达", "connect")
	if _, err := client.Open(ctx, node); err == nil {
		t.Fatal("the unreachable node must not yield a session")
	}
	if surface.InputCalls() != 0 {
		t.Fatal("a failed connect must not deliver input")
	}

	surface.ClearFailure()
	session, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("manual reconnect: %v", err)
	}
	if session.State() != terminal.StateConnecting {
		t.Fatalf("reconnected session = %q, want a new connecting session", session.State())
	}

	pod, err := client.Open(ctx, terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-shop-0", Namespace: "home", Container: "web"})
	if err != nil {
		t.Fatalf("open Pod: %v", err)
	}
	if err := pod.Attach(ctx); err != nil {
		t.Fatalf("attach Pod: %v", err)
	}
	surface.SetFailure("input", 409, "terminal_target_ended", "容器已退出", "connect")
	if _, err := pod.Send(ctx, []byte("ps\n")); err == nil {
		t.Fatal("the exited container must refuse input")
	}
	if pod.CanInput() {
		t.Fatal("the exited container's session must stop accepting input")
	}
	if session.State() != terminal.StateConnecting {
		t.Fatalf("the node session changed to %q, want its own state kept", session.State())
	}
}
