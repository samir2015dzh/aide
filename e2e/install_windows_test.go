package e2e

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestWindowsInstall runs install.ps1 as a person would, against a release
// signed by a key of the test's own: it installs the agents' CLI, installs
// it again over itself, and refuses a checksum list another key signed. A
// machine's install over one from a version with Her removes her and her
// console.
func TestWindowsInstall(t *testing.T) {
	bin := binaries(t)
	rel := t.TempDir()
	prog, err := os.ReadFile(filepath.Join(bin, "aide.exe"))
	if err != nil {
		t.Fatal(err)
	}
	// Every program is the CLI: a machine's install ends by pairing, which
	// the CLI refuses at once, leaving what was installed to look at.
	var list []byte
	add := func(asset string, b []byte) {
		os.WriteFile(filepath.Join(rel, asset), b, 0o644)
		sum := sha256.Sum256(b)
		list = append(list, hex.EncodeToString(sum[:])+"  "+asset+"\n"...)
	}
	for _, name := range []string{"aide", "aided", "aidedw"} {
		add(name+"-windows-"+runtime.GOARCH+".exe", prog)
	}
	os.WriteFile(filepath.Join(rel, "checksums.txt"), list, 0o644)
	sign := func(k *ecdsa.PrivateKey) {
		h := sha256.Sum256(list)
		sig, err := ecdsa.SignASN1(rand.Reader, k, h[:])
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(rel, "checksums.txt.sig"), sig, 0o644)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	sign(key)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	src, err := os.ReadFile("scripts/install.ps1")
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "install.ps1")
	pub := strings.TrimSpace(string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})))
	os.WriteFile(script, []byte(strings.Replace(string(src), "__AIDE_RELEASE_PUBKEY__", pub, 1)), 0o644)
	srv := httptest.NewServer(http.FileServer(http.Dir(rel)))
	defer srv.Close()
	install := func(dir string, args ...string) result {
		return run(t, []string{"AIDE_DOWNLOAD_BASE=" + srv.URL, "AIDE_BIN_DIR=" + dir, "AIDE_NO_PATH=1"}, 3*time.Minute,
			"powershell", append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script}, args...)...)
	}

	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		if r := install(dir, "-Cli"); r.code != 0 {
			t.Fatalf("install %d: %+v", i+1, r)
		}
		if r := run(t, nil, time.Minute, filepath.Join(dir, "aide.exe"), "version"); r.code != 0 {
			t.Fatalf("installed CLI: %+v", r)
		}
	}

	// A machine, over one from a version with Her: aided, and nothing of
	// hers left beside it, twice.
	dir = t.TempDir()
	os.WriteFile(filepath.Join(dir, "her.exe"), prog, 0o755)
	os.MkdirAll(filepath.Join(dir, "her-console"), 0o755)
	os.WriteFile(filepath.Join(dir, "her-console", "index.html"), []byte("<title>Her</title>"), 0o644)
	for i := 0; i < 2; i++ {
		r := install(dir, "-Name", "test-pc")
		for _, f := range []string{"aided.exe", "aidedw.exe"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
				t.Fatalf("install %d of a machine: no %s: %+v", i+1, f, r)
			}
		}
		for _, f := range []string{"her.exe", "her-console"} {
			if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
				t.Fatalf("install %d of a machine: %s left: %+v", i+1, f, r)
			}
		}
	}

	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	sign(other)
	dir = t.TempDir()
	if r := install(dir, "-Cli"); r.code == 0 || !strings.Contains(r.stdout+r.stderr, "verification FAILED") {
		t.Fatalf("installed from a list another key signed: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "aide.exe")); err == nil {
		t.Fatal("the CLI is there all the same")
	}
}
