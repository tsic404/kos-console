// Package consoletc20 checks CONSOLE-TC-20: the component is an independent
// package whose withdrawal ends live sessions and invalidates their tokens, and
// reinstalling restores only the entry and the ability to create a new session
// (CONSOLE-C-16).
package consoletc20

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestDeactivationEndsSessionsAndReinstallRestoresOnlyTheEntry checks the
// lifecycle the control plane reports through the session surface.
func TestDeactivationEndsSessionsAndReinstallRestoresOnlyTheEntry(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: false})
	ctx := context.Background()
	target := terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

	if code := createCode(t, client, target); code != "capability_missing" {
		t.Fatalf("inactive create code = %q, want capability_missing", code)
	}
	if _, err := client.Nodes(ctx); err == nil {
		t.Fatal("an inactive component must not read the node inventory")
	}

	surface.SetActive(true)
	created, err := client.Create(ctx, target)
	if err != nil {
		t.Fatalf("create after install: %v", err)
	}
	if _, err := client.Attach(ctx, created.SessionID, created.Token); err != nil {
		t.Fatalf("attach after install: %v", err)
	}

	surface.SetActive(false)
	if code := createCode(t, client, target); code != "capability_missing" {
		t.Fatalf("uninstalled create code = %q, want capability_missing", code)
	}
	if _, err := client.Attach(ctx, created.SessionID, created.Token); err == nil {
		t.Fatal("an uninstalled component must refuse the old token")
	}

	surface.SetActive(true)
	fresh, err := client.Create(ctx, target)
	if err != nil {
		t.Fatalf("create after reinstall: %v", err)
	}
	if fresh.SessionID == created.SessionID {
		t.Fatal("reinstalling must not restore the old session")
	}
	if fresh.Token == created.Token {
		t.Fatal("reinstalling must not restore the old token")
	}
}

// TestPackageWithdrawsOnlyItsOwnContributions checks the declaration carries one
// page and one setting, so uninstall touches no other component's page.
func TestPackageWithdrawsOnlyItsOwnContributions(t *testing.T) {
	declaration := app.Declaration()
	if len(declaration.Contributions.Pages) != 1 || len(declaration.Contributions.Settings) != 1 {
		t.Fatalf("contributions = %d pages, %d settings, want one each",
			len(declaration.Contributions.Pages), len(declaration.Contributions.Settings))
	}
	if len(declaration.Contributions.Apps) != 0 {
		t.Fatal("the terminal page declares no window application")
	}
	for _, retention := range declaration.Retention {
		if retention.Keep {
			t.Fatalf("retention %q keeps data, want the component's own declarations withdrawn", retention.Resource)
		}
	}
}

// createCode returns one creation's refusal code.
func createCode(t *testing.T, client *terminal.Client, target terminal.Target) string {
	t.Helper()
	_, err := client.Create(context.Background(), target)
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("create error = %v, want an APIError", err)
	}
	return apiError.Code
}
