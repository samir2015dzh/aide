// Command aide-relay runs the self-hosted Aide relay.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/crypto/acme/autocert"

	"github.com/samir2015dzh/aide/internal/brandenv"
	"github.com/samir2015dzh/aide/internal/sqlitedb"
	"github.com/samir2015dzh/aide/pkg/identity"
	"github.com/samir2015dzh/aide/pkg/mcpgw"
	"github.com/samir2015dzh/aide/pkg/metrics"
	"github.com/samir2015dzh/aide/pkg/relay"
	"github.com/samir2015dzh/aide/pkg/rlimit"
)

func main() {
	root := &cobra.Command{Use: "aide-relay", Short: "Aide server: the owner's account, and where devices, machines and agents meet", SilenceUsage: true}
	var dataDir, listen, domain, certFile, keyFile, debugAddr, publicURL string
	var withMCP bool
	var pushSpool string
	serve := &cobra.Command{
		Use:   "serve",
		Short: "Run the relay",
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			if n := rlimit.RaiseNoFile(); n > 0 { // Windows has no such limit
				log.Info("open file limit", "soft", n)
			}
			if debugAddr != "" {
				go func() { log.Warn("debug server stopped", "err", metrics.Serve(debugAddr)) }()
			}
			var pusher relay.Pusher
			var err error
			if pushSpool != "" {
				pusher = relay.Spool{Dir: pushSpool}
			}
			var mcpURL string
			if withMCP {
				public := strings.TrimSuffix(firstNonEmpty(publicURL, defaultPublicURL(domain, certFile, listen)), "/")
				if mcpURL, err = mcpAddress(dataDir, public, false); err != nil {
					return err
				}
			}
			srv, err := relay.New(relay.Config{DataDir: dataDir, Logger: log, Pusher: pusher, MCPURL: mcpURL})
			if err != nil {
				return err
			}
			defer srv.Close()
			// Without a domain or an HTTPS proxy: say what that leaves open.
			if !strings.HasPrefix(firstNonEmpty(publicURL, defaultPublicURL(domain, certFile, listen)), "https://") {
				fmt.Fprint(os.Stderr, "\n  Plain HTTP, not encrypted on the way. Sessions stay end-to-end encrypted,\n"+
					"  but the setup code, the account's password as it logs in, and which machines\n"+
					"  are online can be seen on the network.\n"+
					"  For HTTPS, give it a domain (AIDE_RELAY_DOMAIN, --domain), or put an HTTPS\n"+
					"  proxy in front and set AIDE_RELAY_PUBLIC_URL (--public-url).\n")
				if withMCP {
					fmt.Fprint(os.Stderr, "  Claude, ChatGPT and other MCP apps need HTTPS: they won't connect.\n")
				}
			}
			if code, err := srv.SetupCode(); err != nil {
				return err
			} else if code != "" {
				fmt.Fprintf(os.Stderr, "\n  This server is not set up yet.\n  Setup code: %s\n  In the Aide app, choose Set Up a New Server, and type it with the account\n  and password the server will keep.\n\n", code)
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			handler := srv.Handler()
			if withMCP {
				gw, err := startMCP(srv, log, dataDir, mcpURL)
				if err != nil {
					return err
				}
				defer gw.Close()
				mux := http.NewServeMux()
				srv.Mount(mux)
				gw.Mount(mux)
				// An address without the key, or with a wrong one.
				noKey := func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "This server's MCP address has a key in it. Copy it in Aide: Settings › Connect › Assistants. Or run aide-relay mcp-url on the server.", http.StatusNotFound)
				}
				mux.HandleFunc("/mcp", noKey)
				mux.HandleFunc("/mcp/", noKey)
				handler = mux
			}
			hs := &http.Server{Handler: handler, ReadHeaderTimeout: 15 * time.Second}
			go func() {
				<-ctx.Done()
				sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				hs.Shutdown(sctx)
			}()
			// Each case binds its address before it says it listens: tests and
			// scripts wait for that line.
			switch {
			case domain != "":
				m := &autocert.Manager{Prompt: autocert.AcceptTOS, HostPolicy: autocert.HostWhitelist(domain),
					Cache: autocert.DirCache(filepath.Join(dataDir, "acme"))}
				go http.ListenAndServe(":80", m.HTTPHandler(nil))
				hs.Addr = ":443"
				hs.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate, MinVersion: tls.VersionTLS12}
				ln, lerr := net.Listen("tcp", hs.Addr)
				if lerr != nil {
					return lerr
				}
				log.Info("relay listening", "addr", hs.Addr, "domain", domain, "relay", srv.RelayID())
				err = hs.ServeTLS(ln, "", "")
			case certFile != "":
				hs.Addr = listen
				ln, lerr := net.Listen("tcp", listen)
				if lerr != nil {
					return lerr
				}
				log.Info("relay listening (tls)", "addr", listen, "relay", srv.RelayID())
				err = hs.ServeTLS(ln, certFile, keyFile)
			default:
				hs.Addr = listen
				ln, lerr := net.Listen("tcp", listen)
				if lerr != nil {
					return lerr
				}
				log.Info("relay listening (plain HTTP)", "addr", listen, "relay", srv.RelayID())
				err = hs.Serve(ln)
			}
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		},
	}
	serve.Flags().StringVar(&dataDir, "data-dir", envOr("RELAY_DATA", "./relay-data"), "data directory")
	serve.Flags().StringVar(&listen, "listen", ":8080", "listen address (plain HTTP or with --tls-cert)")
	serve.Flags().StringVar(&domain, "domain", brandenv.Get("RELAY_DOMAIN"), "serve HTTPS on :443 with an automatic Let's Encrypt certificate for this domain (or AIDE_RELAY_DOMAIN)")
	serve.Flags().StringVar(&certFile, "tls-cert", "", "TLS certificate file")
	serve.Flags().StringVar(&keyFile, "tls-key", "", "TLS key file")
	serve.Flags().StringVar(&debugAddr, "debug-addr", "", "serve /metrics and /debug/pprof on this loopback address")
	serve.Flags().BoolVar(&withMCP, "mcp", envBool("RELAY_MCP"), "also serve MCP at <public URL>/mcp/<key> for remote agent products (Claude, ChatGPT, Codex, ...); this server then holds the keys of the MCP connections made with your account (or AIDE_RELAY_MCP=1)")
	serve.Flags().StringVar(&publicURL, "public-url", brandenv.Get("RELAY_PUBLIC_URL"), "the URL people reach this relay at, behind a proxy that serves HTTPS (default: from --domain, else the listen address; or AIDE_RELAY_PUBLIC_URL)")
	serve.Flags().StringVar(&pushSpool, "push-spool", "", "write notifications to files in this directory instead of sending them (tests)")
	serve.Flags().MarkHidden("push-spool")

	setupCode := &cobra.Command{
		Use:     "setup-code",
		Aliases: []string{"claim-code"},
		Short:   "Print the setup code of a server not set up yet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			srv, err := relay.New(relay.Config{DataDir: dataDir, Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))})
			if err != nil {
				return err
			}
			defer srv.Close()
			code, err := srv.SetupCode()
			if err != nil {
				return err
			}
			if code == "" {
				fmt.Println("already set up")
				return nil
			}
			fmt.Println(code)
			return nil
		},
	}
	setupCode.Flags().StringVar(&dataDir, "data-dir", envOr("RELAY_DATA", "./relay-data"), "data directory")

	var account string
	password := &cobra.Command{
		Use:   "password",
		Short: "Set the account's password here on the server, for when it is forgotten: every device and agent that logged in is signed out",
		Long: `Set the account's password on the server's own computer, for when it
is forgotten. It reads the new password from stdin. Every device and agent
that logged in is signed out (machines stay); log in again with the new
password. Restart the server afterwards, so that it signs them out at once.`,
		Example: `  docker exec -i aide-relay aide-relay password --account me < new-password.txt`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			l, err := bufio.NewReader(os.Stdin).ReadString('\n')
			if err != nil && l == "" {
				return fmt.Errorf("reading the new password from stdin: %w", err)
			}
			srv, err := relay.New(relay.Config{DataDir: dataDir, Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))})
			if err != nil {
				return err
			}
			defer srv.Close()
			n, err := srv.SetPassword(account, strings.TrimRight(l, "\r\n"))
			if err != nil {
				return err
			}
			fmt.Printf("The password is set; %d devices and agents were signed out. Restart the server.\n", n)
			return nil
		},
	}
	password.Flags().StringVar(&dataDir, "data-dir", envOr("RELAY_DATA", "./relay-data"), "data directory")
	password.Flags().StringVar(&account, "account", "", "the account (it may be renamed here)")
	password.MarkFlagRequired("account")

	var renew bool
	mcpCmd := &cobra.Command{
		Use:   "mcp-url",
		Short: "Print the MCP address of a relay serving MCP (serve --mcp), with its key",
		RunE: func(cmd *cobra.Command, _ []string) error {
			b, err := os.ReadFile(filepath.Join(dataDir, "mcp-url"))
			if errors.Is(err, os.ErrNotExist) {
				return errors.New("this relay serves no MCP yet: start it with serve --mcp")
			} else if err != nil {
				return err
			}
			addr := strings.TrimSpace(string(b))
			if renew {
				if addr, err = mcpAddress(dataDir, addr[:strings.LastIndex(addr, "/mcp/")], true); err != nil {
					return err
				}
				fmt.Fprintln(os.Stderr, "A new key: restart the relay to use it. Apps connected with the old address must connect again with this one.")
			}
			fmt.Println(addr)
			return nil
		},
	}
	mcpCmd.Flags().StringVar(&dataDir, "data-dir", envOr("RELAY_DATA", "./relay-data"), "data directory")
	mcpCmd.Flags().BoolVar(&renew, "new", false, "make a new key, for when the address got out")
	root.AddCommand(serve, setupCode, password, mcpCmd)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// mcpAddress is the relay's MCP address: <public URL>/mcp/<key>. The key
// keeps anyone without the address from connecting (docs/mcp-gateway.md
// §13); it is kept in <data-dir>/mcp-url, made on first use, and made anew
// with renew.
func mcpAddress(dataDir, public string, renew bool) (string, error) {
	p := filepath.Join(dataDir, "mcp-url")
	key := ""
	if b, err := os.ReadFile(p); err == nil {
		old := strings.TrimSpace(string(b))
		if i := strings.LastIndex(old, "/mcp/"); i >= 0 {
			key = old[i+len("/mcp/"):]
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if key == "" || renew {
		key = strings.ToLower(identity.RandomCode(26))
	}
	addr := public + "/mcp/" + key
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	return addr, os.WriteFile(p, []byte(addr+"\n"), 0o600)
}

// startMCP starts the MCP gateway of a self-hosted relay at mcpURL: its
// state in mcp.db and the key that seals the connections' keys in
// mcp-vault.key.
func startMCP(srv *relay.Server, log *slog.Logger, dataDir, mcpURL string) (*mcpgw.Gateway, error) {
	db, err := sqlitedb.Open(filepath.Join(dataDir, "mcp.db"), []string{mcpgw.Schema(false)})
	if err != nil {
		return nil, err
	}
	vault, err := mcpgw.KeyFileVault(filepath.Join(dataDir, "mcp-vault.key"), true)
	if err != nil {
		return nil, err
	}
	publicURL := mcpURL[:strings.LastIndex(mcpURL, "/mcp/")]
	var webhooks *http.Client
	if brandenv.Get("MCP_LOCAL_WEBHOOKS") == "1" { // tests: webhooks on this computer
		webhooks = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	gw, err := mcpgw.New(mcpgw.Config{Issuer: publicURL, ResourceURL: mcpURL, KeyedResource: true, Relay: srv, Backend: &mcpgw.SQLBackend{DB: db},
		Vault: vault, Accounts: mcpgw.SingleOwner{Relay: srv}, Logger: log, WebhookClient: webhooks})
	if err != nil {
		return nil, err
	}
	log.Info("mcp gateway", "url", publicURL+"/mcp/…", "address", "aide-relay mcp-url prints it")
	return gw, nil
}

// defaultPublicURL is the relay's URL as its flags give it.
func defaultPublicURL(domain, certFile, listen string) string {
	if domain != "" {
		return "https://" + domain
	}
	scheme := "http"
	if certFile != "" {
		scheme = "https"
	}
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return scheme + "://" + listen
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// envBool: an environment variable set to 1 or true, as a flag's default
// (a container is configured with -e).
// envBool and envOr read AIDE_<k>, else AIDE_<k> (brandenv).
func envBool(k string) bool {
	v, _ := strconv.ParseBool(brandenv.Get(k))
	return v
}

func envOr(k, d string) string {
	if v := brandenv.Get(k); v != "" {
		return v
	}
	return d
}
