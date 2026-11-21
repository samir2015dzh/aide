package claude

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Chromium's OSCrypt seals a value as "v10" followed by a 12-byte AES-GCM
// nonce, the ciphertext, and its 16-byte tag, under a 256-bit key the
// browser (here the Claude desktop app, built on Electron/Chromium) keeps
// in its Local State. Only the key's retrieval is platform-specific (on
// Windows DPAPI, see oscrypt_windows.go); sealing and opening a blob given
// the key is the same everywhere, so it is here and runs — and is tested —
// on any OS.

const oscryptPrefix = "v10"

const (
	oscryptNonceLen = 12
	oscryptTagLen   = 16
	oscryptKeyLen   = 32
)

// errUnsupportedOS is returned where the OS has no way to open the Claude
// desktop app's token cache (the key store is Windows-only so far).
var errUnsupportedOS = errors.New("reading the Claude desktop token cache is not supported on this operating system")

// oscryptOpen decrypts a v10 blob with a 32-byte key.
func oscryptOpen(key, blob []byte) ([]byte, error) {
	if len(key) != oscryptKeyLen {
		return nil, fmt.Errorf("os_crypt key is %d bytes, want %d", len(key), oscryptKeyLen)
	}
	if len(blob) < len(oscryptPrefix)+oscryptNonceLen+oscryptTagLen {
		return nil, errors.New("os_crypt blob too short")
	}
	if string(blob[:len(oscryptPrefix)]) != oscryptPrefix {
		return nil, fmt.Errorf("os_crypt blob is not %q (DPAPI-only or another scheme)", oscryptPrefix)
	}
	body := blob[len(oscryptPrefix):]
	nonce := body[:oscryptNonceLen]
	ct := body[oscryptNonceLen:]
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("os_crypt open: %w", err)
	}
	return plain, nil
}

// oscryptSeal is oscryptOpen's inverse: it seals plain into a fresh v10
// blob, as the desktop app would, so a rewritten cache opens the same way.
func oscryptSeal(key, plain []byte) ([]byte, error) {
	if len(key) != oscryptKeyLen {
		return nil, fmt.Errorf("os_crypt key is %d bytes, want %d", len(key), oscryptKeyLen)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, oscryptNonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte(oscryptPrefix), nonce...)
	return gcm.Seal(out, nonce, plain, nil), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithNonceSize(block, oscryptNonceLen)
}

// localStateEncryptedKey reads os_crypt.encrypted_key from a Local State
// file and returns its raw bytes with the 5-byte "DPAPI" marker stripped —
// the part that still needs the platform's user key store to unwrap. The
// JSON read and base64 decode are portable; unwrapping is dpapiUnprotect
// (oscrypt_windows.go), so this split keeps everything but the final
// unwrap testable off Windows.
func localStateEncryptedKey(localStatePath string) ([]byte, error) {
	b, err := os.ReadFile(localStatePath)
	if err != nil {
		return nil, err
	}
	var ls struct {
		OSCrypt struct {
			EncryptedKey string `json:"encrypted_key"`
		} `json:"os_crypt"`
	}
	if err := json.Unmarshal(b, &ls); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", localStatePath, err)
	}
	if ls.OSCrypt.EncryptedKey == "" {
		return nil, fmt.Errorf("%s has no os_crypt.encrypted_key", localStatePath)
	}
	raw, err := base64.StdEncoding.DecodeString(ls.OSCrypt.EncryptedKey)
	if err != nil {
		return nil, fmt.Errorf("decoding os_crypt.encrypted_key: %w", err)
	}
	const marker = "DPAPI"
	if len(raw) < len(marker) || string(raw[:len(marker)]) != marker {
		return nil, errors.New("os_crypt.encrypted_key is not DPAPI-wrapped")
	}
	return raw[len(marker):], nil
}
