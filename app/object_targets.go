package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/tsic404/kosd/api"
	"github.com/tsic404/kosd/api/v1beta1"
	"github.com/tsic404/kosd/pkg/controlplane"
	"github.com/tsic404/kosd/pkg/identity"
	"github.com/tsic404/kosd/pkg/write"
)

// PodCandidate is one Pod the projection of an application owns, as the
// control plane's Pod projection reports it (KOSD-C-76). The console only
// needs the immutable identity and the container names a session may select.
type PodCandidate struct {
	Namespace  string
	Name       string
	UID        string
	Containers []string
	ObservedAt time.Time
}

// PodProjector reads the Pods of one application (KOSD-C-76). The concrete
// projection lives with the control plane, which owns the workload walk; the
// console package only asks it for the candidates a session may connect to.
type PodProjector interface {
	Pods(ctx context.Context, namespace, name string) ([]PodCandidate, *controlplane.Error)
}

// ObjectTargets resolves the two inventories from the control plane's object
// source, applying the account's existing permission at every step
// (KOSD-C-93). It holds no cluster credential of its own.
type ObjectTargets struct {
	// Source reads the node and application objects the inventories come from.
	Source controlplane.ObjectSource
	// Pods projects the Pods of each writable application.
	Pods PodProjector
	// Now supplies the read clock the stale marker compares against; nil
	// defaults to time.Now.
	Now func() time.Time
}

// nodeType is the native node class the terminal target identity names
// (KOSD-C-95).
func nodeType() controlplane.ObjectType {
	return controlplane.ObjectType{Version: "v1", Kind: "Node", Resource: "nodes"}
}

// applicationType is the kernel application class.
func applicationType() controlplane.ObjectType {
	resource, _ := api.ResourceName("Application")
	return controlplane.ObjectType{
		Group:      api.Group,
		Version:    v1beta1.GroupVersion.Version,
		Kind:       "Application",
		Resource:   resource,
		Namespaced: true,
	}
}

// NodeTargets returns every node the session may connect to. Nodes are the
// global Owner's targets alone (KOSD-C-93), and a node carries empty
// namespace and container strings (KOSD-C-95).
func (t *ObjectTargets) NodeTargets(ctx context.Context, session identity.Session) ([]NodeTarget, *controlplane.Error) {
	if session.Role != write.RoleOwner {
		return []NodeTarget{}, nil
	}
	observed, err := t.Source.List(ctx, nodeType(), "")
	if err != nil {
		return nil, readFailure("节点目标来源不可用", err)
	}
	now := t.clock()
	targets := make([]NodeTarget, 0, len(observed))
	for _, entry := range observed {
		targets = append(targets, NodeTarget{
			TargetType:          "node",
			TargetID:            nestedText(entry.Object, "metadata", "name"),
			TargetUID:           nestedText(entry.Object, "metadata", "uid"),
			Namespace:           "",
			Container:           "",
			ExecutionIdentities: []string{"root@node"},
			ObservedAt:          entry.ObservedAt,
			Stale:               now.Sub(entry.ObservedAt) > controlplane.StaleThreshold,
		})
	}
	return targets, nil
}

// PodTargets returns the Pod containers of the applications the session may
// write. An application the account may not write contributes no candidate:
// visibility does not grant a connection (CONSOLE-C-10).
func (t *ObjectTargets) PodTargets(ctx context.Context, session identity.Session) ([]PodTarget, *controlplane.Error) {
	observed, err := t.Source.List(ctx, applicationType(), "")
	if err != nil {
		return nil, readFailure("应用目标来源不可用", err)
	}
	now := t.clock()
	targets := make([]PodTarget, 0)
	for _, entry := range observed {
		application, failure := decodeApplication(entry.Object)
		if failure != nil {
			return nil, failure
		}
		if application.Spec.Paused || !mayWriteApplication(session, application) {
			continue
		}
		namespace := application.Spec.Namespace
		if namespace == "" {
			namespace = application.Namespace
		}
		candidates, failure := t.Pods.Pods(ctx, namespace, application.Name)
		if failure != nil {
			return nil, failure
		}
		for _, candidate := range candidates {
			targets = append(targets, PodTarget{
				Namespace:         candidate.Namespace,
				Application:       application.Name,
				Pod:               candidate.Name,
				TargetUID:         candidate.UID,
				Containers:        candidate.Containers,
				SelectedContainer: selectedContainer(candidate.Containers),
				ObservedAt:        candidate.ObservedAt,
				Stale:             now.Sub(candidate.ObservedAt) > controlplane.StaleThreshold,
			})
		}
	}
	return targets, nil
}

// selectedContainer preselects the one container of a single-container Pod and
// leaves a multi-container Pod unselected (CONSOLE-C-8).
func selectedContainer(containers []string) string {
	if len(containers) == 1 {
		return containers[0]
	}
	return ""
}

// mayWriteApplication applies the session's existing permission to one
// application (KOSD-C-59, KOSD-C-93): the global Owner writes every
// application, a Member only the ones it owns, and a Guest none.
func mayWriteApplication(session identity.Session, application v1beta1.Application) bool {
	switch session.Role {
	case write.RoleOwner:
		return true
	case write.RoleMember:
		return application.Spec.Owner == session.Username
	default:
		return false
	}
}

// decodeApplication reads one stored application object.
func decodeApplication(object map[string]any) (v1beta1.Application, *controlplane.Error) {
	encoded, err := json.Marshal(object)
	if err != nil {
		return v1beta1.Application{}, readFailure("应用目标无法解析", err)
	}
	var application v1beta1.Application
	if err := json.Unmarshal(encoded, &application); err != nil {
		return v1beta1.Application{}, readFailure("应用目标无法解析", err)
	}
	return application, nil
}

// clock returns the read clock.
func (t *ObjectTargets) clock() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

// readFailure turns a source failure into the control-plane read error the
// page surface reports; the underlying error is named and never swallowed.
func readFailure(message string, err error) *controlplane.Error {
	return &controlplane.Error{
		Code:        controlplane.CodeClusterUnavailable,
		Message:     message,
		Stage:       controlplane.StageRead,
		Retryable:   true,
		SideEffects: controlplane.SideEffectsNone,
	}
}

// nestedText reads one string field of an unstructured object.
func nestedText(object map[string]any, fields ...string) string {
	var current any = object
	for _, field := range fields {
		part, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = part[field]
	}
	text, _ := current.(string)
	return text
}
