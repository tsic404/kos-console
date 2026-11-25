// Package consoletc16 checks CONSOLE-TC-16: a target whose credential boundary
// cannot be proven is refused with its own code, and the data this product
// carries never holds a protected credential, raw input or output in its
// session or audit projections (CONSOLE-C-14).
package consoletc16

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
	"github.com/tsic404/kos-console/tests/acceptance/fixtures"
)

// TestUnprovableBoundaryIsRefused checks the boundary refusal is its own code
// and never a connected session.
func TestUnprovableBoundaryIsRefused(t *testing.T) {
	client, surface := fixtures.Start(t, fixtures.Options{Active: true})
	surface.SetFailure("create", 503, "terminal_credential_boundary_unavailable", "无法保证凭据隔离", "connect")

	_, err := client.Create(context.Background(), terminal.Target{Type: terminal.TargetNode, ID: "node-a", UID: "uid-node-a"})
	apiError, ok := err.(*terminal.APIError)
	if !ok || apiError.Code != "terminal_credential_boundary_unavailable" {
		t.Fatalf("error = %v, want terminal_credential_boundary_unavailable", err)
	}
	if strings.Contains(string(apiError.Raw), "root@node") {
		t.Fatal("a boundary refusal must not describe the execution identity it refused")
	}
	if surface.SessionCount() != 0 {
		t.Fatal("a refused boundary must not create a session")
	}
}

// TestSessionAndAuditViewsCarryNoProtectedData checks the projections this
// product owns have no field for a token, raw input or raw output.
func TestSessionAndAuditViewsCarryNoProtectedData(t *testing.T) {
	forbidden := []string{"token", "password", "secret", "kubeconfig", "input", "output"}
	for name, value := range map[string]any{
		"terminal.SessionView": terminal.SessionView{},
		"terminal.AuditEvent":  terminal.AuditEvent{},
		"app.SessionView":      app.SessionView{},
		"app.AuditEvent":       app.AuditEvent{},
	} {
		for _, field := range fieldNames(value) {
			for _, word := range forbidden {
				if strings.Contains(strings.ToLower(field), word) {
					t.Fatalf("%s carries field %q, which the projection must not hold", name, field)
				}
			}
		}
	}

	view := terminal.SessionView{SessionID: "ts-1", TargetType: terminal.TargetNode}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("encode session view: %v", err)
	}
	if strings.Contains(string(encoded), `"token"`) {
		t.Fatal("the session view must not serialize a token")
	}
}

// fieldNames lists the JSON-visible fields of one projection.
func fieldNames(value any) []string {
	kind := reflect.TypeOf(value)
	names := make([]string, 0, kind.NumField())
	for index := 0; index < kind.NumField(); index++ {
		field := kind.Field(index)
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		names = append(names, tag)
	}
	return names
}
