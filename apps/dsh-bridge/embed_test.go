package dshbridge

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestTarball(t *testing.T) {
	a, err := Tarball()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Tarball()
	if !bytes.Equal(a, b) {
		t.Fatal("not the same bytes twice")
	}
	gz, err := gzip.NewReader(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	got := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got[h.Name] = true
	}
	for _, want := range []string{"package/package.json", "package/cordis.patch.yml", "package/README.md", "package/lib/index.js", "package/lib/server.js", "package/lib/host.js"} {
		if !got[want] {
			t.Errorf("no %s in %v", want, got)
		}
	}
	if Version() == "" {
		t.Error("no version")
	}
}
