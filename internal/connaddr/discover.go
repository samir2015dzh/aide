package connaddr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samir2015dzh/aide/internal/brandenv"
)

// ErrNoServer is what pairing or logging in says when no one typed where:
// there is no default server, no build names one, and no official center
// stands in. The person gives the address of their own server.
var ErrNoServer = errors.New("which server? give your server's address (relay.example.com) or a connection address (relay.example.com/o_…), or set $AIDE_SERVER")

// Center picks the server URL or connection address to pair with: the
// argument, else $AIDE_CENTER, else $AIDE_SERVER (as Server reads it).
// "" when none is given: the caller asks for one (ErrNoServer). Nothing
// falls back to a hosted center.
func Center(arg string) string {
	if arg = strings.TrimSpace(arg); arg != "" {
		return arg
	}
	if env := strings.TrimSpace(brandenv.Get("CENTER")); env != "" {
		return env
	}
	return Server("")
}

// DiscoveryPath is where a hosted center says where its devices connect
// (machines, agents and signers), which can differ from the console's own
// address: {"connect_url": "https://connect.example.com"}.
const DiscoveryPath = "/.well-known/aide.json"

// Discover returns the connect URL the center at base publishes, or base
// itself when it publishes none (a self-hosted relay). The center is not
// trusted with this any more than with routing: verification codes and the
// pinned owner id still decide who is trusted.
func Discover(ctx context.Context, hc *http.Client, base string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(base, "/")+DiscoveryPath, nil)
	if err != nil {
		return "", err
	}
	res, err := hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach %s: %w", base, err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return base, nil
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s%s: %s", base, DiscoveryPath, res.Status)
	}
	var d struct {
		ConnectURL string `json:"connect_url"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&d); err != nil {
		return "", fmt.Errorf("%s%s: %w", base, DiscoveryPath, err)
	}
	if d.ConnectURL == "" {
		return base, nil
	}
	u, err := url.Parse(d.ConnectURL)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" ||
		!(u.Scheme == "https" || u.Scheme == "http" && strings.HasPrefix(base, "http://")) {
		return "", fmt.Errorf("%s%s: bad connect_url %q", base, DiscoveryPath, d.ConnectURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// Server picks the server to log in to with a password: the argument (a
// server's address, or a connection address, whose owner part is not
// needed), else $AIDE_SERVER, as a URL. "" when neither is given: no
// build names a default server (it is given to others), the person types
// theirs.
func Server(arg string) string {
	for _, s := range []string{arg, brandenv.Get("SERVER")} {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if !strings.Contains(s, "://") {
			// A server reached by its IP address has no certificate (it
			// gets them for domain names): plain HTTP, as the apps say.
			host := strings.Split(s, "/")[0]
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			if net.ParseIP(strings.Trim(host, "[]")) != nil {
				s = "http://" + s
			} else {
				s = "https://" + s
			}
		}
		if relay, _, err := Parse(s); err == nil {
			return relay
		}
		return strings.TrimSuffix(s, "/")
	}
	return ""
}
