//go:build !windows

package claude

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestListModelsNeverHangs: a Claude Code that does not answer (stuck
// starting, a child holding its output) is stopped with everything it
// started once the wait is over; the check goes on without its models.
func TestListModelsNeverHangs(t *testing.T) {
	scripted(t)
	defer func(d time.Duration) { modelsWait = d }(modelsWait)
	modelsWait = time.Second
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child")
	bin := filepath.Join(dir, "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\nsleep 600 &\necho $! > "+pidFile+"\nwait\n"), 0o755)
	start := time.Now()
	if m, own := listModels(context.Background(), os.Environ(), bin); m != nil || own != "" {
		t.Fatalf("got %+v %q", m, own)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("took %v", d)
	}
	b, _ := os.ReadFile(pidFile)
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	deadline := time.Now().Add(5 * time.Second)
	for pid > 0 && syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("its child %d outlived it", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
