// Package ac72 checks KOSD-AC-72 for this repository's own artifact: the
// component packs through kos-sdk as one manifest plus its assets, verifies with
// the digest the install path checks, declares only its own page and setting, and
// changes digest when an asset changes (SDK-C-3, SDK-C-4, KOSD-C-17, KOSD-C-18).
package ac72

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tsic404/kos-sdk/packaging"

	"github.com/tsic404/kos-console/app"
	"github.com/tsic404/kos-console/internal/artifact"
)

// repositoryRoot is the checkout the package is built from.
func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	return root
}

// TestArtifactPacksAndVerifies checks the built package is the one the install
// path accepts and carries only the declared contributions.
func TestArtifactPacksAndVerifies(t *testing.T) {
	pkg, archive, err := artifact.Build(artifact.Options{Root: repositoryRoot(t)})
	if err != nil {
		t.Fatalf("build artifact: %v", err)
	}
	if pkg.Manifest.Identifier != app.Identifier || pkg.Manifest.Version != app.Version {
		t.Fatalf("manifest identity = %s@%s, want %s@%s",
			pkg.Manifest.Identifier, pkg.Manifest.Version, app.Identifier, app.Version)
	}
	if len(pkg.Assets) == 0 {
		t.Fatal("the artifact carries no UI assets")
	}
	for _, asset := range pkg.Assets {
		if !strings.HasPrefix(asset.Path, packaging.AssetPrefix) {
			t.Fatalf("asset %q is outside the package's asset namespace", asset.Path)
		}
	}

	verified, err := packaging.Verify(archive, pkg.Digest)
	if err != nil {
		t.Fatalf("verify artifact: %v", err)
	}
	if verified.Digest != pkg.Digest {
		t.Fatalf("verified digest = %s, want %s", verified.Digest, pkg.Digest)
	}

	contributions := verified.Manifest.Contributions
	if len(contributions.Pages) != 1 || len(contributions.Settings) != 1 {
		t.Fatalf("contributions = %d pages, %d settings, want one each",
			len(contributions.Pages), len(contributions.Settings))
	}
	if len(contributions.Apps) != 0 || len(contributions.Controllers) != 0 ||
		len(contributions.ObjectClasses) != 0 || contributions.Capabilities != nil {
		t.Fatal("the artifact must not declare an application, controller, class or capability")
	}
	if len(verified.Manifest.Permissions) != 0 {
		t.Fatal("the artifact must not declare a native permission")
	}
	retained := map[string]bool{}
	for _, retention := range verified.Manifest.Retention {
		retained[retention.Resource] = retention.Keep
	}
	if retained["pages"] || retained["settings"] {
		t.Fatalf("retention = %+v, want the component's own declarations withdrawn", retained)
	}
}

// TestDigestCoversEveryAsset checks identical inputs pack identically and a
// changed asset changes the digest the install path compares (SDK-C-4).
func TestDigestCoversEveryAsset(t *testing.T) {
	first := []packaging.Asset{
		{Path: "assets/index.html", Content: []byte("<main>terminal</main>")},
		{Path: "assets/icon.svg", Content: []byte("<svg/>")},
	}
	second := []packaging.Asset{
		{Path: "assets/icon.svg", Content: []byte("<svg/>")},
		{Path: "assets/index.html", Content: []byte("<main>terminal</main>")},
	}
	original, originalArchive, err := artifact.BuildFiles(first)
	if err != nil {
		t.Fatalf("pack first set: %v", err)
	}
	reordered, _, err := artifact.BuildFiles(second)
	if err != nil {
		t.Fatalf("pack reordered set: %v", err)
	}
	if original.Digest != reordered.Digest {
		t.Fatalf("digests differ for the same contents: %s vs %s", original.Digest, reordered.Digest)
	}

	changed, _, err := artifact.BuildFiles([]packaging.Asset{
		{Path: "assets/index.html", Content: []byte("<main>terminal v2</main>")},
		{Path: "assets/icon.svg", Content: []byte("<svg/>")},
	})
	if err != nil {
		t.Fatalf("pack changed set: %v", err)
	}
	if changed.Digest == original.Digest {
		t.Fatal("a changed asset must change the digest")
	}
	if _, err := packaging.Verify(originalArchive, changed.Digest); err == nil {
		t.Fatal("the changed digest must not verify the original archive")
	}
}
