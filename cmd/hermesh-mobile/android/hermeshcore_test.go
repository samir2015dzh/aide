package hermeshcore

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"

	localv1 "github.com/hermeshai/hermesh/gen/go/hermesh/local/v1"
	"github.com/hermeshai/hermesh/gen/go/hermesh/local/v1/localv1connect"
)

type bearer struct{ token string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return http.DefaultTransport.RoundTrip(r)
}

func TestStartServesLocalService(t *testing.T) {
	// The core lives as long as the process, as the app's does, its log
	// open: Windows can't remove its folder before the process ends, so
	// not t.TempDir, whose cleanup would fail the test there.
	dir, err := os.MkdirTemp("", "hermeshcore")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	info, err := Start(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Start(t.TempDir())
	if again.Port != info.Port || again.Token != info.Token {
		t.Fatal("a second Start started another core")
	}
	hc := &http.Client{Transport: bearer{token: info.Token}}
	c := localv1connect.NewLocalServiceClient(hc, fmt.Sprintf("http://127.0.0.1:%d", info.Port))
	st, err := c.GetStatus(context.Background(), &localv1.GetStatusRequest{})
	if err != nil || st.Initialized {
		t.Fatalf("status: %v %v", st, err)
	}
	Pause()
	Resume()
	NetworkChanged()
	PathChanged()
	Pause()
}
