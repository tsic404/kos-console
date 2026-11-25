// Package consoletc21 checks CONSOLE-TC-21: the page and the client expose only
// session creation, interaction and end; there is no generic native-resource
// proxy, no port forward and no workload write (CONSOLE-C-16, KOSD-F-20).
package consoletc21

import (
	"reflect"
	"strings"
	"testing"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
)

// TestPageDeclaresOnlySessionActions checks both declared writes stay on the
// terminal session surface.
func TestPageDeclaresOnlySessionActions(t *testing.T) {
	page := app.TerminalPage()
	if len(page.Actions) != 2 {
		t.Fatalf("actions = %+v, want exactly create and end", page.Actions)
	}
	for _, action := range page.Actions {
		if !strings.HasPrefix(action.Path, app.PathTerminalSessions) {
			t.Fatalf("action %q targets %q, want the terminal session surface", action.ID, action.Path)
		}
		if action.Method != "POST" && action.Method != "DELETE" {
			t.Fatalf("action %q method = %q, want POST or DELETE", action.ID, action.Method)
		}
	}
}

// TestClientHasNoProxySurface checks the exported client calls are the terminal
// session operations and nothing that could proxy another object.
func TestClientHasNoProxySurface(t *testing.T) {
	allowed := map[string]bool{
		"Attach": true, "Audit": true, "Create": true, "End": true,
		"Input": true, "Nodes": true, "Open": true, "Renew": true, "Status": true,
	}
	kind := reflect.TypeOf(&terminal.Client{})
	for index := 0; index < kind.NumMethod(); index++ {
		name := kind.Method(index).Name
		if !allowed[name] {
			t.Fatalf("client exposes %q, which is outside the terminal session surface", name)
		}
	}
	for _, forbidden := range []string{"Proxy", "Forward", "Exec", "Apply", "DeleteObject", "Patch"} {
		if _, present := kind.MethodByName(forbidden); present {
			t.Fatalf("client exposes %q, a generic native write", forbidden)
		}
	}
}

// TestNoWindowApplicationIsDeclared checks the terminal is a native page and the
// browser receives no iframe application whose token could be widened.
func TestNoWindowApplicationIsDeclared(t *testing.T) {
	if len(app.Declaration().Contributions.Apps) != 0 {
		t.Fatal("the terminal must not ship a window application")
	}
}
