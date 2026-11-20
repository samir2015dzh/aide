package machine

import (
	"github.com/hermeshai/hermesh/internal/legacy"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sealSecret seals a custom agent's secret with DPAPI for this Windows
// user (CryptProtectData): only processes of this user on this PC open it,
// and a copy of the file taken elsewhere opens nowhere.
func sealSecret(b []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptProtectData(blob(b), nil, blob(secretEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}

// openSecret opens what sealSecret sealed.
func openSecret(b []byte) ([]byte, error) {
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(blob(b), nil, blob(secretEntropy), 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	return takeBlob(out), nil
}

// secretStore says where the values are kept, for the log and the docs.
const secretStore = "Windows DPAPI"

// secretEntropy binds the sealed values to this use: another program of
// the same user calling CryptUnprotectData without it opens nothing.
var secretEntropy = []byte(legacy.AgentSecretEntropy)

func blob(b []byte) *windows.DataBlob {
	if len(b) == 0 {
		return &windows.DataBlob{}
	}
	return &windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
}

func takeBlob(b windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data)))
	return append([]byte(nil), unsafe.Slice(b.Data, b.Size)...)
}
