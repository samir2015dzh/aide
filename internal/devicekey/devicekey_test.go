package devicekey

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/hermeshai/hermesh/pkg/identity"
)

func TestSealedKey(t *testing.T) {
	dir := t.TempDir()
	k, err := Create(dir, "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	id := identity.KeyID(k.Public())
	b, _ := os.ReadFile(Path(dir, id))
	if fi, _ := os.Stat(Path(dir, id)); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v", fi.Mode())
	}
	if strings.Contains(string(b), "PRIVATE KEY") {
		t.Fatal("the key is in the clear")
	}
	if _, err := Open(dir, id, "wrong"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	got, err := Open(dir, id, "correct horse")
	if err != nil || !got.Key.Equal(k.Key) {
		t.Fatalf("open: %v", err)
	}
	if _, err := Open(dir, "nope", "correct horse"); err == nil {
		t.Fatal("opened a key that isn't there")
	}
	if err := Delete(dir, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir, id)); !os.IsNotExist(err) {
		t.Fatal("the key is still there")
	}
}

// TestSealed: any secret seals as a key does, bound to its public key.
func TestSealed(t *testing.T) {
	path := t.TempDir() + "/pin"
	if err := Seal(path, "correct horse", []byte("1234"), []byte("pub")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Unseal(path, "wrong"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("a wrong passphrase: %v", err)
	}
	secret, pub, err := Unseal(path, "correct horse")
	if err != nil || string(secret) != "1234" || string(pub) != "pub" {
		t.Fatalf("unseal: %q %q %v", secret, pub, err)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "1234") {
		t.Fatal("the secret is in the clear")
	}
	// Bound to its public key: another one doesn't open it.
	os.WriteFile(path, []byte(strings.Replace(string(b), `"cHVi"`, `"cHVj"`, 1)), 0o600)
	if _, _, err := Unseal(path, "correct horse"); !errors.Is(err, ErrPassphrase) {
		t.Fatalf("another public key: %v", err)
	}
}
