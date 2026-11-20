// Package hermeshcore is the owner role bound for the Android app with
// gomobile (apps/android/scripts/build-core.sh: gomobile bind
// -target=android): internal/mobile, the core the iPhone app had too (its C
// library went with it, removed with the fork's iPhone app).
// Everything else goes through LocalService on the loopback port Start
// returns. The phone logs in with the owner's account and password; the
// server signs for it, so the phone holds no root key and asks nothing.
package hermeshcore

import (
	"sync"

	"github.com/hermeshai/hermesh/internal/mobile"
)

// Info is where the core serves LocalService: 127.0.0.1:Port, to callers
// that send "Authorization: Bearer <Token>".
type Info struct {
	Port  int
	Token string
}

var (
	mu   sync.Mutex
	core *mobile.Core
)

// Start starts the owner role with its data in dataDir (once per process;
// later calls return the same port and token).
func Start(dataDir string) (*Info, error) {
	mu.Lock()
	defer mu.Unlock()
	if core == nil {
		c, err := mobile.Start(dataDir, mobile.Logger(dataDir))
		if err != nil {
			return nil, err
		}
		core = c
	}
	return &Info{Port: core.Port(), Token: core.Token()}, nil
}

// Resume keeps the relay connection up (the app came to the front).
func Resume() {
	if c := started(); c != nil {
		c.Resume()
	}
}

// Pause stops following the machines, keeping the relay connection to try
// again first (the app went to the background).
func Pause() {
	if c := started(); c != nil {
		c.Pause()
	}
}

// NetworkChanged connects again over the network now in use.
func NetworkChanged() {
	if c := started(); c != nil {
		c.Reconnect()
	}
}

// PathChanged connects again when the device's addresses are not those
// last seen.
func PathChanged() {
	if c := started(); c != nil {
		c.PathChanged()
	}
}

func started() *mobile.Core {
	mu.Lock()
	defer mu.Unlock()
	return core
}
