package main

// atomicqms_test.go covers the /atomicqms chat command and the web server's
// /atomicqms/callback route. A fake Gitea answers the token and user
// endpoints, and HOME points at a temp dir so auth.json and config.json are
// scratch files.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newFakeGitea(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "gitea-access", "refresh_token": "gitea-refresh", "expires_in": 3600,
			})
		case "/api/v1/user":
			json.NewEncoder(w).Encode(map[string]string{"login": "qmsuser"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupAtomicQMSHome writes a config.json pointing at giteaURL under a temp
// HOME.
func setupAtomicQMSHome(t *testing.T, giteaURL string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.AtomicQMS = config.AtomicQMSConfig{BaseURL: giteaURL, ClientID: "client-1"}
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		t.Fatal(err)
	}
}

func beginRequest(remoteAddr string, header http.Header) *http.Request {
	req := httptest.NewRequest(http.MethodPost, config.AtomicQMSCallbackPath, strings.NewReader(`{"channel":"discord","sender_id":"1001"}`))
	req.RemoteAddr = remoteAddr
	for k, v := range header {
		req.Header[k] = v
	}
	return req
}

func TestHandleAtomicQMSCallbackBeginAcceptsOnlyDirectLoopback(t *testing.T) {
	setupAtomicQMSHome(t, newFakeGitea(t).URL)
	srv := newWebServer(nil, "")

	tests := []struct {
		name   string
		remote string
		header http.Header
		want   int
	}{
		{"loopback", "127.0.0.1:5555", nil, http.StatusOK},
		{"ipv6 loopback", "[::1]:5555", nil, http.StatusOK},
		{"remote", "203.0.113.9:5555", nil, http.StatusForbidden},
		{"proxied", "127.0.0.1:5555", http.Header{"X-Forwarded-For": {"203.0.113.9"}}, http.StatusForbidden},
		{"forwarded", "127.0.0.1:5555", http.Header{"Forwarded": {"for=203.0.113.9"}}, http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, beginRequest(tt.remote, tt.header))
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.want, rec.Body.String())
			}
		})
	}
}

func TestHandleAtomicQMSCallbackCompletesOnce(t *testing.T) {
	gitea := newFakeGitea(t)
	setupAtomicQMSHome(t, gitea.URL)
	srv := newWebServer(nil, "")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, beginRequest("127.0.0.1:5555", nil))
	var begin atomicQMSBeginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &begin); err != nil || !strings.HasPrefix(begin.URL, gitea.URL+"/login/oauth/authorize?") {
		t.Fatalf("begin = %d %s", rec.Code, rec.Body.String())
	}
	authURL, _ := url.Parse(begin.URL)
	if got := authURL.Query().Get("redirect_uri"); got != "http://127.0.0.1:4142/atomicqms/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	callback := config.AtomicQMSCallbackPath + "?code=abc&state=" + authURL.Query().Get("state")

	tests := []struct {
		name   string
		target string
		status int
		line   string
	}{
		{"valid", callback, http.StatusOK, atomicQMSPageConnected},
		{"replayed", callback, http.StatusBadRequest, atomicQMSPageFailed},
		{"unknown state", config.AtomicQMSCallbackPath + "?code=abc&state=nope", http.StatusBadRequest, atomicQMSPageFailed},
		{"no state", config.AtomicQMSCallbackPath + "?code=abc", http.StatusBadRequest, atomicQMSPageFailed},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.target, nil))
		if rec.Code != tt.status {
			t.Fatalf("%s: status = %d, want %d", tt.name, rec.Code, tt.status)
		}
		body := rec.Body.String()
		if strings.Count(body, "<p>") != 1 || !strings.Contains(body, "<p>"+tt.line+"</p>") {
			t.Fatalf("%s: page = %s", tt.name, body)
		}
		if strings.Contains(body, "gitea-access") || strings.Contains(body, "gitea-refresh") {
			t.Fatalf("%s: page leaks a token", tt.name)
		}
	}

	cred, _ := auth.GetCredential(auth.AtomicQMSCredentialKey("discord:1001"))
	if cred == nil || cred.AccessToken != "gitea-access" || cred.AccountID != "qmsuser" {
		t.Fatalf("stored credential = %+v", cred)
	}
}

type fakeDirect struct {
	sent []string
	err  error
}

func (d *fakeDirect) SendDirect(_ context.Context, userID, content string) error {
	if d.err != nil {
		return d.err
	}
	d.sent = append(d.sent, userID+" "+content)
	return nil
}

func TestAtomicQMSCommandsHandle(t *testing.T) {
	gitea := newFakeGitea(t)
	setupAtomicQMSHome(t, gitea.URL)
	web := httptest.NewServer(newWebServer(nil, ""))
	defer web.Close()

	connected := &auth.AuthCredential{AccessToken: "t", AccountID: "qmsuser", Provider: "atomicqms"}
	tests := []struct {
		name      string
		webURL    string
		dmErr     error
		inDM      bool
		action    string
		stored    *auth.AuthCredential
		wantReply string
		wantLink  bool
	}{
		{name: "connect from a room", webURL: web.URL, action: "connect", wantReply: atomicQMSReplyDMSent, wantLink: true},
		{name: "connect from a DM", webURL: web.URL, action: "connect", inDM: true, wantReply: "", wantLink: true},
		{name: "connect fails closed without DM", webURL: web.URL, action: "connect", dmErr: errors.New("blocked"), wantReply: atomicQMSReplyDMFailed},
		{name: "connect with web server down", webURL: "http://127.0.0.1:1", action: "connect", wantReply: atomicQMSReplyUnavailable},
		{name: "status not connected", action: "status", wantReply: atomicQMSReplyNotConnected},
		{name: "status connected", action: "status", stored: connected, wantReply: "Connected to AtomicQMS as qmsuser."},
		{name: "disconnect", action: "disconnect", stored: connected, wantReply: atomicQMSReplyDisconnected},
		{name: "usage", action: "", wantReply: atomicQMSReplyUsage},
		{name: "unknown action", action: "write", wantReply: atomicQMSReplyUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := auth.AtomicQMSCredentialKey(atomicQMSSenderKey("discord", "1001"))
			_ = auth.DeleteCredential(key)
			if tt.stored != nil {
				if err := auth.SetCredential(key, tt.stored); err != nil {
					t.Fatal(err)
				}
			}
			dm := &fakeDirect{err: tt.dmErr}
			commands := &atomicQMSCommands{
				cfg:    config.AtomicQMSConfig{WebURL: tt.webURL},
				client: http.DefaultClient,
				direct: func(channel string) (directMessenger, bool) { return dm, channel == "discord" },
			}

			reply := commands.handle(context.Background(), "discord", "1001", tt.inDM, tt.action)
			if reply != tt.wantReply {
				t.Fatalf("reply = %q, want %q", reply, tt.wantReply)
			}
			if tt.wantLink != (len(dm.sent) == 1) {
				t.Fatalf("direct messages = %v", dm.sent)
			}
			if tt.wantLink && !strings.HasPrefix(dm.sent[0], "1001 Connect AtomicQMS: "+gitea.URL+"/login/oauth/authorize?") {
				t.Fatalf("direct message = %q", dm.sent[0])
			}
			if strings.Contains(reply, "http") {
				t.Fatalf("reply carries a link: %q", reply)
			}
			if tt.action == "disconnect" {
				if cred, _ := auth.GetCredential(key); cred != nil {
					t.Fatal("credential survived disconnect")
				}
			}
		})
	}
}

func TestAtomicQMSChatHandlerRepliesInChat(t *testing.T) {
	setupAtomicQMSHome(t, newFakeGitea(t).URL)
	mb := bus.NewMessageBus()
	defer mb.Close()
	commands := &atomicQMSCommands{client: http.DefaultClient, direct: func(string) (directMessenger, bool) { return nil, false }}

	commands.chatHandler(mb)(context.Background(), bus.InboundMessage{
		Channel: "telegram", SenderID: "42", ChatID: "-100", Content: "/atomicqms status",
	}, "status")

	out, ok := mb.SubscribeOutbound(context.Background())
	if !ok || out.Channel != "telegram" || out.ChatID != "-100" || out.Content != atomicQMSReplyNotConnected {
		t.Fatalf("outbound = %+v", out)
	}
}
