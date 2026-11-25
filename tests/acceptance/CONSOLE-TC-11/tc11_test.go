// Package consoletc11 checks CONSOLE-TC-11: target lists and direct session
// requests follow the account's existing permission only, a Guest has no entry,
// a non-Owner sees no node type, and an unavailable target does not reveal
// whether it exists (CONSOLE-C-10).
package consoletc11

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

// member and owner are the two signed-in accounts the case reads as.
var (
	member = identity.Session{Username: "lin", Role: write.RoleMember}
	owner  = identity.Session{Username: "tsic", Role: write.RoleOwner}
)

// world is two applications owned by different members, each with one Pod, plus
// one node.
func world() *app.Readings {
	return &app.Readings{
		Targets: &app.ObjectTargets{
			Source: fixtures.Objects{Items: map[string][]controlplane.ObservedObject{
				"Application": {
					{Object: fixtures.ApplicationObject("home", "shop", "uid-shop", "lin", false), ObservedAt: time.Now()},
					{Object: fixtures.ApplicationObject("home", "reports", "uid-reports", "kai", false), ObservedAt: time.Now()},
				},
				"Node": {{Object: map[string]any{"metadata": map[string]any{"name": "node-a", "uid": "uid-node-a"}}, ObservedAt: time.Now()}},
			}},
			Pods: fixtures.Pods{
				"shop":    {{Namespace: "home", Name: "shop-0", UID: "uid-shop-0", Containers: []string{"web"}, ObservedAt: time.Now()}},
				"reports": {{Namespace: "home", Name: "reports-0", UID: "uid-reports-0", Containers: []string{"web"}, ObservedAt: time.Now()}},
			},
		},
	}
}

// podNames returns the Pod names one role's page shows.
func podNames(t *testing.T, readings *app.Readings, session identity.Session) []string {
	t.Helper()
	value := fixtures.ReadValue(t, readings, session, app.ItemPodTargets)
	view, _ := value.(map[string]any)
	pods, _ := view["items"].([]app.PodTarget)
	names := make([]string, 0, len(pods))
	for _, pod := range pods {
		names = append(names, pod.Pod)
	}
	return names
}

// TestListedTargetsFollowExistingPermission checks the Member sees only its own
// application's Pod and no nodes, while the Owner sees every Pod and the node.
func TestListedTargetsFollowExistingPermission(t *testing.T) {
	readings := world()

	memberPods := podNames(t, readings, member)
	if len(memberPods) != 1 || memberPods[0] != "shop-0" {
		t.Fatalf("member Pods = %v, want only the owned application's Pod", memberPods)
	}
	nodeValue := fixtures.ReadValue(t, readings, member, app.ItemNodeTargets)
	nodeView, _ := nodeValue.(map[string]any)
	if items, _ := nodeView["items"].([]app.NodeTarget); len(items) != 0 {
		t.Fatalf("member node targets = %+v, want none", items)
	}

	ownerPods := podNames(t, readings, owner)
	if len(ownerPods) != 2 {
		t.Fatalf("owner Pods = %v, want both applications' Pods", ownerPods)
	}
	ownerNodes := fixtures.ReadValue(t, readings, owner, app.ItemNodeTargets)
	ownerNodeView, _ := ownerNodes.(map[string]any)
	nodes, _ := ownerNodeView["items"].([]app.NodeTarget)
	if len(nodes) != 1 || nodes[0].ExecutionIdentities[0] != "root@node" {
		t.Fatalf("owner node targets = %+v, want node-a as root@node", nodes)
	}
}

// TestUnavailableTargetDoesNotRevealExistence checks a refused direct request
// answers the same code for an existing and a nonexistent target.
func TestUnavailableTargetDoesNotRevealExistence(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	surface.SetFailure("create", 403, "terminal_target_unavailable", "目标不可用", "authorize")

	existing := terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-shop-0", Namespace: "home", Container: "web"}
	missing := terminal.Target{Type: terminal.TargetPod, ID: "absent", UID: "uid-absent", Namespace: "home", Container: "web"}
	first := createFailure(t, client, existing)
	second := createFailure(t, client, missing)
	if first.Code != "terminal_target_unavailable" || second.Code != "terminal_target_unavailable" {
		t.Fatalf("codes = %q and %q, want terminal_target_unavailable for both", first.Code, second.Code)
	}
	if first.StatusCode != second.StatusCode || first.Message != second.Message {
		t.Fatalf("answers differ: %+v vs %+v", first, second)
	}
	if surface.SessionCount() != 0 {
		t.Fatal("a refused target must not create a session")
	}
}

// createFailure submits one creation and returns its refusal.
func createFailure(t *testing.T, client *terminal.Client, target terminal.Target) *terminal.APIError {
	t.Helper()
	_, err := client.Create(context.Background(), target)
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("create %s error = %v, want an APIError", target.ID, err)
	}
	return apiError
}
