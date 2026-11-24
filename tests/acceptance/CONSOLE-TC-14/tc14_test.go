// Package consoletc14 checks CONSOLE-TC-14: an unavailable executor or an
// unprovable credential boundary is a connect failure, a live session that hits
// it is shown failed with its reason, and recovery enables only new sessions
// (CONSOLE-C-12).
package consoletc14

import (
	"context"
	"testing"

	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// node is the node target the case connects to.
var node = terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"}

// TestExecutorAndBoundaryFailuresAreDistinct checks both connect-time codes
// reach the caller unchanged.
func TestExecutorAndBoundaryFailuresAreDistinct(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})

	surface.SetFailure("create", 503, "terminal_executor_unavailable", "执行服务不可用", "connect")
	if code := createCode(t, client, node); code != "terminal_executor_unavailable" {
		t.Fatalf("executor code = %q, want terminal_executor_unavailable", code)
	}
	surface.SetFailure("create", 503, "terminal_credential_boundary_unavailable", "无法保证凭据隔离", "connect")
	if code := createCode(t, client, node); code != "terminal_credential_boundary_unavailable" {
		t.Fatalf("boundary code = %q, want terminal_credential_boundary_unavailable", code)
	}
	if surface.SessionCount() != 0 {
		t.Fatal("neither refusal may create a session")
	}
}

// TestRecoveryOpensOnlyANewSession checks the failed session stays failed and
// input is not replayed to the recovered executor.
func TestRecoveryOpensOnlyANewSession(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	ctx := context.Background()

	session, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := session.Attach(ctx); err != nil {
		t.Fatalf("attach: %v", err)
	}

	surface.SetFailure("input", 503, "terminal_executor_unavailable", "执行服务不可用", "connect")
	if _, err := session.Send(ctx, []byte("uptime\n")); err == nil {
		t.Fatal("a failed executor must refuse input")
	}
	if session.State() != terminal.StateFailed || session.View().Reason != "terminal_executor_unavailable" {
		t.Fatalf("session = %q reason %q, want failed with the code", session.State(), session.View().Reason)
	}

	calls := surface.InputCalls()
	if _, err := session.Send(ctx, []byte("uptime\n")); err == nil {
		t.Fatal("the failed session must keep refusing input")
	}
	if surface.InputCalls() != calls {
		t.Fatal("a refused send must not reach the executor again")
	}

	surface.ClearFailure()
	fresh, err := client.Open(ctx, node)
	if err != nil {
		t.Fatalf("open after recovery: %v", err)
	}
	if err := fresh.Attach(ctx); err != nil {
		t.Fatalf("attach after recovery: %v", err)
	}
	if _, err := fresh.Send(ctx, []byte("uptime\n")); err != nil {
		t.Fatalf("send after recovery: %v", err)
	}
	if session.CanInput() {
		t.Fatal("recovery must not revive the failed session")
	}
}

// createCode returns the refusal code of one creation.
func createCode(t *testing.T, client *terminal.Client, target terminal.Target) string {
	t.Helper()
	_, err := client.Create(context.Background(), target)
	apiError, ok := err.(*terminal.APIError)
	if !ok {
		t.Fatalf("create error = %v, want an APIError", err)
	}
	return apiError.Code
}
