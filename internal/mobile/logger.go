package mobile

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Logger writes to <dataDir>/logs/core.log (kept under 5 MB) and stderr:
// the iPhone's core log and the Android phone's alike.
func Logger(dataDir string) *slog.Logger {
	var out io.Writer = os.Stderr
	dir := filepath.Join(dataDir, "logs")
	os.MkdirAll(dir, 0o700)
	path := filepath.Join(dir, "core.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 5<<20 {
		os.Rename(path, path+".1")
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		out = io.MultiWriter(os.Stderr, f)
	}
	return slog.New(slog.NewTextHandler(out, nil))
}
