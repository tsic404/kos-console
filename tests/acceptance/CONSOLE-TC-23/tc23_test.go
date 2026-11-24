// Package consoletc23 checks CONSOLE-TC-23: the token is delivered once, lives
// five minutes, is replaced rather than widened by renewal, does not reset the
// idle bound, and is absent from the session and audit projections and the URL
// (CONSOLE-C-14, KOSD-C-91).
package consoletc23

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestTokenLifetimeAndRenewal checks the delivery window and the replacement.
func TestTokenLifetimeAndRenewal(t *testing.T) {
	client, _ := fixtures.Start(t, fixtures.Options{Active: true, IdleMinutes: 10})
	ctx := context.Background()

	created, err := client.Create(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	lifetime := time.Until(created.ExpiresAt)
	if lifetime < 4*time.Minute+50*time.Second || lifetime > 5*time.Minute+10*time.Second {
		t.Fatalf("token lifetime = %v, want five minutes", lifetime)
	}
	if _, err := client.Attach(ctx, created.SessionID, created.Token); err != nil {
		t.Fatalf("attach with the delivered token: %v", err)
	}

	renewed, err := client.Renew(ctx, created.SessionID)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if renewed.Token == "" || renewed.Token == created.Token {
		t.Fatal("renewal must deliver a new token")
	}
	if renewed.IdleTimeoutMinutes != 10 {
		t.Fatalf("idle minutes after renewal = %d, want the unchanged 10", renewed.IdleTimeoutMinutes)
	}
	if _, err := client.Attach(ctx, created.SessionID, created.Token); err == nil {
		t.Fatal("the replaced token must stop working")
	}
	if _, err := client.Attach(ctx, created.SessionID, renewed.Token); err != nil {
		t.Fatalf("attach with the renewed token: %v", err)
	}

	encoded, err := json.Marshal(renewed.SessionView)
	if err != nil {
		t.Fatalf("encode session view: %v", err)
	}
	if strings.Contains(string(encoded), renewed.Token) || strings.Contains(string(encoded), `"token"`) {
		t.Fatal("the session metadata must not carry the token")
	}
}

// TestRevokedAccountCannotRenew checks a revoked session cannot renew and ends.
func TestRevokedAccountCannotRenew(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()
	session, err := client.Open(ctx, terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	surface.SetFailure("renew", 403, "terminal_forbidden", "权限已撤销", "authorize")
	if err := session.Renew(ctx); err == nil {
		t.Fatal("a revoked session must not renew")
	}
	if session.CanInput() {
		t.Fatal("a revoked session must leave the live set")
	}
}
