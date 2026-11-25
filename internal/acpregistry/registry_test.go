package acpregistry

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func buildZipArchive(t *testing.T, files map[string][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func buildTarGzArchive(t *testing.T, files map[string][]byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, data := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func TestEmbeddedFallbackAndRemoteSync(t *testing.T) {
	// 1. Offline / broken URL falls back cleanly to embedded snapshot.
	regOffline := New(Options{
		DataDir: t.TempDir(),
		URL:     "http://127.0.0.1:1/unreachable",
	})
	res := regOffline.List(context.Background(), true)
	if res.Source != "embedded" {
		t.Fatalf("expected embedded fallback, got source=%q", res.Source)
	}
	if len(res.Entries) == 0 {
		t.Fatalf("expected embedded entries to be non-empty")
	}
	if res.LastError == "" {
		t.Fatalf("expected lastError recorded on remote failure")
	}
	for _, e := range res.Entries {
		if strings.EqualFold(e.ID, "antigravity") {
			t.Fatalf("antigravity must not be bundled in generic ACP registry")
		}
	}

	// 2. Healthy remote index syncs and persists disk cache.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Index{
			Version: "1.0.0",
			Agents: []Entry{
				{
					ID:      "demo-acp",
					Name:    "Demo ACP",
					Version: "2.0.0",
					Distribution: Distribution{
						NPX: &PackageTarget{Package: "@example/demo-acp@2.0.0", Args: []string{"--acp"}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	reg := New(Options{DataDir: dataDir, URL: srv.URL})
	res = reg.List(context.Background(), false)
	if res.Source != "remote" || len(res.Entries) != 1 || res.Entries[0].ID != "demo-acp" {
		t.Fatalf("unexpected remote list result: %+v", res)
	}

	// 3. New Registry pointing at unreachable URL still serves fresh disk cache.
	reg2 := New(Options{DataDir: dataDir, URL: "http://127.0.0.1:1/unreachable"})
	res2 := reg2.List(context.Background(), false)
	if res2.Source != "cache" || len(res2.Entries) != 1 || res2.Entries[0].ID != "demo-acp" {
		t.Fatalf("expected disk cache hit, got %+v", res2)
	}
}

func TestInstallBinarySHA256AndUninstall(t *testing.T) {
	plat := CurrentPlatform()
	cmdName := "bin/demo-agent"
	if runtime.GOOS == "windows" {
		cmdName = "bin/demo-agent.exe"
	}
	zipBytes, goodSum := buildZipArchive(t, map[string][]byte{
		cmdName: []byte("fake-binary-payload"),
	})
	tgzBytes, tgzSum := buildTarGzArchive(t, map[string][]byte{
		cmdName: []byte("fake-tgz-payload"),
	})
	evilZip, evilSum := buildZipArchive(t, map[string][]byte{
		"../escape.exe": []byte("evil"),
	})

	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(Index{
				Version: "1.0.0",
				Agents: []Entry{
					{
						ID:      "good-zip",
						Name:    "Good Zip",
						Version: "1.0.0",
						Distribution: Distribution{
							Binary: map[string]BinaryTarget{
								plat: {Archive: srvURL + "/good.zip", Cmd: "./bin/demo-agent", Args: []string{"--acp"}, SHA256: goodSum},
							},
						},
					},
					{
						ID:      "bad-hash",
						Name:    "Bad Hash",
						Version: "1.0.0",
						Distribution: Distribution{
							Binary: map[string]BinaryTarget{
								plat: {Archive: srvURL + "/good.zip", Cmd: "./bin/demo-agent", SHA256: strings.Repeat("0", 64)},
							},
						},
					},
					{
						ID:      "good-tgz",
						Name:    "Good TGZ",
						Version: "1.2.0",
						Distribution: Distribution{
							Binary: map[string]BinaryTarget{
								plat: {Archive: srvURL + "/good.tar.gz", Cmd: "./bin/demo-agent", SHA256: tgzSum},
							},
						},
					},
					{
						ID:      "evil-zip",
						Name:    "Evil Zip",
						Version: "1.0.0",
						Distribution: Distribution{
							Binary: map[string]BinaryTarget{
								plat: {Archive: srvURL + "/evil.zip", Cmd: "./bin/demo-agent", SHA256: evilSum},
							},
						},
					},
				},
			})
		case "/good.zip":
			_, _ = w.Write(zipBytes)
		case "/good.tar.gz":
			_, _ = w.Write(tgzBytes)
		case "/evil.zip":
			_, _ = w.Write(evilZip)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	dataDir := t.TempDir()
	reg := New(Options{DataDir: dataDir, URL: srvURL + "/registry.json"})

	// Antigravity must be refused (#111).
	if _, err := reg.Install(context.Background(), "antigravity", "", nil); err == nil || !strings.Contains(err.Error(), "dedicated built-in provider") {
		t.Fatalf("expected antigravity install refusal, got %v", err)
	}

	// Bad SHA-256 must be rejected before extraction.
	if _, err := reg.Install(context.Background(), "bad-hash", "", nil); err == nil || !strings.Contains(err.Error(), "SHA-256 verification failed") {
		t.Fatalf("expected SHA-256 failure, got %v", err)
	}

	// Zip Slip path traversal must be rejected.
	if _, err := reg.Install(context.Background(), "evil-zip", "", nil); err == nil || !strings.Contains(err.Error(), "escapes target directory") {
		t.Fatalf("expected zip slip rejection, got %v", err)
	}

	// Valid zip installs isolated under dataDir/acp-registry/installed/<name>/<version>.
	inst, err := reg.Install(context.Background(), "good-zip", "", nil)
	if err != nil {
		t.Fatalf("install good-zip: %v", err)
	}
	if inst.Distribution != "binary" || inst.SHA256 != goodSum {
		t.Fatalf("unexpected install result: %+v", inst)
	}
	if !strings.HasPrefix(inst.InstallDir, filepath.Join(dataDir, "acp-registry", "installed", "good-zip")) {
		t.Fatalf("install dir not isolated under dataDir: %q", inst.InstallDir)
	}
	if _, err := os.Stat(inst.Command); err != nil {
		t.Fatalf("installed command missing: %v", err)
	}

	// Valid tar.gz also installs cleanly.
	instTgz, err := reg.Install(context.Background(), "good-tgz", "", nil)
	if err != nil {
		t.Fatalf("install good-tgz: %v", err)
	}
	if instTgz.SHA256 != tgzSum {
		t.Fatalf("unexpected tgz sha256: %q", instTgz.SHA256)
	}

	// Uninstall cleans up machine-isolated directory.
	if err := reg.Uninstall("good-zip"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(inst.InstallDir); !os.IsNotExist(err) {
		t.Fatalf("expected install dir removed, stat err=%v", err)
	}
}
