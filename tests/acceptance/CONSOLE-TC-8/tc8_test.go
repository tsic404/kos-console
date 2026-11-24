// Package consoletc8 checks CONSOLE-TC-8: choosing a node target, cancelling
// before confirmation, creating a session with the fields KOSD-C-90 fixes, and
// seeing the node's identity, bounds and occupancy. A deep link only prefills a
// target; it never creates a session on its own (CONSOLE-C-8, CONSOLE-C-9).
package consoletc8

import (
	"context"
	"testing"

	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestNodeChoiceConfirmsAndCreatesOneSession drives the node journey up to the
// point the browser's confirmation dialog submits (CONSOLE-C-8, CONSOLE-C-9).
func TestNodeChoiceConfirmsAndCreatesOneSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{
		Active: true,
		Nodes:  fixtures.NodeInventory(fixtures.Node("node-a", "uid-node-a")),
	})
	ctx := context.Background()
	target := terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

	if surface.SessionCount() != 0 {
		t.Fatalf("sessions before confirmation = %d, want none", surface.SessionCount())
	}

	list, err := client.Nodes(ctx)
	if err != nil {
		t.Fatalf("read node targets: %v", err)
	}
	if len(list) != 1 || list[0].TargetUID != "uid-node-a" {
		t.Fatalf("node targets = %+v, want the chosen node only", list)
	}
	if surface.SessionCount() != 0 {
		t.Fatal("reading a target list must not create a session")
	}

	response, err := client.Create(ctx, target)
	if err != nil {
		t.Fatalf("create node session: %v", err)
	}
	if response.Namespace != "" || response.Container != "" {
		t.Fatalf("node target fields = namespace %q container %q, want both empty", response.Namespace, response.Container)
	}
	if response.TargetType != terminal.TargetNode || response.TargetID != "node-a" || response.TargetUID != "uid-node-a" {
		t.Fatalf("session target = %+v, want the confirmed node", response.SessionView)
	}
	if response.ExecutionIdentity != "root@node" {
		t.Fatalf("execution identity = %q, want root@node", response.ExecutionIdentity)
	}
	if response.Limits.Account != 4 || response.Limits.Target != 4 || response.Usage.Account != 1 || response.Usage.Target != 1 {
		t.Fatalf("bounds/occupancy = limits %+v usage %+v, want 4/4 and 1/1", response.Limits, response.Usage)
	}
	if response.IdleTimeoutMinutes != 10 {
		t.Fatalf("idle minutes = %d, want the control plane's 10", response.IdleTimeoutMinutes)
	}
	if response.Token == "" {
		t.Fatal("the first creation must deliver the one token")
	}
	if surface.SessionCount() != 1 {
		t.Fatalf("sessions after confirmation = %d, want one", surface.SessionCount())
	}
}

// TestNodeTargetRejectsNamespaceAndContainer checks the create body keeps a
// node's namespace and container empty, as KOSD-C-95 requires.
func TestNodeTargetRejectsNamespaceAndContainer(t *testing.T) {
	client, _ := fixtures.Start(t, fixtures.Options{Active: true})
	_, err := client.Create(context.Background(), terminal.Target{
		Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a",
		Namespace: "home", Container: "web",
	})
	apiError, ok := err.(*terminal.APIError)
	if !ok || apiError.Code != "invalid_field" {
		t.Fatalf("node with namespace/container error = %v, want invalid_field", err)
	}
}

// TestGuestHasNoEntry checks the page admits no Guest, which is the condition
// under which the desktop entry is not rendered (CONSOLE-C-10).
func TestGuestHasNoEntry(t *testing.T) {
	if app.TerminalPage().Allows(write.RoleGuest) {
		t.Fatal("a Guest must not see the terminal entry")
	}
}
