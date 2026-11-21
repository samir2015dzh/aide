package claude

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// dpapiUnprotect unwraps the Local State os_crypt key (its "DPAPI" marker
// already stripped) with CryptUnprotectData for this Windows user: it opens
// only in this user's processes on this PC, so a copy of the file taken
// elsewhere yields nothing.
func dpapiUnprotect(b []byte) ([]byte, error) {
	in := windows.DataBlob{}
	if len(b) > 0 {
		in = windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// desktopKeyStoreAvailable: this OS can unwrap the token cache key.
const desktopKeyStoreAvailable = true
