// Package app declares the kos-console extension package: one native
// "terminal" page the control plane shell renders (CONSOLE-C-8, DASH-C-22) and
// the cluster-scoped idle bound every session reads (KOSD-C-94). The
// declaration is the single source of the packed manifest, so the artifact,
// the page registry and the acceptance cases all read the same field set
// (KOSD-C-17, KOSD-C-18).
package app

import (
	"encoding/json"

	"github.com/tsic404/kos-sdk/manifest"
	"github.com/tsic404/kosd/pkg/extension"
)

const (
	// Identifier is the extension identity across versions (KOSD-C-17).
	Identifier = "kos-console"
	// Version is the extension version this package ships.
	Version = "0.1.0"
	// FrameworkVersion is the framework API range the package targets.
	FrameworkVersion = ">=1.0 <2.0"

	// PageTerminal is the native terminal page the shell shows (DASH-C-22).
	PageTerminal = "terminal"
	// SettingIdleMinutes is the cluster-scoped idle bound the terminal
	// component contributes (KOSD-C-94).
	SettingIdleMinutes = "terminal.idleTimeoutMinutes"
)

// Control-plane addresses the page and the browser drive (KOSD-C-12,
// KOSD-C-90). Both are the control plane's own, so the browser never reaches
// the runtime kernel and holds no cluster credential.
const (
	// PathTerminalSessions creates one terminal session.
	PathTerminalSessions = "/api/v1/terminal/sessions"
	// PathTerminalSession deletes (ends) one terminal session.
	PathTerminalSession = "/api/v1/terminal/sessions/{sessionId}"
)

// Declaration is the extension declaration the packer writes (KOSD-C-18). It
// contributes the terminal page and the idle setting and nothing else, so
// installing, disabling or removing the package touches no other contribution
// and no cluster object (KOSD-C-17).
func Declaration() manifest.ExtensionManifest {
	return manifest.ExtensionManifest{
		Identifier:       Identifier,
		Version:          Version,
		FrameworkVersion: FrameworkVersion,
		Contributions: manifest.Contributions{
			Pages:    pageDocuments(),
			Settings: settingDocuments(),
		},
		Dependencies:  manifest.Dependencies{},
		Permissions:   []manifest.ExtensionPermission{},
		Configuration: manifest.Configuration{},
		// The page and the setting are the component's own; uninstall
		// withdraws both (KOSD-C-18).
		Retention: []manifest.Retention{
			{Resource: "pages", Keep: false},
			{Resource: "settings", Keep: false},
		},
	}
}

// IdleSetting is the idle bound contribution (KOSD-C-94): a cluster-scoped,
// user-written setting whose value is an integer minute count.
func IdleSetting() extension.Setting {
	return extension.Setting{
		ID:       SettingIdleMinutes,
		Scope:    extension.SettingScopeCluster,
		Writer:   extension.SettingWriterUser,
		Editable: true,
	}
}

// pageDocuments renders the declared page into the manifest's page payload,
// which the SDK validates structurally and the kernel loads by its own field
// set. Rendering the same Go declaration keeps the shipped manifest and the
// served page from drifting (KOSD-C-18, DASH-C-22).
func pageDocuments() []map[string]any {
	encoded, err := json.Marshal([]extension.Page{TerminalPage()})
	if err != nil {
		panic("app: the terminal page declaration cannot be encoded: " + err.Error())
	}
	var documents []map[string]any
	if err := json.Unmarshal(encoded, &documents); err != nil {
		panic("app: the terminal page declaration cannot be decoded: " + err.Error())
	}
	return documents
}

// settingDocuments renders the declared setting into the manifest's setting
// payload, the same round trip the page declaration takes.
func settingDocuments() []map[string]any {
	encoded, err := json.Marshal([]extension.Setting{IdleSetting()})
	if err != nil {
		panic("app: the terminal idle setting cannot be encoded: " + err.Error())
	}
	var documents []map[string]any
	if err := json.Unmarshal(encoded, &documents); err != nil {
		panic("app: the terminal idle setting cannot be decoded: " + err.Error())
	}
	return documents
}
