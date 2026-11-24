// Package consoletc29 checks CONSOLE-TC-29, the narrow-and-empty journey: an
// empty page states there is nothing to connect to and starts nothing, then a
// real target can connect, take input and disconnect while a second failed
// session keeps its own target and reason. The layout that keeps those controls
// reachable is a browser asset check (CONSOLE-AC-16).
package consoletc29

import (
	"context"
	"testing"

	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestEmptyThenConnectAndFail checks the empty state and the two distinct
// sessions after it.
func TestEmptyThenConnectAndFail(t *testing.T) {
	member := identity.Session{Username: "lin", Role: write.RoleMember}
	empty := &app.Readings{Targets: &app.ObjectTargets{Source: fixtures.Objects{}, Pods: fixtures.Pods{}}}
	pods := fixtures.ReadValue(t, empty, member, app.ItemPodTargets).(map[string]any)
	if items := pods["items"].([]app.PodTarget); len(items) != 0 {
		t.Fatalf("empty Pod list = %+v, want no target", items)
	}
	if pods["empty"] != app.EmptyTargetsReason {
		t.Fatalf("empty statement = %v, want %q", pods["empty"], app.EmptyTargetsReason)
	}

	client, surface := fixtures.Start(t, fixtures.Options{
		Active: true,
		Nodes:  fixtures.NodeInventory(fixtures.Node("node-a", "uid-node-a")),
	})
	ctx := context.Background()
	surface.SetOutput("connected\n")

	node, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("connect node: %v", err)
	}
	if err := node.Attach(ctx); err != nil {
		t.Fatalf("attach node: %v", err)
	}
	if _, err := node.Send(ctx, []byte("id\n")); err != nil {
		t.Fatalf("send: %v", err)
	}

	pod, err := client.Open(ctx, terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-shop-0", Namespace: "home", Container: "web"})
	if err != nil {
		t.Fatalf("connect Pod: %v", err)
	}
	if err := pod.Attach(ctx); err != nil {
		t.Fatalf("attach Pod: %v", err)
	}
	surface.SetFailure("input", 409, "terminal_target_ended", "容器已退出", "connect")
	if _, err := pod.Send(ctx, []byte("id\n")); err == nil {
		t.Fatal("the exited container must refuse input")
	}

	if node.State() != terminal.StateConnected || node.View().TargetUID != "uid-node-a" {
		t.Fatalf("live session = %+v, want its own connected state", node.View())
	}
	if pod.View().Reason != "terminal_target_ended" || pod.View().TargetUID != "uid-shop-0" {
		t.Fatalf("failed session = %+v, want its own target and reason", pod.View())
	}

	if err := node.End(ctx); err != nil {
		t.Fatalf("disconnect node: %v", err)
	}
	if node.CanInput() {
		t.Fatal("disconnecting must stop input")
	}
}
