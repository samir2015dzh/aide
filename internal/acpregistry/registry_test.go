package acpregistry

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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
		if DedicatedBuiltin(e) != "" {
			if kind, _, reason := BestDistribution(e, CurrentPlatform()); kind != "" || !strings.Contains(reason, "dedicated built-in") {
				t.Fatalf("%s must not be installable via the generic ACP registry: %q %q", e.ID, kind, reason)
			}
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
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	reg := New(Options{DataDir: dataDir, URL: srvURL + "/registry.json", HTTPClient: srv.Client()})

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

// tarBz2Fixture is a .tar.bz2 holding bin/demo-agent ("fake-bz2-payload");
// the standard library cannot write bzip2.
const tarBz2Fixture = "QlpoOTFBWSZTWUnbWw0AAHJ7gMqAEABAAv6AEAB3r94wCAggAHUVlMgDQB6h6Rk009QSSobQ1MEwjAaJh1u88IQXekIRRt8kI3s5AhgYhfXCp3lkOYBRTArVf2bViW9INJxR4Y44G9J4ChxGUrqEeiMtIAQdi7kinChIJO2thoA="

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// 审计 D: what is offered as installable is what Install does.
func TestBestDistributionHonesty(t *testing.T) {
	plat := "linux-x86_64"
	bin := func(archive, sha string) map[string]BinaryTarget {
		return map[string]BinaryTarget{plat: {Archive: archive, Cmd: "./agent", SHA256: sha}}
	}
	good := strings.Repeat("a", 64)
	npx := &PackageTarget{Package: "@example/agent@1.0.0"}
	for _, c := range []struct {
		name     string
		e        Entry
		kind     string
		verified bool
		reason   string
	}{
		{"binary with sha", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.zip", good)}}, "binary", true, ""},
		{"sha256: prefix", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.zip", "sha256:"+strings.ToUpper(good))}}, "binary", true, ""},
		{"no sha, no fallback", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.zip", "")}}, "", false, "no SHA-256"},
		{"no sha, npx fallback", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.zip", ""), NPX: npx}}, "npx", false, "no SHA-256"},
		{"http url", Entry{ID: "a", Distribution: Distribution{Binary: bin("http://example.com/a.zip", good)}}, "", false, "not an https URL"},
		{"tar.xz", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.tar.xz", good)}}, "", false, "format Aide cannot unpack"},
		{"tar.bz2", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/a.tar.bz2", good)}}, "binary", true, ""},
		{"bare executable", Entry{ID: "a", Distribution: Distribution{Binary: bin("https://example.com/download/agent-linux-amd64", good)}}, "binary", true, ""},
		{"antigravity-acp", Entry{ID: "antigravity-acp", Distribution: Distribution{Binary: bin("https://example.com/a.zip", good)}}, "", false, "dedicated built-in"},
		{"antigravity by name", Entry{ID: "agy", Name: "Google Antigravity", Distribution: Distribution{NPX: npx}}, "", false, "dedicated built-in"},
	} {
		kind, verified, reason := BestDistribution(c.e, plat)
		if kind != c.kind || verified != c.verified || (c.reason == "") != (reason == "") || !strings.Contains(reason, c.reason) {
			t.Errorf("%s: got %q %v %q", c.name, kind, verified, reason)
		}
	}
}

func TestInstallRefusals(t *testing.T) {
	plat := CurrentPlatform()
	zipBytes, zipSum := buildZipArchive(t, map[string][]byte{"agent": []byte("payload")})
	var srvURL string
	var downloads int
	var mu sync.Mutex
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/registry.json" {
			_ = json.NewEncoder(w).Encode(Index{Version: "1.0.0", Agents: []Entry{
				{ID: "no-sha", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: srvURL + "/a.zip", Cmd: "./agent"}}}},
				// Plain http off this computer (loopback http is allowed: TestSecureDownload).
				{ID: "plain-http", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: "http://192.0.2.10/a.zip", Cmd: "./agent", SHA256: zipSum}}}},
				{ID: "antigravity-acp", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: srvURL + "/a.zip", Cmd: "./agent", SHA256: zipSum}}}},
				{ID: "good", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: srvURL + "/a.zip", Cmd: "./agent", SHA256: zipSum}}}},
			}})
			return
		}
		mu.Lock()
		downloads++
		mu.Unlock()
		_, _ = w.Write(zipBytes)
	}))
	defer srv.Close()
	srvURL = srv.URL
	reg := New(Options{DataDir: t.TempDir(), URL: srvURL + "/registry.json", HTTPClient: srv.Client()})
	ctx := context.Background()
	for _, c := range []struct{ id, name, want string }{
		{"no-sha", "", "no SHA-256"},
		{"plain-http", "", "not an https URL"},
		{"antigravity-acp", "", "dedicated built-in"},
		{"good", "Bad Name", "not one Aide can use"},
		{"good", "taken", "already used by another agent"},
	} {
		_, err := reg.Install(ctx, c.id, c.name, func(n string) bool { return n == "taken" })
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v", c.id, c.name, err)
		}
	}
	mu.Lock()
	n := downloads
	mu.Unlock()
	if n != 0 {
		t.Fatalf("refused installs downloaded %d times", n)
	}
	if inst, err := reg.Install(ctx, "good", "mine", func(n string) bool { return n == "taken" }); err != nil || inst.Name != "mine" {
		t.Fatalf("explicit free name: %+v %v", inst, err)
	}
}

func TestInstallTarBz2AndBareExecutable(t *testing.T) {
	plat := CurrentPlatform()
	bz, _ := base64.StdEncoding.DecodeString(tarBz2Fixture)
	exe := []byte("#!/bin/sh\necho bare\n")
	var srvURL string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/registry.json":
			_ = json.NewEncoder(w).Encode(Index{Version: "1.0.0", Agents: []Entry{
				{ID: "bz", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: srvURL + "/agent.tar.bz2", Cmd: "./bin/demo-agent", SHA256: sum(bz)}}}},
				{ID: "bare", Version: "1", Distribution: Distribution{Binary: map[string]BinaryTarget{plat: {Archive: srvURL + "/download/?file=agent-linux", Cmd: "./agent", SHA256: sum(exe)}}}},
			}})
		case "/agent.tar.bz2":
			_, _ = w.Write(bz)
		case "/download/":
			_, _ = w.Write(exe)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL
	reg := New(Options{DataDir: t.TempDir(), URL: srvURL + "/registry.json", HTTPClient: srv.Client()})
	inst, err := reg.Install(context.Background(), "bz", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(inst.Command); string(b) != "fake-bz2-payload" {
		t.Fatalf("tar.bz2 contents: %q", b)
	}
	inst, err = reg.Install(context.Background(), "bare", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(inst.Command)
	st, _ := os.Stat(inst.Command)
	if string(b) != string(exe) || filepath.Base(inst.Command) != "agent" || (runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0) {
		t.Fatalf("bare executable: %s %q %v", inst.Command, b, st.Mode())
	}
}

// 审计 D: a large download on a slow line succeeds while bytes keep coming;
// one that stops sending fails on the idle timeout, not a total one.
func TestDownloadIdleTimeout(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		for i := 0; i < 8; i++ {
			w.Write([]byte("chunk"))
			fl.Flush()
			if r.URL.Path == "/stall" && i == 1 {
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer srv.Close()
	reg := New(Options{DataDir: t.TempDir(), HTTPClient: srv.Client()})
	reg.idle = 200 * time.Millisecond
	dst := filepath.Join(t.TempDir(), "f")
	start := time.Now()
	if _, err := reg.downloadAndHash(context.Background(), srv.URL+"/slow", dst); err != nil {
		t.Fatalf("slow but steady download (%s): %v", time.Since(start), err)
	}
	if time.Since(start) <= reg.idle {
		t.Fatalf("the slow download should outlast the idle timeout")
	}
	if _, err := reg.downloadAndHash(context.Background(), srv.URL+"/stall", dst); err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stalled download: %v", err)
	}
	// The production download client has no total timeout.
	if New(Options{}).download.Timeout != 0 {
		t.Fatal("downloads must not have a total timeout")
	}
}

// 审计 D: offline, List serves its fallback for a while instead of waiting
// on the network every time.
func TestListCachesFailure(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	reg := New(Options{DataDir: t.TempDir(), URL: srv.URL})
	for i := 0; i < 3; i++ {
		if res := reg.List(context.Background(), false); res.Source != "embedded" || res.LastError == "" {
			t.Fatalf("list %d: %+v", i, res.Source)
		}
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return hits }
	if n := count(); n != 1 {
		t.Fatalf("the registry was asked %d times; want 1 while the failure is cached", n)
	}
	reg.List(context.Background(), true) // an explicit refresh tries again
	if n := count(); n != 2 {
		t.Fatalf("forced refresh: %d hits", n)
	}
}

// The embedded snapshot is the official index: its antigravity entry is the
// Registry's id, guarded; it has no ids the Registry does not.
func TestEmbeddedSnapshotIsOfficial(t *testing.T) {
	idx, err := EmbeddedIndex()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]Entry{}
	for _, e := range idx.Agents {
		ids[e.ID] = e
	}
	for _, wrong := range []string{"claude-agent-acp", "amp"} {
		if _, ok := ids[wrong]; ok {
			t.Errorf("snapshot has %q, which is not a Registry id", wrong)
		}
	}
	ag, ok := ids["antigravity-acp"]
	if !ok || DedicatedBuiltin(ag) != "antigravity" {
		t.Fatalf("antigravity-acp: %+v", ag)
	}
}

// Plain http is only for this computer's own loopback (a test's or a local
// mirror's registry); anywhere else a binary comes over https (#147).
func TestSecureDownload(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://example.com/a.zip":      true,
		"http://127.0.0.1:8080/a.zip":    true,
		"http://[::1]:8080/a.zip":        true,
		"http://localhost/a.zip":         true,
		"http://example.com/a.zip":       false,
		"http://192.0.2.10/a.zip":        false,
		"http://127.0.0.1.example.com/a": false,
		"ftp://127.0.0.1/a.zip":          false,
		"file:///tmp/a.zip":              false,
	} {
		u, _ := url.Parse(raw)
		if got := secureDownload(u); got != want {
			t.Errorf("secureDownload(%s) = %v, want %v", raw, got, want)
		}
	}
}
