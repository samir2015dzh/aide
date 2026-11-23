package owner

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	localv1 "github.com/samir2015dzh/aide/gen/go/aide/local/v1"
)

// A watch with nothing new to say is still sent something every watchBeat
// (工单 #87): an empty message, by which the app tells a quiet watch from
// one that hangs.
func TestWatchBeats(t *testing.T) {
	defer func(b time.Duration) { watchBeat = b }(watchBeat)
	watchBeat = 50 * time.Millisecond
	d, err := Open(Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	l := &localService{d: d}
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), viewerKey{}, true), 10*time.Second)
	defer cancel()
	got := make(chan *localv1.WatchResponse, 16)
	go l.watchRound(ctx, func(m *localv1.WatchResponse) error {
		got <- m
		return nil
	})
	first := <-got
	if !first.Reset_ {
		t.Fatalf("first message = %v, want everything (reset)", first)
	}
	select {
	case m := <-got:
		if !proto.Equal(m, &localv1.WatchResponse{}) {
			t.Fatalf("beat = %v, want an empty message", m)
		}
	case <-ctx.Done():
		t.Fatal("no beat on a quiet watch")
	}
}
