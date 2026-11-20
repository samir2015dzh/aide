// Package dshbridge carries the Aide bridge plugin for DSH Desktop
// (this folder's npm package, built into lib/) inside aided, which
// writes it out as an npm tarball for DSH's plugin manager to install
// (docs/dsh-takeover.md).
package dshbridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"time"
)

//go:embed package.json cordis.patch.yml README.md lib/*.js
var files embed.FS

// Version is the plugin's version, from its package.json.
func Version() string {
	b, _ := files.ReadFile("package.json")
	var p struct{ Version string }
	json.Unmarshal(b, &p)
	return p.Version
}

// Tarball is the plugin as npm packs it: its files under package/, the
// same bytes each time.
func Tarball() ([]byte, error) {
	var names []string
	fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			names = append(names, p)
		}
		return err
	})
	sort.Strings(names)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	when := time.Date(1985, 10, 26, 8, 15, 0, 0, time.UTC) // npm's fixed time
	for _, n := range names {
		b, err := files.ReadFile(n)
		if err != nil {
			return nil, err
		}
		if err := tw.WriteHeader(&tar.Header{Name: path.Join("package", n), Mode: 0o644, Size: int64(len(b)), ModTime: when, Typeflag: tar.TypeReg}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(b); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
