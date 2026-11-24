// Package consoletc9 checks CONSOLE-TC-9: a single-container Pod is preselected
// and shown, a multi-container Pod is never preselected, and a multi-container
// target cannot connect until one container is chosen (CONSOLE-C-8).
package consoletc9

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

// inventory is the member's owned application with a single-container and a
// multi-container Pod.
func inventory() *app.Readings {
	return &app.Readings{
		Targets: &app.ObjectTargets{
			Source: fixtures.Objects{Items: map[string][]controlplane.ObservedObject{
				"Application": {{Object: fixtures.ApplicationObject("home", "shop", "uid-shop", "lin", false), ObservedAt: time.Now()}},
			}},
			Pods: fixtures.Pods{"shop": {
				{Namespace: "home", Name: "shop-0", UID: "uid-pod-0", Containers: []string{"web"}, ObservedAt: time.Now()},
				{Namespace: "home", Name: "shop-1", UID: "uid-pod-1", Containers: []string{"api", "worker"}, ObservedAt: time.Now()},
			}},
		},
	}
}

// TestContainerSelectionIsVisible checks the two Pods are listed with their
// containers and that only the single-container Pod carries a preselection.
func TestContainerSelectionIsVisible(t *testing.T) {
	value := fixtures.ReadValue(t, inventory(), identity.Session{Username: "lin", Role: write.RoleMember}, app.ItemPodTargets)
	view, _ := value.(map[string]any)
	pods, _ := view["items"].([]app.PodTarget)
	if len(pods) != 2 {
		t.Fatalf("pod targets = %+v, want both Pods", pods)
	}
	if pods[0].Namespace != "home" || pods[0].Pod != "shop-0" || len(pods[0].Containers) != 1 || pods[0].SelectedContainer != "web" {
		t.Fatalf("single-container target = %+v, want web preselected", pods[0])
	}
	if len(pods[1].Containers) != 2 || pods[1].SelectedContainer != "" {
		t.Fatalf("multi-container target = %+v, want no preselection", pods[1])
	}
}

// TestMultiContainerNeedsAnExplicitChoice checks an unselected multi-container
// target is refused and a chosen container binds the session to it.
func TestMultiContainerNeedsAnExplicitChoice(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	unselected := terminal.Target{Type: terminal.TargetPod, ID: "shop-1", UID: "uid-pod-1", Namespace: "home"}
	if _, err := client.Create(ctx, unselected); err == nil {
		t.Fatal("a multi-container target without a container must be refused")
	} else if apiError, ok := err.(*terminal.APIError); !ok || apiError.Code != "invalid_field" {
		t.Fatalf("unselected container error = %v, want invalid_field", err)
	}

	surface.SetOutput("from-worker")
	chosen := unselected
	chosen.Container = "worker"
	session, err := client.Open(ctx, chosen)
	if err != nil {
		t.Fatalf("create chosen container: %v", err)
	}
	if session.View().Container != "worker" || session.View().ExecutionIdentity != "container:home/shop-1/worker" {
		t.Fatalf("session view = %+v, want the chosen container", session.View())
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	receipt, err := session.Send(ctx, []byte("ps\n"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if receipt.Output != "from-worker" {
		t.Fatalf("output = %q, want the chosen container's output", receipt.Output)
	}
}
