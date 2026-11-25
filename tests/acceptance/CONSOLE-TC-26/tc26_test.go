// Package consoletc26 checks CONSOLE-TC-26, the no-permission journey: a Guest
// has no entry, a Member without a writable application sees no target, a
// non-Owner sees no node type, and a prefilled address is refused without
// creating a session (CONSOLE-AC-9).
package consoletc26

import (
	"context"
	"testing"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestNoPermissionJourney checks the page and the direct request together.
func TestNoPermissionJourney(t *testing.T) {
	if app.TerminalPage().Allows(write.RoleGuest) {
		t.Fatal("a Guest must not find the terminal entry")
	}

	member := identity.Session{Username: "lin", Role: write.RoleMember}
	readings := &app.Readings{Targets: &app.ObjectTargets{
		Source: fixtures.Objects{Items: map[string][]controlplane.ObservedObject{
			"Application": {{Object: fixtures.ApplicationObject("home", "reports", "uid-reports", "kai", false), ObservedAt: time.Now()}},
			"Node":        {{Object: map[string]any{"metadata": map[string]any{"name": "node-a", "uid": "uid-node-a"}}, ObservedAt: time.Now()}},
		}},
		Pods: fixtures.Pods{"reports": {{Namespace: "home", Name: "reports-0", UID: "uid-reports-0", Containers: []string{"web"}, ObservedAt: time.Now()}}},
	}}

	pods := fixtures.ReadValue(t, readings, member, app.ItemPodTargets).(map[string]any)
	if items := pods["items"].([]app.PodTarget); len(items) != 0 {
		t.Fatalf("member Pod targets = %+v, want no target it may not write", items)
	}
	nodes := fixtures.ReadValue(t, readings, member, app.ItemNodeTargets).(map[string]any)
	if items := nodes["items"].([]app.NodeTarget); len(items) != 0 {
		t.Fatalf("member node targets = %+v, want no node type", items)
	}

	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	surface.SetFailure("create", 403, "terminal_target_unavailable", "目标不可用", "authorize")
	_, err := client.Create(context.Background(), terminal.Target{
		Type: terminal.TargetPod, ID: "reports-0", UID: "uid-reports-0", Namespace: "home", Container: "web",
	})
	apiError, ok := err.(*terminal.APIError)
	if !ok || apiError.Code != "terminal_target_unavailable" {
		t.Fatalf("prefilled target error = %v, want terminal_target_unavailable", err)
	}
	if surface.SessionCount() != 0 {
		t.Fatal("a refused prefilled target must not create a session")
	}
}
