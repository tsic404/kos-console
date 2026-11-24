package fixtures

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/terminal"
)

// Start mounts one scripted terminal surface and returns a client over it.
func Start(t *testing.T, options Options) (*terminal.Client, *Terminal) {
	t.Helper()
	if options.Extension == "" {
		options.Extension = "kos-console"
	}
	if options.ConsoleToken == "" {
		options.ConsoleToken = "console-session"
	}
	return StartAs(t, options, options.ConsoleToken)
}

// StartAs mounts a surface expecting options.ConsoleToken and returns a client
// that authenticates with consoleToken instead, so a case can drive an expired
// or foreign credential.
func StartAs(t *testing.T, options Options, consoleToken string) (*terminal.Client, *Terminal) {
	t.Helper()
	if options.Extension == "" {
		options.Extension = "kos-console"
	}
	if options.ConsoleToken == "" {
		options.ConsoleToken = "console-session"
	}
	surface := NewTerminal(options)
	server := httptest.NewServer(surface.Handler())
	t.Cleanup(server.Close)
	client, err := terminal.New(server.URL, options.Extension, consoleToken)
	if err != nil {
		t.Fatalf("build terminal client: %v", err)
	}
	return client, surface
}

// NodeInventory is one scripted node list (KOSD-C-95).
func NodeInventory(items ...terminal.NodeTarget) []terminal.NodeTarget {
	return items
}

// Node is one node inventory item with the identity a session binds to.
func Node(name, uid string) terminal.NodeTarget {
	return terminal.NodeTarget{
		TargetType:          terminal.TargetNode,
		TargetID:            name,
		TargetUID:           uid,
		ExecutionIdentities: []string{"root@node"},
	}
}

// ApplicationObject is one stored Application the Pod inventory is projected
// from (KOSD-C-76).
func ApplicationObject(namespace, name, uid, owner string, paused bool) map[string]any {
	spec := map[string]any{"owner": owner, "namespace": namespace}
	if paused {
		spec["paused"] = true
	}
	return map[string]any{
		"apiVersion": "kos.tsic.top/v1beta1",
		"kind":       "Application",
		"metadata":   map[string]any{"namespace": namespace, "name": name, "uid": uid},
		"spec":       spec,
	}
}

// DeclaredItem is one page item as the terminal page declares it.
func DeclaredItem(id string) extension.PageItem {
	return extension.PageItem{ID: id, Label: id, Reference: app.Identifier + "/" + id}
}

// ReadValue resolves one page item and fails the case on a read error.
func ReadValue(t *testing.T, readings *app.Readings, session identity.Session, id string) any {
	t.Helper()
	reading, failure := readings.Read(context.Background(), session, DeclaredItem(id))
	if failure != nil {
		t.Fatalf("read %s: %v", id, failure)
	}
	return reading.Value
}

// Objects is a scripted object source keyed by kind.
type Objects struct {
	Items   map[string][]controlplane.ObservedObject
	Failure error
}

// List returns the configured objects of one kind.
func (o Objects) List(_ context.Context, object controlplane.ObjectType, _ string) ([]controlplane.ObservedObject, error) {
	if o.Failure != nil {
		return nil, o.Failure
	}
	return o.Items[object.Kind], nil
}

// Get is not used by the terminal read path; it fails loudly rather than
// answering an object the case did not script.
func (o Objects) Get(context.Context, controlplane.ObjectType, string, string) (controlplane.ObservedObject, error) {
	return controlplane.ObservedObject{}, errors.New("fixtures: the terminal page never reads a single object")
}

// Pods is a scripted Pod projection keyed by application name.
type Pods map[string][]app.PodCandidate

// Pods returns the configured Pods of one application.
func (p Pods) Pods(_ context.Context, _, name string) ([]app.PodCandidate, *controlplane.Error) {
	return p[name], nil
}

// Sessions is a scripted session list.
type Sessions []app.SessionView

// List returns the configured sessions.
func (s Sessions) List(context.Context, identity.Session) ([]app.SessionView, *controlplane.Error) {
	return s, nil
}

// Audits is a scripted audit read.
type Audits []app.AuditEvent

// Events returns the configured events.
func (a Audits) Events(context.Context, identity.Session, string) ([]app.AuditEvent, *controlplane.Error) {
	return a, nil
}
