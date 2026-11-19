package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Spool writes each notification to a file in Dir as JSON ({"service",
// "token", "environment", "payload", "urgent"}), for tests.
type Spool struct{ Dir string }

var spoolSafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

func (s Spool) Push(_ context.Context, p Push) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"service": p.Service, "token": p.Token, "environment": p.Environment, "payload": json.RawMessage(p.Payload), "urgent": p.Urgent})
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), spoolSafe.ReplaceAllString(p.Thread, ""))
	tmp := filepath.Join(s.Dir, "."+name+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, name))
}
