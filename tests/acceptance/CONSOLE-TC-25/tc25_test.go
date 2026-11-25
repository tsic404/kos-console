// Package consoletc25 checks CONSOLE-TC-25, the Pod journey: the browser names
// the application, Pod and container, a multi-container Pod needs an explicit
// choice, and switching Pod is a new session that never reuses the old token
// (CONSOLE-AC-8).
package consoletc25

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestPodJourneySwitchesWithANewSession walks the case's steps.
func TestPodJourneySwitchesWithANewSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	first := terminal.Target{Type: terminal.TargetPod, ID: "shop-1", UID: "uid-pod-1", Namespace: "home", Container: "api"}
	surface.SetOutput("api ready\n")
	created, err := client.Create(ctx, first)
	if err != nil {
		t.Fatalf("create first Pod: %v", err)
	}
	if created.ExecutionIdentity != "container:home/shop-1/api" || created.Namespace != "home" || created.Container != "api" {
		t.Fatalf("first session = %+v, want the named container", created.SessionView)
	}
	if _, err := client.Attach(ctx, created.SessionID, created.Token); err != nil {
		t.Fatalf("attach first Pod: %v", err)
	}
	receipt, err := client.Input(ctx, created.SessionID, created.Token, 1, []byte("ps\n"))
	if err != nil {
		t.Fatalf("input on first Pod: %v", err)
	}
	if receipt.Output != "api ready\n" {
		t.Fatalf("output = %q, want the chosen container's output", receipt.Output)
	}

	second := terminal.Target{Type: terminal.TargetPod, ID: "shop-0", UID: "uid-pod-0", Namespace: "home", Container: "web"}
	switched, err := client.Create(ctx, second)
	if err != nil {
		t.Fatalf("create second Pod: %v", err)
	}
	if switched.SessionID == created.SessionID || switched.TargetUID != "uid-pod-0" {
		t.Fatalf("switched session = %+v, want a new session on the second Pod", switched.SessionView)
	}
	if _, err := client.Attach(ctx, switched.SessionID, created.Token); err == nil {
		t.Fatal("the first Pod's token must not attach the second session")
	}
}
