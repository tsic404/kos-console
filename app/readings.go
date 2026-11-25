package app

import (
	"context"
	"time"

	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/pages"
	"github.com/tsic404/kosd/pkg/write"
)

// NodeTarget is one node a session may connect to, as the node inventory
// reports it (KOSD-C-95). A node carries empty namespace and container strings.
type NodeTarget struct {
	TargetType          string    `json:"targetType"`
	TargetID            string    `json:"targetId"`
	TargetUID           string    `json:"targetUid"`
	Namespace           string    `json:"namespace"`
	Container           string    `json:"container"`
	ExecutionIdentities []string  `json:"executionIdentities"`
	ObservedAt          time.Time `json:"observedAt"`
	Stale               bool      `json:"stale"`
}

// PodTarget is one Pod container a session may connect to. A multi-container
// Pod is never preselected: SelectedContainer is empty unless the Pod declares
// exactly one container, which the page still shows (CONSOLE-C-8).
type PodTarget struct {
	Namespace         string    `json:"namespace"`
	Application       string    `json:"application"`
	Pod               string    `json:"pod"`
	TargetUID         string    `json:"targetUid"`
	Containers        []string  `json:"containers"`
	SelectedContainer string    `json:"selectedContainer"`
	ObservedAt        time.Time `json:"observedAt"`
	Stale             bool      `json:"stale"`
}

// Limits and Usage are the per-account and per-target session bounds and the
// occupancy a response reports (KOSD-C-90, KOSD-C-94).
type (
	Limits struct {
		Account int `json:"account"`
		Target  int `json:"target"`
	}
	Usage struct {
		Account int `json:"account"`
		Target  int `json:"target"`
	}
)

// SessionView is one terminal session as the session surface reports it
// (KOSD-C-90).
type SessionView struct {
	SessionID          string    `json:"sessionId"`
	TargetType         string    `json:"targetType"`
	TargetID           string    `json:"targetId"`
	TargetUID          string    `json:"targetUid"`
	Namespace          string    `json:"namespace"`
	Container          string    `json:"container"`
	ExecutionIdentity  string    `json:"executionIdentity"`
	State              string    `json:"state"`
	Reason             string    `json:"reason,omitempty"`
	ExpiresAt          time.Time `json:"expiresAt"`
	IdleTimeoutMinutes int       `json:"idleTimeoutMinutes"`
	Limits             Limits    `json:"limits"`
	Usage              Usage     `json:"usage"`
}

// AuditEvent is one terminal audit record as the audit read reports it
// (KOSD-C-92, KOSD-C-96): metadata only, never raw input, output or token.
type AuditEvent struct {
	EventID           int64     `json:"eventId"`
	Actor             string    `json:"actor"`
	Extension         string    `json:"extension"`
	SessionID         string    `json:"sessionId,omitempty"`
	TargetType        string    `json:"targetType"`
	TargetID          string    `json:"targetId"`
	TargetUID         string    `json:"targetUid"`
	Namespace         string    `json:"namespace"`
	Container         string    `json:"container"`
	ExecutionIdentity string    `json:"executionIdentity"`
	Event             string    `json:"event"`
	Result            string    `json:"result"`
	Receipt           string    `json:"receipt,omitempty"`
	Sequence          *uint64   `json:"sequence,omitempty"`
	OccurredAt        time.Time `json:"occurredAt"`
}

// Targets resolves the two target inventories the page shows (KOSD-C-95,
// KOSD-C-76 filtered by CONSOLE-C-93). The concrete reader keeps every read
// trimmed to the session's existing permission, so a target the account may
// not connect to never appears.
type Targets interface {
	// NodeTargets returns the nodes the session may connect to.
	NodeTargets(ctx context.Context, session identity.Session) ([]NodeTarget, *controlplane.Error)
	// PodTargets returns the Pod containers the session may connect to.
	PodTargets(ctx context.Context, session identity.Session) ([]PodTarget, *controlplane.Error)
}

// Sessions lists the terminal sessions the account holds (KOSD-C-90).
type Sessions interface {
	List(ctx context.Context, session identity.Session) ([]SessionView, *controlplane.Error)
}

// Audits reads one session's audit events (KOSD-C-96).
type Audits interface {
	Events(ctx context.Context, session identity.Session, sessionID string) ([]AuditEvent, *controlplane.Error)
}

// Readings resolves the items the terminal page declares (KOSD-C-53). Every
// value comes from the control plane the host wired in; a missing seam is a
// capability the deployment does not offer, reported rather than invented.
type Readings struct {
	// Targets resolves the node and Pod inventories.
	Targets Targets
	// Sessions lists the account's live sessions.
	Sessions Sessions
	// Audits reads session audit events.
	Audits Audits
	// Now supplies the read clock the stale marker compares against; nil
	// defaults to time.Now.
	Now func() time.Time
}

// Read implements pages.Readings.
func (r *Readings) Read(ctx context.Context, session identity.Session, item extension.PageItem) (pages.Reading, *controlplane.Error) {
	observedAt := r.clock()
	value, failure := r.value(ctx, session, item)
	if failure != nil {
		return pages.Reading{}, failure
	}
	return pages.Reading{Value: value, ObservedAt: observedAt}, nil
}

// value resolves one item from the source type it declares.
func (r *Readings) value(ctx context.Context, session identity.Session, item extension.PageItem) (any, *controlplane.Error) {
	switch item.ID {
	case ItemTargetTypes:
		return targetTypes(session), nil
	case ItemNodeTargets:
		return r.nodeTargets(ctx, session)
	case ItemPodTargets:
		return r.podTargets(ctx, session)
	case ItemSessionState:
		return r.sessionState(ctx, session)
	case ItemSessionAudit:
		return r.sessionAudit(ctx, session)
	default:
		return nil, &controlplane.Error{
			Code:        controlplane.CodeInternal,
			Message:     "页面条目 " + item.ID + " 没有来源",
			Stage:       controlplane.StageRead,
			SideEffects: controlplane.SideEffectsNone,
		}
	}
}

// clock returns the moment this read observes its values (KOSD-C-13).
func (r *Readings) clock() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// targetTypes reports which target kinds the session may use and the
// execution identity each reaches the target as (CONSOLE-C-9, CONSOLE-C-10).
// A node is the global Owner's target; a Pod follows the account's existing
// application write permission.
func targetTypes(session identity.Session) map[string]any {
	return map[string]any{
		"node": map[string]any{
			"available":    session.Role == write.RoleOwner,
			"identity":     "root@node",
			"requiredRole": "Owner",
		},
		"pod": map[string]any{
			"available":    true,
			"requiredRole": "Member",
		},
	}
}

// nodeTargets resolves the node inventory. A role that may not connect to a
// node is answered with an empty list and the page's own reason, so the page
// never presents a node type it cannot act on (CONSOLE-C-10).
func (r *Readings) nodeTargets(ctx context.Context, session identity.Session) (any, *controlplane.Error) {
	if session.Role != write.RoleOwner || r.Targets == nil {
		return map[string]any{"items": []NodeTarget{}, "reason": NodeTargetsReason, "empty": EmptyTargetsReason}, nil
	}
	nodes, failure := r.Targets.NodeTargets(ctx, session)
	if failure != nil {
		return nil, failure
	}
	return map[string]any{"items": nodes, "reason": NodeTargetsReason, "empty": EmptyTargetsReason}, nil
}

// podTargets resolves the Pod container inventory, trimmed by the target
// reader to the applications the session may write (KOSD-C-93).
func (r *Readings) podTargets(ctx context.Context, session identity.Session) (any, *controlplane.Error) {
	if r.Targets == nil {
		return nil, capabilityMissing(ItemPodTargets)
	}
	pods, failure := r.Targets.PodTargets(ctx, session)
	if failure != nil {
		return nil, failure
	}
	return map[string]any{"items": pods, "reason": PodTargetsReason, "empty": EmptyTargetsReason}, nil
}

// sessionState lists the account's sessions together with the limits, the
// occupancy and the idle bound each reports (KOSD-C-90, KOSD-C-94).
func (r *Readings) sessionState(ctx context.Context, session identity.Session) (any, *controlplane.Error) {
	if r.Sessions == nil {
		return nil, capabilityMissing(ItemSessionState)
	}
	sessions, failure := r.Sessions.List(ctx, session)
	if failure != nil {
		return nil, failure
	}
	return map[string]any{"sessions": sessions, "rule": SessionStateReason}, nil
}

// sessionAudit reads the audit events of every session the account holds. Each
// row is metadata only: the read never returns raw input, output or token
// (KOSD-C-92, KOSD-C-96).
func (r *Readings) sessionAudit(ctx context.Context, session identity.Session) (any, *controlplane.Error) {
	if r.Sessions == nil || r.Audits == nil {
		return nil, capabilityMissing(ItemSessionAudit)
	}
	sessions, failure := r.Sessions.List(ctx, session)
	if failure != nil {
		return nil, failure
	}
	rows := make([]map[string]any, 0, len(sessions))
	for _, entry := range sessions {
		events, failure := r.Audits.Events(ctx, session, entry.SessionID)
		if failure != nil {
			return nil, failure
		}
		rows = append(rows, map[string]any{"sessionId": entry.SessionID, "events": events})
	}
	return map[string]any{"sessions": rows, "rule": SessionAuditReason}, nil
}

// capabilityMissing is the answer to a server-source item whose seam the
// deployment does not offer (KOSD-C-53): the page fails rather than showing a
// value it cannot stand behind.
func capabilityMissing(item string) *controlplane.Error {
	return &controlplane.Error{
		Code:        controlplane.CodeCapabilityMissing,
		Message:     "页面条目 " + item + " 的来源没有接入控制面",
		Stage:       controlplane.StageCapability,
		SideEffects: controlplane.SideEffectsNone,
	}
}
