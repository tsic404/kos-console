package terminal_test

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// open creates and attaches one session, failing the case if either step is
// refused.
func open(t *testing.T, client *terminal.Client) *terminal.Session {
	t.Helper()
	session, err := client.Open(context.Background(), nodeTarget())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(context.Background()); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return session
}

// TestSessionAcceptsInputOnlyAfterConnect checks that input is refused while the
// session is still connecting and accepted once it is connected
// (KOSD-C-90, CONSOLE-C-11).
func TestSessionAcceptsInputOnlyAfterConnect(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	session, err := client.Open(ctx, nodeTarget())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if session.CanInput() {
		t.Fatal("a connecting session must not accept input")
	}
	if _, err := session.Send(ctx, []byte("uptime")); !isSessionError(err, terminal.CodeNotConnected) {
		t.Fatalf("send before connect error = %v, want %s", err, terminal.CodeNotConnected)
	}

	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if !session.CanInput() {
		t.Fatal("a connected session must accept input")
	}
}

// TestSessionNumbersEachInputAndDoesNotReplay checks that every batch carries
// the next per-session sequence number and that repeating a sequence returns the
// original receipt without delivering the batch again (KOSD-C-92).
func TestSessionNumbersEachInputAndDoesNotReplay(t *testing.T) {
	client, surface := start(t, fixtures.Options{Active: true})
	session := open(t, client)

	first, err := session.Send(context.Background(), []byte("uptime"))
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if first.Sequence != 1 || first.Result != "accepted" {
		t.Fatalf("first receipt = %+v, want sequence 1 accepted", first)
	}

	second, err := session.Send(context.Background(), []byte("date"))
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if second.Sequence != 2 {
		t.Fatalf("second sequence = %d, want 2", second.Sequence)
	}

	repeat, err := session.SendSequence(context.Background(), 1, []byte("uptime"))
	if err != nil {
		t.Fatalf("repeat send: %v", err)
	}
	if !repeat.Duplicate || repeat.Result != "accepted" {
		t.Fatalf("repeat receipt = %+v, want the original receipt marked duplicate", repeat)
	}
	if got := surface.InputCalls(); got != 2 {
		t.Fatalf("input calls = %d, want 2, the repeat must not be delivered", got)
	}
}

// TestSessionRefusesInputAfterFailure checks that a terminal failure ends the
// session and no later input is offered (CONSOLE-C-12).
func TestSessionRefusesInputAfterFailure(t *testing.T) {
	client, surface := start(t, fixtures.Options{Active: true})
	session := open(t, client)
	surface.SetFailure("input", 503, "terminal_executor_unavailable", "执行服务不可用，会话已结束", "connect")

	_, err := session.Send(context.Background(), []byte("uptime"))
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("send error = %v, want an APIError", err)
	}
	if apiError.Code != "terminal_executor_unavailable" {
		t.Fatalf("code = %q, want terminal_executor_unavailable", apiError.Code)
	}
	if session.State() != terminal.StateFailed || session.CanInput() {
		t.Fatalf("state = %q, want failed and no further input", session.State())
	}
}

// TestSessionRenewReplacesTheTokenAndKeepsIdle checks that a renewal delivers a
// new token and leaves the idle bound untouched (KOSD-C-91, KOSD-C-94).
func TestSessionRenewReplacesTheTokenAndKeepsIdle(t *testing.T) {
	client, _ := start(t, fixtures.Options{Active: true})
	session := open(t, client)
	idle := session.View().IdleTimeoutMinutes

	if err := session.Renew(context.Background()); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if !session.HasToken() || session.View().IdleTimeoutMinutes != idle {
		t.Fatalf("after renew, token = %v, idle = %d, want a token and the same idle", session.HasToken(), session.View().IdleTimeoutMinutes)
	}
}

// TestSessionEndIsIdempotent checks that a repeated end has no second effect
// (KOSD-C-90, CONSOLE-C-11).
func TestSessionEndIsIdempotent(t *testing.T) {
	client, surface := start(t, fixtures.Options{Active: true})
	session := open(t, client)

	if err := session.End(context.Background()); err != nil {
		t.Fatalf("end: %v", err)
	}
	if err := session.End(context.Background()); err != nil {
		t.Fatalf("repeat end: %v", err)
	}
	if got := surface.EndCalls(); got != 1 {
		t.Fatalf("end calls = %d, want 1", got)
	}
	if session.CanInput() {
		t.Fatal("a closed session must not accept input")
	}
}

// TestSessionRefusesAttachWithoutToken checks that a session created by a
// repeated creation, which delivers no second token, cannot attach without the
// token the original creation delivered (KOSD-C-91).
func TestSessionRefusesAttachWithoutToken(t *testing.T) {
	surface := fixtures.NewTerminal(fixtures.Options{Active: true, ConsoleToken: consoleToken, Extension: extension})
	server := newServer(t, surface)
	first, err := terminal.New(server, extension, consoleToken)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	if _, err := first.Create(context.Background(), nodeTarget()); err != nil {
		t.Fatalf("create: %v", err)
	}

	second, err := terminal.New(server, extension, consoleToken)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	session, err := second.Open(context.Background(), nodeTarget())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if session.HasToken() {
		t.Fatal("a repeated creation must not deliver a second token")
	}
	if err := session.Attach(context.Background()); !isSessionError(err, terminal.CodeNoToken) {
		t.Fatalf("attach error = %v, want %s", err, terminal.CodeNoToken)
	}
}

// isSessionError reports whether an error is the machine's refusal with code.
func isSessionError(err error, code string) bool {
	machine, ok := err.(*terminal.SessionError)
	return ok && machine.Code == code
}
