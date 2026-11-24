// Package consoletc24 checks CONSOLE-TC-24, the first-install node journey: the
// installed package contributes the terminal entry in the design's information
// order, and an Owner confirms, connects, sends, reads output and disconnects
// (CONSOLE-AC-7).
package consoletc24

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/tsic404/kos-sdk/manifest"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestFirstInstallNodeJourney walks the journey's Go-observable steps.
func TestFirstInstallNodeJourney(t *testing.T) {
	if !app.TerminalPage().Allows(write.RoleOwner) {
		t.Fatal("installing the component must give the Owner the terminal entry")
	}
	order := []string{app.ItemTargetTypes, app.ItemNodeTargets, app.ItemPodTargets, app.ItemSessionState, app.ItemSessionAudit}
	items := app.TerminalPage().Items
	if len(items) != len(order) {
		t.Fatalf("items = %d, want %d in the design's order", len(items), len(order))
	}
	for index, id := range order {
		if items[index].ID != id {
			t.Fatalf("item %d = %q, want %q", index, items[index].ID, id)
		}
	}
	encoded, err := json.Marshal(app.Declaration())
	if err != nil {
		t.Fatalf("encode declaration: %v", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode declaration: %v", err)
	}
	if checked := manifest.ValidateExtensionManifest(document); !checked.OK() {
		t.Fatalf("declaration invalid: %+v", checked.Issues)
	}

	client, surface := fixtures.Start(t, fixtures.Options{
		Active: true,
		Nodes:  fixtures.NodeInventory(fixtures.Node("node-a", "uid-node-a")),
	})
	ctx := context.Background()
	surface.SetOutput("node-a uptime\n")

	nodes, err := client.Nodes(ctx)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("node targets = %+v, %v, want the installed node", nodes, err)
	}
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("confirm and connect: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if session.State() != terminal.StateConnected || session.View().ExecutionIdentity != "root@node" {
		t.Fatalf("session = %+v, want a connected root@node session", session.View())
	}
	receipt, err := session.Send(ctx, []byte("uptime\n"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if receipt.Output != "node-a uptime\n" {
		t.Fatalf("output = %q, want the node's answer", receipt.Output)
	}
	if err := session.End(ctx); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := session.Send(ctx, []byte("id\n")); err == nil {
		t.Fatal("input after disconnect must be refused")
	}
}
