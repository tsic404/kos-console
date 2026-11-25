package app_test

import (
	"encoding/json"
	"testing"

	"github.com/tsic404/kos-sdk/manifest"
	"github.com/tsic404/kosd/pkg/extension"
	"github.com/tsic404/kosd/pkg/write"

	"github.com/tsic404/kos-console/app"
)

// TestDeclarationIsAValidExtensionManifest checks the declaration the packer
// writes passes the SDK's structural check, so the artifact is installable
// (KOSD-C-18, SDK-C-3).
func TestDeclarationIsAValidExtensionManifest(t *testing.T) {
	encoded, err := json.Marshal(app.Declaration())
	if err != nil {
		t.Fatalf("encode declaration: %v", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode declaration: %v", err)
	}
	checked := manifest.ValidateExtensionManifest(document)
	if !checked.OK() {
		t.Fatalf("declaration is invalid: %+v", checked.Issues)
	}
	if checked.Manifest.Identifier != app.Identifier || checked.Manifest.Version != app.Version {
		t.Fatalf("declaration identity = %s@%s, want %s@%s",
			checked.Manifest.Identifier, checked.Manifest.Version, app.Identifier, app.Version)
	}
}

// TestDeclarationContributesThePageAndTheSetting checks the two contributions
// the package declares and that it declares no window application: the terminal
// page is a native page the shell renders, not an iframe (DASH-C-22).
func TestDeclarationContributesThePageAndTheSetting(t *testing.T) {
	encoded, err := json.Marshal(app.Declaration())
	if err != nil {
		t.Fatalf("encode declaration: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode declaration: %v", err)
	}
	contributions, _ := document["contributions"].(map[string]any)
	if _, present := contributions["apps"]; present {
		t.Fatal("the terminal page declares no iframe application")
	}
	pages, _ := contributions["pages"].([]any)
	if len(pages) != 1 {
		t.Fatalf("pages = %v, want exactly the terminal page", pages)
	}
	page, _ := pages[0].(map[string]any)
	if page["id"] != app.PageTerminal {
		t.Fatalf("page id = %v, want %s", page["id"], app.PageTerminal)
	}
	settings, _ := contributions["settings"].([]any)
	if len(settings) != 1 {
		t.Fatalf("settings = %v, want exactly the idle bound", settings)
	}
	setting, _ := settings[0].(map[string]any)
	if setting["id"] != app.SettingIdleMinutes || setting["scope"] != extension.SettingScopeCluster {
		t.Fatalf("setting = %v, want the cluster idle bound", setting)
	}
}

// TestTerminalPageSourcesAndActions checks the page declares the source types
// the design fixes and the two control-plane writes the browser submits
// (DASH-C-52, DASH-C-22).
func TestTerminalPageSourcesAndActions(t *testing.T) {
	page := app.TerminalPage()
	wantSources := map[string]string{
		app.ItemTargetTypes:  extension.PageSourceComputed,
		app.ItemNodeTargets:  extension.PageSourceObject,
		app.ItemPodTargets:   extension.PageSourceObject,
		app.ItemSessionState: extension.PageSourceRead,
		app.ItemSessionAudit: extension.PageSourceDatabase,
	}
	if len(page.Items) != len(wantSources) {
		t.Fatalf("items = %d, want %d", len(page.Items), len(wantSources))
	}
	for _, item := range page.Items {
		if want, known := wantSources[item.ID]; !known {
			t.Fatalf("unexpected item %q", item.ID)
		} else if item.Source != want {
			t.Fatalf("item %q source = %q, want %q", item.ID, item.Source, want)
		}
	}
	if len(page.Actions) != 2 {
		t.Fatalf("actions = %+v, want the create and end entries", page.Actions)
	}
}

// TestTerminalPageAdmission checks a Guest has no entry while a Member and the
// Owner do (CONSOLE-C-10).
func TestTerminalPageAdmission(t *testing.T) {
	page := app.TerminalPage()
	if !page.Allows(write.RoleMember) || !page.Allows(write.RoleOwner) {
		t.Fatal("a Member and the Owner must see the terminal page")
	}
	if page.Allows(write.RoleGuest) {
		t.Fatal("a Guest must not see the terminal page")
	}
}
