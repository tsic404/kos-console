package app_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
)

// fixture clock every read observes its values at.
var readClock = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

// session fixtures the cases read as.
var (
	ownerSession  = identity.Session{Username: "owner", Role: write.RoleOwner}
	memberSession = identity.Session{Username: "lin", Role: write.RoleMember}
)

// fakeTargets returns a configured inventory.
type fakeTargets struct {
	nodes []app.NodeTarget
	pods  []app.PodTarget
}

func (f fakeTargets) NodeTargets(context.Context, identity.Session) ([]app.NodeTarget, *controlplane.Error) {
	return f.nodes, nil
}

func (f fakeTargets) PodTargets(context.Context, identity.Session) ([]app.PodTarget, *controlplane.Error) {
	return f.pods, nil
}

// fakeSessions returns a configured session list.
type fakeSessions struct {
	sessions []app.SessionView
}

func (f fakeSessions) List(context.Context, identity.Session) ([]app.SessionView, *controlplane.Error) {
	return f.sessions, nil
}

// fakeAudits returns a configured event list.
type fakeAudits struct {
	events []app.AuditEvent
}

func (f fakeAudits) Events(context.Context, identity.Session, string) ([]app.AuditEvent, *controlplane.Error) {
	return f.events, nil
}

// read resolves one item through a readings value.
func read(t *testing.T, readings *app.Readings, session identity.Session, item string) (any, *controlplane.Error) {
	t.Helper()
	declared := extension.PageItem{ID: item, Label: item, Source: extension.PageSourceObject, Reference: app.Identifier + "/" + item}
	result, failure := readings.Read(context.Background(), session, declared)
	if failure != nil {
		return nil, failure
	}
	return result.Value, nil
}

// TestTargetTypesFollowTheSessionRole checks which target kinds the page offers
// and the execution identity each reaches its target as (CONSOLE-C-9,
// CONSOLE-C-10).
func TestTargetTypesFollowTheSessionRole(t *testing.T) {
	readings := &app.Readings{Now: func() time.Time { return readClock }}

	member, failure := read(t, readings, memberSession, app.ItemTargetTypes)
	if failure != nil {
		t.Fatalf("member target types: %v", failure)
	}
	memberTypes, _ := member.(map[string]any)
	if node, _ := memberTypes["node"].(map[string]any); node["available"] != false {
		t.Fatalf("member node type = %v, want unavailable", node)
	}

	owner, failure := read(t, readings, ownerSession, app.ItemTargetTypes)
	if failure != nil {
		t.Fatalf("owner target types: %v", failure)
	}
	ownerTypes, _ := owner.(map[string]any)
	if node, _ := ownerTypes["node"].(map[string]any); node["available"] != true || node["identity"] != "root@node" {
		t.Fatalf("owner node type = %v, want available as root@node", node)
	}
}

// TestNodeTargetsAreHiddenFromANonOwner checks that a role that may not connect
// to a node is answered with an empty list and the page's reason, not with a
// node inventory (CONSOLE-C-10).
func TestNodeTargetsAreHiddenFromANonOwner(t *testing.T) {
	readings := &app.Readings{
		Targets: fakeTargets{nodes: []app.NodeTarget{{TargetID: "node-a", TargetUID: "uid-a"}}},
		Now:     func() time.Time { return readClock },
	}

	value, failure := read(t, readings, memberSession, app.ItemNodeTargets)
	if failure != nil {
		t.Fatalf("member node targets: %v", failure)
	}
	view, _ := value.(map[string]any)
	if items, _ := view["items"].([]app.NodeTarget); len(items) != 0 {
		t.Fatalf("member node items = %v, want none", items)
	}
	if view["reason"] != app.NodeTargetsReason {
		t.Fatalf("reason = %v, want the page's own statement", view["reason"])
	}
}

// TestSessionStateAndAuditAreRead checks the two session-side items carry the
// session metadata and the audit metadata without raw input, output or token
// (KOSD-C-90, KOSD-C-92, KOSD-C-96).
func TestSessionStateAndAuditAreRead(t *testing.T) {
	sessions := app.SessionView{
		SessionID: "ts-1", State: "connected",
		Limits: app.Limits{Account: 4, Target: 4}, Usage: app.Usage{Account: 1, Target: 1},
		IdleTimeoutMinutes: 10,
	}
	readings := &app.Readings{
		Sessions: fakeSessions{sessions: []app.SessionView{sessions}},
		Audits:   fakeAudits{events: []app.AuditEvent{{EventID: 2, SessionID: "ts-1", Event: "input", Result: "accepted"}}},
		Now:      func() time.Time { return readClock },
	}

	state, failure := read(t, readings, memberSession, app.ItemSessionState)
	if failure != nil {
		t.Fatalf("session state: %v", failure)
	}
	stateView, _ := state.(map[string]any)
	if _, present := stateView["rule"]; !present {
		t.Fatalf("session state = %v, want the page's read rule", stateView)
	}

	audit, failure := read(t, readings, memberSession, app.ItemSessionAudit)
	if failure != nil {
		t.Fatalf("session audit: %v", failure)
	}
	auditView, _ := audit.(map[string]any)
	rows, _ := auditView["sessions"].([]map[string]any)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %v, want one row per session", rows)
	}
	row := rows[0]
	if events, _ := row["events"].([]app.AuditEvent); len(events) != 1 || events[0].Event != "input" {
		t.Fatalf("audit events = %v, want the input event", events)
	}
}

// pageObject decodes one page value the way the shell serves it to the page.
func pageObject(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal page value: %v", err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatalf("decode page value: %v", err)
	}
	return object
}

// requireKeys checks one decoded value carries exactly the keys the page
// scripts read; a Go field name instead of a JSON key leaves a value empty.
func requireKeys(t *testing.T, object map[string]any, want ...string) {
	t.Helper()
	for _, key := range want {
		if _, present := object[key]; !present {
			t.Errorf("page value is missing %q: %v", key, object)
		}
	}
	for key := range object {
		if !slices.Contains(want, key) {
			t.Errorf("page value carries unexpected %q: %v", key, object)
		}
	}
}

// TestPageValuesCarryTheKeysThePageReads checks the target, session and audit
// values the page read serves marshal to the camelCase keys the page scripts
// consume (CONSOLE-C-8, CONSOLE-C-9, CONSOLE-C-15).
func TestPageValuesCarryTheKeysThePageReads(t *testing.T) {
	sequence := uint64(3)
	readings := &app.Readings{
		Targets: fakeTargets{
			nodes: []app.NodeTarget{{
				TargetType:          "node",
				TargetID:            "node-a",
				TargetUID:           "uid-node-a",
				ExecutionIdentities: []string{"root@node"},
				ObservedAt:          readClock,
			}},
			pods: []app.PodTarget{{
				Namespace:         "home",
				Application:       "shop",
				Pod:               "shop-0",
				TargetUID:         "uid-pod-0",
				Containers:        []string{"web"},
				SelectedContainer: "web",
				ObservedAt:        readClock,
			}},
		},
		Sessions: fakeSessions{sessions: []app.SessionView{{
			SessionID:          "ts-1",
			TargetType:         "pod",
			TargetID:           "shop-0",
			TargetUID:          "uid-pod-0",
			Namespace:          "home",
			Container:          "web",
			ExecutionIdentity:  "container:home/shop-0/web",
			State:              "failed",
			Reason:             "terminal_target_ended",
			ExpiresAt:          readClock,
			IdleTimeoutMinutes: 10,
			Limits:             app.Limits{Account: 4, Target: 4},
			Usage:              app.Usage{Account: 1, Target: 1},
		}}},
		Audits: fakeAudits{events: []app.AuditEvent{{
			EventID:           7,
			Actor:             "lin",
			Extension:         "kos-console",
			SessionID:         "ts-1",
			TargetType:        "pod",
			TargetID:          "shop-0",
			TargetUID:         "uid-pod-0",
			Namespace:         "home",
			Container:         "web",
			ExecutionIdentity: "container:home/shop-0/web",
			Event:             "input",
			Result:            "accepted",
			Sequence:          &sequence,
			OccurredAt:        readClock,
		}}},
		Now: func() time.Time { return readClock },
	}

	nodes := pageObject(t, mustRead(t, readings, ownerSession, app.ItemNodeTargets))
	nodeItems, _ := nodes["items"].([]any)
	if len(nodeItems) != 1 {
		t.Fatalf("node items = %v, want one node", nodes["items"])
	}
	node, _ := nodeItems[0].(map[string]any)
	requireKeys(t, node, "targetType", "targetId", "targetUid", "namespace", "container", "executionIdentities", "observedAt", "stale")

	pods := pageObject(t, mustRead(t, readings, memberSession, app.ItemPodTargets))
	podItems, _ := pods["items"].([]any)
	if len(podItems) != 1 {
		t.Fatalf("pod items = %v, want one Pod", pods["items"])
	}
	pod, _ := podItems[0].(map[string]any)
	requireKeys(t, pod, "namespace", "application", "pod", "targetUid", "containers", "selectedContainer", "observedAt", "stale")

	state := pageObject(t, mustRead(t, readings, memberSession, app.ItemSessionState))
	stateSessions, _ := state["sessions"].([]any)
	if len(stateSessions) != 1 {
		t.Fatalf("session state = %v, want one session", state)
	}
	session, _ := stateSessions[0].(map[string]any)
	requireKeys(t, session, "sessionId", "targetType", "targetId", "targetUid", "namespace", "container", "executionIdentity", "state", "reason", "expiresAt", "idleTimeoutMinutes", "limits", "usage")
	limits, _ := session["limits"].(map[string]any)
	requireKeys(t, limits, "account", "target")
	usage, _ := session["usage"].(map[string]any)
	requireKeys(t, usage, "account", "target")

	audit := pageObject(t, mustRead(t, readings, memberSession, app.ItemSessionAudit))
	auditSessions, _ := audit["sessions"].([]any)
	if len(auditSessions) != 1 {
		t.Fatalf("session audit = %v, want one session", audit)
	}
	auditSession, _ := auditSessions[0].(map[string]any)
	requireKeys(t, auditSession, "sessionId", "events")
	events, _ := auditSession["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("audit events = %v, want one event", auditSession["events"])
	}
	event, _ := events[0].(map[string]any)
	requireKeys(t, event, "eventId", "actor", "extension", "sessionId", "targetType", "targetId", "targetUid", "namespace", "container", "executionIdentity", "event", "result", "sequence", "occurredAt")
}

// mustRead resolves one item and fails the case on a read failure.
func mustRead(t *testing.T, readings *app.Readings, session identity.Session, item string) any {
	t.Helper()
	value, failure := read(t, readings, session, item)
	if failure != nil {
		t.Fatalf("%s: %v", item, failure)
	}
	return value
}

// TestUnknownItemIsRefused checks an item the page does not declare is answered
// as an unreadable source rather than an invented value.
func TestUnknownItemIsRefused(t *testing.T) {
	readings := &app.Readings{Now: func() time.Time { return readClock }}
	if _, failure := read(t, readings, memberSession, "not-declared"); failure == nil {
		t.Fatal("an undeclared item must be refused")
	}
}

// fakeSource answers a configured object list by kind.
type fakeSource struct {
	objects map[string][]controlplane.ObservedObject
}

func (f fakeSource) List(_ context.Context, object controlplane.ObjectType, _ string) ([]controlplane.ObservedObject, error) {
	return f.objects[object.Kind], nil
}

func (f fakeSource) Get(_ context.Context, object controlplane.ObjectType, _, _ string) (controlplane.ObservedObject, error) {
	return controlplane.ObservedObject{}, nil
}

// fakePodProjector answers a configured Pod list by application.
type fakePodProjector struct {
	pods map[string][]app.PodCandidate
}

func (f fakePodProjector) Pods(_ context.Context, _, name string) ([]app.PodCandidate, *controlplane.Error) {
	return f.pods[name], nil
}

// TestPodTargetsFollowTheWritePermission checks that only the applications the
// account may write contribute Pod candidates, that a multi-container Pod is
// not preselected and a single-container Pod is (CONSOLE-C-8, CONSOLE-C-10).
func TestPodTargetsFollowTheWritePermission(t *testing.T) {
	shop := map[string]any{
		"apiVersion": "kos.tsic.top/v1beta1", "kind": "Application",
		"metadata": map[string]any{"namespace": "home", "name": "shop", "uid": "uid-shop"},
		"spec":     map[string]any{"owner": "lin", "namespace": "home"},
	}
	other := map[string]any{
		"apiVersion": "kos.tsic.top/v1beta1", "kind": "Application",
		"metadata": map[string]any{"namespace": "home", "name": "reports", "uid": "uid-reports"},
		"spec":     map[string]any{"owner": "kai", "namespace": "home"},
	}
	source := fakeSource{objects: map[string][]controlplane.ObservedObject{
		"Application": {
			{Object: shop, ObservedAt: readClock},
			{Object: other, ObservedAt: readClock},
		},
	}}
	projector := fakePodProjector{pods: map[string][]app.PodCandidate{
		"shop": {
			{Namespace: "home", Name: "shop-0", UID: "uid-pod-0", Containers: []string{"web"}, ObservedAt: readClock},
			{Namespace: "home", Name: "shop-1", UID: "uid-pod-1", Containers: []string{"a", "b"}, ObservedAt: readClock},
		},
	}}
	readings := &app.Readings{
		Targets: &app.ObjectTargets{Source: source, Pods: projector, Now: func() time.Time { return readClock }},
		Now:     func() time.Time { return readClock },
	}

	value, failure := read(t, readings, memberSession, app.ItemPodTargets)
	if failure != nil {
		t.Fatalf("pod targets: %v", failure)
	}
	view, _ := value.(map[string]any)
	pods, _ := view["items"].([]app.PodTarget)
	if len(pods) != 2 {
		t.Fatalf("pods = %+v, want the two Pods of the owned application", pods)
	}
	if pods[0].SelectedContainer != "web" {
		t.Fatalf("single-container selection = %q, want web", pods[0].SelectedContainer)
	}
	if pods[1].SelectedContainer != "" {
		t.Fatalf("multi-container selection = %q, want no preselection", pods[1].SelectedContainer)
	}

	ownerValue, failure := read(t, readings, ownerSession, app.ItemPodTargets)
	if failure != nil {
		t.Fatalf("owner pod targets: %v", failure)
	}
	ownerView, _ := ownerValue.(map[string]any)
	ownerPods, _ := ownerView["items"].([]app.PodTarget)
	if len(ownerPods) != 2 {
		t.Fatalf("owner pods = %+v, want every application's Pods", ownerPods)
	}
}
