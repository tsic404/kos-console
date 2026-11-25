// Package consoletc10 checks CONSOLE-TC-10: a same-name replacement is a new
// identity that never inherits the old session, and switching target is a new
// confirmation and creation (CONSOLE-C-9).
package consoletc10

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestSameNameReplacementDoesNotInheritTheSession checks the immutable UID,
// not the name, binds a session to its Pod.
func TestSameNameReplacementDoesNotInheritTheSession(t *testing.T) {
	client, _ := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	original, err := client.Create(ctx, terminal.Target{
		Type: terminal.TargetPod, ID: "shop-0", UID: "uid-pod-0", Namespace: "home", Container: "web",
	})
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if _, err := client.Attach(ctx, original.SessionID, original.Token); err != nil {
		t.Fatalf("attach original: %v", err)
	}

	replacement, err := client.Create(ctx, terminal.Target{
		Type: terminal.TargetPod, ID: "shop-0", UID: "uid-pod-0-replaced", Namespace: "home", Container: "web",
	})
	if err != nil {
		t.Fatalf("create replacement: %v", err)
	}
	if replacement.SessionID == original.SessionID {
		t.Fatal("a replaced object must be a new session, not the original one")
	}
	if replacement.TargetUID != "uid-pod-0-replaced" || replacement.State != terminal.StateConnecting {
		t.Fatalf("replacement view = %+v, want a new connecting session on the new identity", replacement.SessionView)
	}

	if _, err := client.Attach(ctx, replacement.SessionID, original.Token); err == nil {
		t.Fatal("the original token must not attach the replacement session")
	}

	if _, err := client.Input(ctx, original.SessionID, original.Token, 1, []byte("ps\n")); err != nil {
		t.Fatalf("input on the original session: %v", err)
	}
	status, err := client.Status(ctx, replacement.SessionID)
	if err != nil {
		t.Fatalf("status of replacement: %v", err)
	}
	if status.TargetUID != "uid-pod-0-replaced" {
		t.Fatalf("replacement still targets %q, want the new UID", status.TargetUID)
	}
}
