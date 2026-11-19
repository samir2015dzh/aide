package passlogin

import (
	"testing"

	"github.com/hermeshai/hermesh/pkg/identity"
)

func TestSealOpen(t *testing.T) {
	key, _ := identity.GenerateKey()
	l, err := Seal("o_owner", " Samir ", "correct horse battery", key)
	if err != nil {
		t.Fatal(err)
	}
	if l.Account != "samir" {
		t.Fatalf("account kept as %q", l.Account)
	}
	authKey, sealKey, err := Derive("correct horse battery", l.Kdf)
	if err != nil {
		t.Fatal(err)
	}
	if !Check(l.Verifier, authKey) || Check(l.Verifier, sealKey) {
		t.Fatal("the verifier checks the auth key, and only it")
	}
	got, err := Open("o_owner", l, sealKey)
	if err != nil || !got.Key.Equal(key.Key) {
		t.Fatalf("open: %v", err)
	}
	// Another owner's name, or another password, does not open it.
	if _, err := Open("o_other", l, sealKey); err == nil {
		t.Fatal("opened as another owner's")
	}
	_, wrong, _ := Derive("correct horse batterz", l.Kdf)
	if _, err := Open("o_owner", l, wrong); err == nil {
		t.Fatal("opened with another password")
	}
	// A swapped public key is refused.
	other, _ := identity.GenerateKey()
	l.PublicKey = identity.MarshalPublicKey(other.Public())
	if _, err := Open("o_owner", l, sealKey); err == nil {
		t.Fatal("opened under another public key")
	}
}

// A relay can't make a client spend unbounded memory or time.
func TestDeriveBounds(t *testing.T) {
	k := NewKDF()
	k.MemoryKib = 4 << 20
	if _, _, err := Derive("x", k); err == nil {
		t.Fatal("4 GiB taken")
	}
	k = NewKDF()
	k.Salt = nil
	if _, _, err := Derive("x", k); err == nil {
		t.Fatal("no salt taken")
	}
}

func TestCheckNew(t *testing.T) {
	for _, c := range []struct{ account, password string }{{"", "long enough"}, {"a b", "long enough"}, {"samir", "short"}, {"samirsamir", "SamirSamir"}} {
		if CheckNew(c.account, c.password) == nil {
			t.Errorf("%q / %q accepted", c.account, c.password)
		}
	}
	if err := CheckNew("Samir", "correct horse"); err != nil {
		t.Error(err)
	}
}
