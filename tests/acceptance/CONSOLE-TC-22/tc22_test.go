// Package consoletc22 checks CONSOLE-TC-22: with no permitted target the page
// carries empty lists and no session, and one failed session leaves the other
// session's target and state distinguishable. The narrow-screen layout itself is
// a browser asset check (CONSOLE-C-17).
package consoletc22

import (
	"context"
	"testing"

	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestEmptyInventoryShowsNoTargetAndStartsNothing checks the read side of the
// empty state: no target rows, the page's own reason, and no session.
func TestEmptyInventoryShowsNoTargetAndStartsNothing(t *testing.T) {
	readings := &app.Readings{Targets: &app.ObjectTargets{Source: fixtures.Objects{}, Pods: fixtures.Pods{}}}
	session := identity.Session{Username: "lin", Role: write.RoleMember}

	nodes := fixtures.ReadValue(t, readings, session, app.ItemNodeTargets).(map[string]any)
	if items := nodes["items"].([]app.NodeTarget); len(items) != 0 {
		t.Fatalf("node items = %+v, want none", items)
	}
	if nodes["reason"] == "" {
		t.Fatal("the empty node list must state the page's reason")
	}
	if nodes["empty"] != app.EmptyTargetsReason {
		t.Fatalf("empty statement = %v, want %q", nodes["empty"], app.EmptyTargetsReason)
	}

	pods := fixtures.ReadValue(t, readings, session, app.ItemPodTargets).(map[string]any)
	if items := pods["items"].([]app.PodTarget); len(items) != 0 {
		t.Fatalf("pod items = %+v, want none", items)
	}
	if pods["reason"] == "" {
		t.Fatal("the empty Pod list must state the page's reason")
	}
	if pods["empty"] != "无可连接目标" {
		t.Fatalf("empty statement = %v, want the design's 无可连接目标", pods["empty"])
	}
}

// TestOneFailureKeepsTheOtherSessionDistinguishable checks a failed session does
// not overwrite the other session's target or state.
func TestOneFailureKeepsTheOtherSessionDistinguishable(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	pod, err := client.Open(ctx, terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-shop-0", Namespace: "home", Container: "web"})
	if err != nil {
		t.Fatalf("open Pod: %v", err)
	}
	node, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open node: %v", err)
	}
	if err := pod.Attach(ctx); err != nil {
		t.Fatalf("attach Pod: %v", err)
	}
	if err := node.Attach(ctx); err != nil {
		t.Fatalf("attach node: %v", err)
	}

	surface.SetFailure("input", 409, "terminal_target_ended", "容器已退出", "connect")
	if _, err := pod.Send(ctx, []byte("ps\n")); err == nil {
		t.Fatal("the ended Pod container must refuse input")
	}
	if node.State() != terminal.StateConnected || node.View().TargetUID != "uid-node-a" {
		t.Fatalf("node session = %+v, want its own connected state", node.View())
	}
	if pod.View().TargetUID != "uid-shop-0" || pod.View().Reason != "terminal_target_ended" {
		t.Fatalf("Pod session = %+v, want its target and failure reason kept", pod.View())
	}
}
