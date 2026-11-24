// Package artifact builds this repository's extension package: the declaration
// in package app plus every UI asset under assets/, packed through kos-sdk.
//
// The build reads no product identity and no install source, and it verifies
// its own output with the same check an install path runs before writing the
// package, so a caller never holds an artifact the install path would reject
// (SDK-C-4).
package artifact

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"

	"github.com/tsic404/kos-sdk/packaging"

	"github.com/tsic404/kos-console/app"
)

// Options is one package build.
type Options struct {
	// Root is the repository root the asset directory lives under.
	Root string
	// AssetsDir is the asset directory relative to Root; "assets" when empty.
	AssetsDir string
}

// AssetPaths returns the package-relative names of every UI asset, sorted. The
// names are exactly the archive entries: the packer adds no asset the repository
// does not carry (SDK-C-4).
func AssetPaths(options Options) ([]string, error) {
	root := options.AssetsDir
	if root == "" {
		root = "assets"
	}
	base := filepath.Join(options.Root, root)
	var names []string
	err := filepath.WalkDir(base, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(base, name)
		if err != nil {
			return err
		}
		names = append(names, path.Join(root, filepath.ToSlash(relative)))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	sort.Strings(names)
	return names, nil
}

// Assets reads every UI asset the repository carries.
func Assets(options Options) ([]packaging.Asset, error) {
	names, err := AssetPaths(options)
	if err != nil {
		return nil, err
	}
	assets := make([]packaging.Asset, 0, len(names))
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(options.Root, filepath.FromSlash(name)))
		if err != nil {
			return nil, fmt.Errorf("read asset %s: %w", name, err)
		}
		assets = append(assets, packaging.Asset{Path: name, Content: content})
	}
	return assets, nil
}

// Build packs the declaration with the repository's assets and returns the
// package together with the archive bytes an install path receives.
func Build(options Options) (packaging.Package, []byte, error) {
	assets, err := Assets(options)
	if err != nil {
		return packaging.Package{}, nil, err
	}
	declaration, err := declarationDocument()
	if err != nil {
		return packaging.Package{}, nil, err
	}
	pkg, archive, err := packaging.Pack(declaration, assets)
	if err != nil {
		return packaging.Package{}, nil, err
	}
	// Verifying the produced bytes keeps the CLI and every case on one structure:
	// the archive a caller writes is the one Verify accepts.
	verified, err := packaging.Verify(archive, pkg.Digest)
	if err != nil {
		return packaging.Package{}, nil, fmt.Errorf("verify the packed artifact: %w", err)
	}
	if verified.Digest != pkg.Digest {
		return packaging.Package{}, nil, fmt.Errorf(
			"verify read digest %s, the packer wrote %s", verified.Digest, pkg.Digest)
	}
	return pkg, archive, nil
}

// BuildFiles packs an explicit asset set, which is how a case holds every input
// the digest covers and changes exactly one of them.
func BuildFiles(assets []packaging.Asset) (packaging.Package, []byte, error) {
	declaration, err := declarationDocument()
	if err != nil {
		return packaging.Package{}, nil, err
	}
	return packaging.Pack(declaration, assets)
}

// declarationDocument is the declaration as the packer validates it: the SDK
// checks the JSON field set, so the struct is round-tripped rather than handed
// over as a Go value.
func declarationDocument() (any, error) {
	encoded, err := json.Marshal(app.Declaration())
	if err != nil {
		return nil, fmt.Errorf("encode the declaration: %w", err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, fmt.Errorf("decode the declaration: %w", err)
	}
	return document, nil
}
