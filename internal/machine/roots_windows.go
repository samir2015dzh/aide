package machine

import (
	"strings"

	"golang.org/x/sys/windows"
)

// GetFinalPathNameByHandle's flags: names as the disk has them, behind a
// drive letter.
const fileNameNormalized, volumeNameDOS = 0x0, 0x0

// realPath is the path Windows opens for path: links and junctions
// followed (filepath.EvalSymlinks leaves junctions as they are), short
// names long, each name as the disk has it.
func realPath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	// FILE_FLAG_BACKUP_SEMANTICS opens a folder.
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), fileNameNormalized|volumeNameDOS)
	if err != nil {
		return "", err
	}
	real := windows.UTF16ToString(buf[:n])
	if rest, ok := strings.CutPrefix(real, `\\?\UNC\`); ok {
		return `\\` + rest, nil
	}
	return strings.TrimPrefix(real, `\\?\`), nil
}
