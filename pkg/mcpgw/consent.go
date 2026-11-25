package mcpgw

import (
	"context"
	"crypto/hmac"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
)

// The consent page: who asks and what for; the owner's account and
// password, and Connect or Cancel. The right password connects the app at
// once: the server signs its grant itself (docs/accounts.md), and the
// browser goes on to the app.

// authorization returns the browser's authorization, checking that this
// browser started it.
func (g *Gateway) authorization(r *http.Request, id string) (*Authorization, string) {
	if id == "" {
		return nil, "missing"
	}
	if ck, err := r.Cookie(g.authzCookie()); err != nil || ck.Value != id {
		return nil, "browser"
	}
	a, err := g.db.GetAuthorization(r.Context(), hashSecret("authz", id))
	if err != nil || a == nil || g.cfg.Now().After(a.ExpiresAt) {
		return nil, "expired"
	}
	return a, ""
}

type consentData struct {
	T            map[string]string
	Lang         string
	ID           string
	CSRF         string
	App          string
	AppHost      string
	Verified     bool
	RedirectHost string
	LoopbackOnly bool
	Server       string
	// Error: why the account and password did not connect it.
	Error string
	// Typed: the account typed, kept when the password was wrong.
	Typed string
	// Progress: after Connect.
	Progress    bool
	State       string
	ContinueURL string
	Message     string
}

func (g *Gateway) handleConsentPage(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	a, why := g.authorization(r, id)
	if a == nil {
		g.errorPage(w, r, http.StatusBadRequest, map[string]string{
			"missing": "This page needs the link the app opened.",
			"browser": "Open the connection from the app again, in this browser.",
			"expired": "This connection request has expired. Connect from the app again.",
		}[why])
		return
	}
	acct, ok := g.cfg.Accounts.Browser(r)
	if !ok {
		http.Redirect(w, r, g.cfg.Accounts.SignInURL("/oauth/consent?id="+url.QueryEscape(id)), http.StatusFound)
		return
	}
	if a.AccountID != "" && a.AccountID != acct.ID {
		g.errorPage(w, r, http.StatusForbidden, "This connection request belongs to another account. Connect from the app again.")
		return
	}
	ctx := r.Context()
	c, err := g.client(ctx, a.ClientID)
	if err != nil || c == nil {
		g.errorPage(w, r, http.StatusBadRequest, "The app could not be identified. Connect from the app again.")
		return
	}
	d := g.pageData(r, id, c, a)
	if a.ConnectionID != "" {
		conn, err := g.db.GetConnection(ctx, a.ConnectionID)
		if err != nil || conn == nil {
			g.errorPage(w, r, http.StatusInternalServerError, "Something went wrong. Connect from the app again.")
			return
		}
		d.Progress, d.State, d.Message = true, conn.State, g.status(conn).Next
		d.ContinueURL = "/oauth/consent/continue?id=" + url.QueryEscape(id)
	}
	g.render(w, consentPage, d)
}

func (g *Gateway) pageData(r *http.Request, id string, c *Client, a *Authorization) consentData {
	lang, t := strings_(r)
	d := consentData{T: t, Lang: lang, ID: id, CSRF: g.csrfToken(id), App: c.Name, Verified: c.Verified, LoopbackOnly: c.loopbackOnly(),
		Server: g.issuer.Host}
	if u, err := url.Parse(c.ID); err == nil && c.Source == "cimd" {
		d.AppHost = u.Hostname()
	}
	if u, err := url.Parse(a.RedirectURI); err == nil {
		d.RedirectHost = u.Host
	}
	return d
}

// handleConsentAction answers Connect, Cancel and Finish later.
func (g *Gateway) handleConsentAction(w http.ResponseWriter, r *http.Request) {
	// Browsers send "null" for a form on a page with no referrer (this one):
	// the CSRF token and the browser-bound id are what protect the form.
	if o := r.Header.Get("Origin"); o != "" && o != "null" && o != g.issuer.Scheme+"://"+g.issuer.Host {
		g.errorPage(w, r, http.StatusForbidden, "This request came from another site.")
		return
	}
	if err := r.ParseForm(); err != nil {
		g.errorPage(w, r, http.StatusBadRequest, "Bad request.")
		return
	}
	id := r.PostForm.Get("id")
	a, _ := g.authorization(r, id)
	if a == nil || !hmacEqual(r.PostForm.Get("csrf"), g.csrfToken(id)) {
		g.errorPage(w, r, http.StatusBadRequest, "This connection request has expired. Connect from the app again.")
		return
	}
	acct, ok := g.cfg.Accounts.Browser(r)
	if !ok || a.AccountID != "" && a.AccountID != acct.ID {
		g.errorPage(w, r, http.StatusForbidden, "Sign in again, then connect from the app again.")
		return
	}
	ctx := r.Context()
	switch r.PostForm.Get("action") {
	case "cancel":
		g.consume(ctx, a)
		redirectWith(w, r, a.RedirectURI, url.Values{"error": {"access_denied"}, "error_description": {"the person cancelled"},
			"state": {a.State}, "iss": {g.issuer.String()}})
		return
	case "connect":
		if a.ConnectionID == "" {
			c, err := g.client(ctx, a.ClientID)
			if err != nil || c == nil {
				g.errorPage(w, r, http.StatusBadRequest, "The app could not be identified. Connect from the app again.")
				return
			}
			conn, err := g.createConnection(ctx, a, acct, c, r.UserAgent(), r.PostForm.Get("account"), r.PostForm.Get("password"))
			if err == errTooMany {
				g.errorPage(w, r, http.StatusConflict, "You have connected as many apps as Aide allows. Disconnect one in Aide, then connect again.")
				return
			}
			if why := loginRefusal(err); why != "" {
				d := g.pageData(r, id, c, a)
				d.Error, d.Typed = d.T[why], r.PostForm.Get("account")
				w.WriteHeader(http.StatusUnauthorized)
				g.render(w, consentPage, d)
				return
			}
			if err != nil {
				g.log.Error("mcp: create connection", "err", err)
				g.errorPage(w, r, http.StatusInternalServerError, "Something went wrong. Connect from the app again.")
				return
			}
			a.AccountID, a.ConnectionID = acct.ID, conn.ID
			if err := g.db.PutAuthorization(ctx, a); err != nil {
				g.errorPage(w, r, http.StatusInternalServerError, "Something went wrong. Connect from the app again.")
				return
			}
		}
	}
	http.Redirect(w, r, "/oauth/consent?id="+url.QueryEscape(id), http.StatusSeeOther)
}

// handleConsentContinue sends the browser back to the app with a code.
func (g *Gateway) handleConsentContinue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a, _ := g.authorization(r, r.URL.Query().Get("id"))
	if a == nil || a.ConnectionID == "" {
		g.errorPage(w, r, http.StatusBadRequest, "This connection request has expired. Connect from the app again.")
		return
	}
	if acct, ok := g.cfg.Accounts.Browser(r); !ok || acct.ID != a.AccountID {
		g.errorPage(w, r, http.StatusForbidden, "Sign in again, then connect from the app again.")
		return
	}
	code := randomToken(32)
	now := g.cfg.Now()
	if err := g.db.PutCode(ctx, &Code{Hash: hashSecret("code", code), ConnectionID: a.ConnectionID, ClientID: a.ClientID, RedirectURI: a.RedirectURI,
		CodeChallenge: a.CodeChallenge, Resource: a.Resource, ExpiresAt: now.Add(codeTTL)}); err != nil {
		g.errorPage(w, r, http.StatusInternalServerError, "Something went wrong. Connect from the app again.")
		return
	}
	g.consume(ctx, a)
	http.SetCookie(w, &http.Cookie{Name: g.authzCookie(), Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: g.secureCookies()})
	redirectWith(w, r, a.RedirectURI, url.Values{"code": {code}, "state": {a.State}, "iss": {g.issuer.String()}})
}

// consume ends an authorization: it gives one code, or one refusal.
func (g *Gateway) consume(ctx context.Context, a *Authorization) {
	a.ExpiresAt = g.cfg.Now().Add(-time.Second)
	g.db.PutAuthorization(ctx, a)
}

func hmacEqual(a, b string) bool { return a != "" && hmac.Equal([]byte(a), []byte(b)) }

// errorPage shows a problem in the browser; it never redirects.
func (g *Gateway) errorPage(w http.ResponseWriter, r *http.Request, status int, msg string) {
	lang, t := strings_(r)
	if zh, ok := zhErrors[msg]; ok && lang == "zh" {
		msg = zh
	}
	w.WriteHeader(status)
	g.render(w, errorPageTmpl, consentData{T: t, Lang: lang, Message: msg})
}

func (g *Gateway) render(w http.ResponseWriter, t *template.Template, d consentData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if err := t.Execute(w, d); err != nil {
		g.log.Warn("mcp: render", "err", err)
	}
}

// strings_ picks the page's language from Accept-Language.
func strings_(r *http.Request) (string, map[string]string) {
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		switch {
		case strings.HasPrefix(tag, "zh"):
			return "zh", zhText
		case strings.HasPrefix(tag, "en"):
			return "en", enText
		}
	}
	return "en", enText
}

// loginRefusal is the page's text for why the account and password did not
// connect the app, "" for any other error.
func loginRefusal(err error) string {
	switch connect.CodeOf(err) {
	case connect.CodePermissionDenied:
		return "wrong"
	case connect.CodeResourceExhausted:
		return "locked"
	case connect.CodeFailedPrecondition:
		return "off"
	}
	return ""
}

var enText = map[string]string{
	"title":         "Connect %s to Aide",
	"verified":      "Verified app",
	"unverified":    "Aide could not verify this app. Connect only if you just started this from %s.",
	"returns":       "It will return to %s.",
	"loopback":      "This app returns to a program on your computer. Any program there could claim to be this app: connect only if you just started this yourself.",
	"server":        "Server",
	"login":         "Log in with your Aide account to connect it. Once connected, it can:",
	"can1":          "See your machines, projects and tasks.",
	"can2":          "Start Claude Code or Codex tasks and talk to them. Those agents run with full permissions in your projects.",
	"custodian":     "%s holds this connection's key: it can act as the app, and sees the tasks the app reaches. Disconnect it in Aide at any time.",
	"account":       "Account",
	"password":      "Password",
	"wrong":         "Wrong account or password.",
	"locked":        "Too many tries. Wait a few minutes, then try again.",
	"off":           "Logging in is turned off for this account. Turn it on in Aide › Settings › Account.",
	"connect":       "Connect",
	"cancel":        "Cancel",
	"connected":     "Connected",
	"connectedBody": "%s is authorized for Aide. Finish both onboarding pieces before calling setup done: (1) Keepalive — keep one MCP session open with periodic ping (stateless calls cannot keep online status); (2) Inbox watching — declare and wire your platform's wake mechanism (daemon cron polling message_list or webhook via message_wake; webhook-woken routine via message_wake; or external poll hook calling message_list on unseen ids) and call message_ack (held=true for owner-held items). Done = your owner sends a real message from the Aide app and you pick it up and reply without reminders elsewhere.",
	"continue":      "Continue to %s",
	"ended":         "This connection has ended",
	"errorTitle":    "Can't connect",
}

var zhText = map[string]string{
	"title":         "将 %s 连接到 Aide",
	"verified":      "已验证的应用",
	"unverified":    "Aide 无法验证这个应用。只有在你刚刚从 %s 发起连接时才继续。",
	"returns":       "完成后将返回 %s。",
	"loopback":      "这个应用会返回到你电脑上的某个程序，那里的任何程序都可能冒充它：只有在你刚刚亲自发起时才继续。",
	"server":        "服务器",
	"login":         "用你的 Aide 账号登录即可连接。连接后，它可以：",
	"can1":          "查看你的机器、项目和任务。",
	"can2":          "启动 Claude Code 或 Codex 任务并与之对话。这些 agent 会在你的项目里以完全权限运行。",
	"custodian":     "%s 保管这个连接的密钥：它可以代表该应用操作，并能看到该应用触及的任务。随时可以在 Aide 里断开它。",
	"account":       "账号",
	"password":      "密码",
	"wrong":         "账号或密码不对。",
	"locked":        "尝试次数太多，请等几分钟再试。",
	"off":           "这个账号的登录已关闭。请在 Aide › 设置 › 账号 里打开。",
	"connect":       "连接",
	"cancel":        "取消",
	"connected":     "已连接",
	"connectedBody": "%s 已获授权，但还需补齐「两件套」才算接入完成：①保活——保持一条 MCP 会话常开并在会话内定期 ping（无状态逐次调用撑不起在线）；②信箱盯守——按平台落实并当场声明唤醒方式（常驻进程型用内部定时查 message_list 或登记 doorbell webhook；webhook 型用 message_wake；会话型用外部轮询 hook 按未见 id 唤醒），处理完逐条 message_ack（留给所有者用 held=true）。验收：从 App 发一条真实消息，无需其他通道提醒即能取走并回复；仅「显示在线」不算完成。",
	"continue":      "继续前往 %s",
	"ended":         "这个连接已结束",
	"errorTitle":    "无法连接",
}

var zhErrors = map[string]string{
	"This page needs the link the app opened.":                                                    "这个页面需要从应用打开的链接进入。",
	"Open the connection from the app again, in this browser.":                                    "请在这个浏览器里，从应用重新发起连接。",
	"This connection request has expired. Connect from the app again.":                            "这个连接请求已过期，请从应用重新连接。",
	"This connection request belongs to another account. Connect from the app again.":             "这个连接请求属于另一个账号，请从应用重新连接。",
	"The app could not be identified. Connect from the app again.":                                "无法识别这个应用，请从应用重新连接。",
	"Something went wrong. Connect from the app again.":                                           "出了点问题，请从应用重新连接。",
	"This request came from another site.":                                                        "这个请求来自其他网站。",
	"Sign in again, then connect from the app again.":                                             "请重新登录，然后从应用重新连接。",
	"You have connected as many apps as Aide allows. Disconnect one in Aide, then connect again.": "已连接的应用数量已达上限。请先在 Aide 中断开一个，再重新连接。",
	"The app's request is missing client_id or redirect_uri.":                                     "应用的请求缺少 client_id 或 redirect_uri。",
	"The app is not registered here. Remove the connection in the app and add it again.":          "这个应用没有在此注册。请在应用里移除这个连接后重新添加。",
	"The app asked to return to an address it did not register.":                                  "应用要求返回到一个它没有登记的地址。",
	"Something went wrong. Try connecting again.":                                                 "出了点问题，请重新连接。",
}

var funcs = template.FuncMap{
	"f": func(format string, a ...any) string { return fmt.Sprintf(format, a...) },
}

const pageHead = `<!doctype html><html lang="{{.Lang}}"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1"><title>Aide</title>
<style>
:root{--ink:#1d1d1f;--muted:#6e6e73;--line:#e5e5ea;--bg:#f5f5f7;--card:#fff;--accent:#2f6fde;--warn:#b25000}
@media (prefers-color-scheme:dark){:root{--ink:#f5f5f7;--muted:#a1a1a6;--line:#38383a;--bg:#000;--card:#1c1c1e;--accent:#5b8ff0;--warn:#ff9f0a}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:15px/1.5 -apple-system,BlinkMacSystemFont,"Inter","PingFang SC",sans-serif}
main{max-width:440px;margin:8vh auto;padding:0 16px}.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:28px}
h1{font-size:20px;font-weight:600;margin:0 0 8px}p{margin:8px 0}.muted{color:var(--muted);font-size:13px}.warn{color:var(--warn);font-size:13px}
ul{margin:8px 0 8px 18px;padding:0}li{margin:4px 0}.row{display:flex;gap:8px;margin-top:20px}
button,.btn{flex:1;font:inherit;font-weight:500;padding:9px 14px;border-radius:9px;border:1px solid var(--line);background:var(--card);color:var(--ink);cursor:pointer;text-align:center;text-decoration:none}
.primary{background:var(--accent);border-color:var(--accent);color:#fff}
.kv{display:flex;justify-content:space-between;border-top:1px solid var(--line);padding-top:10px;margin-top:14px;font-size:13px;color:var(--muted)}
label{display:block;margin-top:12px;font-size:13px;color:var(--muted)}input{display:block;width:100%;margin-top:4px;font:inherit;padding:8px 10px;border-radius:8px;border:1px solid var(--line);background:var(--bg);color:var(--ink)}
</style></head><body><main><div class="card">`

const pageFoot = `</div></main></body></html>`

var consentPage = template.Must(template.New("consent").Funcs(funcs).Parse(pageHead + `
{{if not .Progress}}
<h1>{{f (index .T "title") .App}}</h1>
{{if .Verified}}<p class="muted">✓ {{index .T "verified"}}{{if .AppHost}} · {{.AppHost}}{{end}}</p>
{{else}}<p class="warn">{{f (index .T "unverified") .RedirectHost}}</p>{{end}}
<p class="muted">{{f (index .T "returns") .RedirectHost}}</p>
{{if .LoopbackOnly}}<p class="warn">{{index .T "loopback"}}</p>{{end}}
<p>{{index .T "login"}}</p>
<ul><li>{{index .T "can1"}}</li><li>{{index .T "can2"}}</li></ul>
<p class="muted">{{f (index .T "custodian") .Server}}</p>
<form method="post" action="/oauth/consent"><input type="hidden" name="id" value="{{.ID}}"><input type="hidden" name="csrf" value="{{.CSRF}}">
<label>{{index .T "account"}}<input name="account" autocomplete="username" value="{{.Typed}}"></label>
<label>{{index .T "password"}}<input name="password" type="password" autocomplete="current-password"></label>
{{if .Error}}<p class="warn">{{.Error}}</p>{{end}}
<div class="row"><button name="action" value="cancel">{{index .T "cancel"}}</button><button class="primary" name="action" value="connect">{{index .T "connect"}}</button></div></form>
<div class="kv"><span>{{index .T "server"}}</span><span>{{.Server}}</span></div>
{{else if eq .State "active"}}
<h1>{{index .T "connected"}}</h1><p>{{f (index .T "connectedBody") .App}}</p>
<div class="row"><a class="btn primary" id="go" href="{{.ContinueURL}}">{{f (index .T "continue") .App}}</a></div>
<script>setTimeout(function(){location.href=document.getElementById("go").href},800)</script>
{{else}}
<h1>{{index .T "ended"}}</h1><p>{{.Message}}</p>
{{end}}` + pageFoot))

var errorPageTmpl = template.Must(template.New("error").Funcs(funcs).Parse(pageHead + `
<h1>{{index .T "errorTitle"}}</h1><p>{{.Message}}</p>` + pageFoot))
