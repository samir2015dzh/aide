package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hermeshai/hermesh/internal/signer/unlock"
)

// unlockCmd switches how a root key of the user sid is unlocked
// (internal/signer/unlock), elevated, as the app runs it from Settings
// (one UAC prompt): to a passphrase the owner chooses in the signer's own
// window, once Windows Hello confirms it; or back to Windows Hello, with
// the passphrase. Without --to, as the user, it prints how the key is
// unlocked now, for the app to show.
func unlockCmd(args []string) error {
	fs := flag.NewFlagSet("unlock", flag.ContinueOnError)
	user := fs.String("user", "", "the SID of the user whose root key it is")
	keyID := fs.String("key", "", "the root key's id")
	to := fs.String("to", "", "hello or passphrase")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyID == "" {
		return errors.New("--key: which root key")
	}
	if *to == "" {
		sid, err := userSID()
		if err != nil {
			return err
		}
		fmt.Println(shownMode(sid, *keyID))
		return nil
	}
	mode := unlock.Mode(*to)
	if mode != unlock.Hello && mode != unlock.Passphrase {
		return fmt.Errorf("--to: hello or passphrase, not %q", *to)
	}
	if err := refusals(*user); err != nil {
		return err
	}
	keys := &unlock.Keys{Vault: tpm{}, Dir: keysDir(*user)}
	now, err := keys.Mode(*keyID)
	if err != nil {
		return err
	}
	if now != mode {
		p := newDrawnPanel()
		p.keys = keys
		var ok bool
		onOwnThread(func() {
			if mode == unlock.Passphrase {
				ok, err = p.usePassphrase(*keyID)
			} else {
				ok, err = p.useHello(*keyID)
			}
		})
		switch {
		case errors.Is(err, errNoHello):
			return errHelloNeeded
		case err != nil:
			return err
		case !ok:
			return errUnchanged
		}
	}
	return showMode(*user, *keyID, mode)
}

// modesDir tells the user sid's programs how each of their root keys is
// unlocked (they can't read keysDir): only administrators write it, so
// what it says is what this elevated program wrote.
func modesDir(sid string) string {
	return filepath.Join(os.Getenv("ProgramData"), "Hermesh Signer", "unlock", sid)
}

func showMode(sid, keyID string, mode unlock.Mode) error {
	dir := modesDir(sid)
	if err := privateDir(dir, sid); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, keyID), []byte(mode), 0o644)
}

// shownMode is how the root key keyID is unlocked, as showMode last wrote
// it: Windows Hello unless it was switched.
func shownMode(sid, keyID string) unlock.Mode {
	b, err := os.ReadFile(filepath.Join(modesDir(sid), keyID))
	if err != nil || unlock.Mode(b) != unlock.Passphrase {
		return unlock.Hello
	}
	return unlock.Passphrase
}
