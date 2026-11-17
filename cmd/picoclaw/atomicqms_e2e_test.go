//go:build e2e

package main

// End-to-end test of /atomicqms connect against the production AtomicQMS
// Gitea image, the official gitea-mcp server and a real `sciclaw web`
// process. Run with:
//
//	go test -tags e2e -run TestAtomicQMSEndToEnd -v ./cmd/picoclaw
//
// It skips when Docker is unavailable. Set SCICLAW_E2E_BROWSER=1 to approve
// consent in a real browser instead of the scripted login, and
// SCICLAW_E2E_ARTIFACTS=<dir> to keep the chat transcript and logs.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
)

const (
	// Production image from atomicqms Dockerfile.kamal.
	e2eGiteaImage      = "ghcr.io/cincibrainlab/atomicqms-gitea@sha256:a8802e5fc29b442c3c66a9c13a246ba35ea33bdf313f264d08934cada42fd09d"
	e2eGiteaMCPVersion = "1.13.0"
	e2eUser            = "qmsuser"
	e2eUserPassword    = "UserPass123!"
	e2eAdmin           = "e2eadmin"
	e2eAdminPassword   = "AdminPass123!"
	e2eRepo            = "qms-docs"
	e2eIssueTitle      = "CAPA-042 calibration drift"
	e2eSender          = "1001"
	e2eOtherSender     = "2002"
)

var e2eGiteaMCPSHA256 = map[string]string{
	"amd64": "23383ad6adbb7a8a6a848ac736c07de19417d26311c3581160b356db01b3366d",
	"arm64": "b12335dbf1590e3f306a376789de192d37e9ae7729c28272081764d0dca87b07",
}

func TestAtomicQMSEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon unavailable")
	}
	if runtime.GOOS != "linux" || e2eGiteaMCPSHA256[runtime.GOARCH] == "" {
		t.Skipf("no pinned gitea-mcp build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	work := t.TempDir()
	artifacts := os.Getenv("SCICLAW_E2E_ARTIFACTS")
	if artifacts == "" {
		artifacts = filepath.Join(work, "artifacts")
	}
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		t.Fatal(err)
	}

	// Build sciclaw and fetch gitea-mcp with the caller's environment, before
	// HOME is pointed at a scratch directory.
	sciclawBin := filepath.Join(work, "sciclaw")
	build := exec.CommandContext(ctx, "go", "build", "-o", sciclawBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build sciclaw: %v\n%s", err, out)
	}
	giteaMCPBin := fetchGiteaMCP(t, work)

	home := filepath.Join(work, "home")
	if err := os.MkdirAll(filepath.Join(home, ".picoclaw"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	giteaURL, giteaContainer := startE2EGitea(t, ctx)
	webPort := e2eFreePort(t)
	mcpPort := e2eFreePort(t)
	webURL := fmt.Sprintf("http://127.0.0.1:%d", webPort)
	redirectURL := webURL + config.AtomicQMSCallbackPath
	clientID := seedE2EGitea(t, giteaURL, giteaContainer, redirectURL)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = filepath.Join(home, "sciclaw")
	cfg.Agents.Defaults.SharedWorkspace = filepath.Join(home, "sciclaw")
	cfg.AtomicQMS = config.AtomicQMSConfig{BaseURL: giteaURL, ClientID: clientID, WebURL: webURL}
	cfg.Tools.MCP = config.MCPConfig{
		Enabled: true,
		Servers: map[string]config.MCPServerConfig{
			"atomicqms": {Enabled: true, Type: "http", URL: fmt.Sprintf("http://127.0.0.1:%d/mcp", mcpPort), Auth: "atomicqms"},
		},
	}
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		t.Fatal(err)
	}

	webLog := filepath.Join(artifacts, "web.log")
	startE2EProcess(t, ctx, webLog, sciclawBin, "web", "--listen", fmt.Sprintf("127.0.0.1:%d", webPort))
	waitE2EHTTP(t, webURL+"/api/snapshot")
	mcpLog := filepath.Join(artifacts, "gitea-mcp.log")
	startE2EProcess(t, ctx, mcpLog, giteaMCPBin, "-t", "http", "-H", giteaURL, "-b", "127.0.0.1", "-p", fmt.Sprint(mcpPort), "-r")
	waitE2EPort(t, mcpPort)

	// Gateway side: the same consumer loop gatewayCmd runs, fed through the
	// bus exactly as a channel's HandleMessage publishes.
	gatewayLog := filepath.Join(artifacts, "gateway.log")
	logger.SetLevel(logger.DEBUG)
	if err := logger.EnableFileLogging(gatewayLog); err != nil {
		t.Fatal(err)
	}
	defer logger.DisableFileLogging()

	msgBus := bus.NewMessageBus()
	llm := &e2eLLM{}
	agentLoop := agent.NewAgentLoop(cfg, msgBus, llm)
	mcpManager := newMCPManager(ctx, cfg)
	if mcpManager == nil {
		t.Fatal("MCP manager did not connect")
	}
	defer mcpManager.Close()
	attachMCPTools(agentLoop, mcpManager, cfg.Tools.MCP)

	dm := &e2eDirect{}
	commands := newAtomicQMSCommands(cfg.AtomicQMS, nil)
	commands.direct = func(string) (directMessenger, bool) { return dm, true }
	go runGatewayDefaultDispatch(ctx, msgBus, agentLoop, nil, commands.chatHandler(msgBus), routing.LoopTarget{Workspace: cfg.WorkspacePath()})

	chat := &e2eChat{t: t, bus: msgBus, out: make(chan bus.OutboundMessage, 32)}
	go func() {
		for {
			msg, ok := msgBus.SubscribeOutbound(ctx)
			if !ok {
				return
			}
			chat.out <- msg
		}
	}()

	// connect: the link goes by direct message, the room gets one line.
	if got := chat.say(ctx, e2eSender, "/atomicqms connect"); got != atomicQMSReplyDMSent {
		t.Fatalf("connect reply = %q", got)
	}
	authURL := dm.lastLink(t)
	if !strings.Contains(authURL, "scope="+url.QueryEscape(auth.AtomicQMSScopes)) || !strings.Contains(authURL, "code_challenge_method=S256") {
		t.Fatalf("authorize URL missing exact scope or PKCE: %s", authURL)
	}
	writeE2EArtifact(t, artifacts, "authorize-url.txt", authURL)

	credKey := auth.AtomicQMSCredentialKey(atomicQMSSenderKey("discord", e2eSender))
	var callbackURL string
	if os.Getenv("SCICLAW_E2E_BROWSER") == "1" {
		t.Logf("Open this URL in a browser and approve as %s / %s:\n%s", e2eUser, e2eUserPassword, authURL)
		waitE2ECredential(t, credKey, 10*time.Minute)
	} else {
		callbackURL = scriptedE2EConsent(t, giteaURL, authURL)
		status, body := e2eGet(t, callbackURL)
		writeE2EArtifact(t, artifacts, "callback.html", body)
		if status != http.StatusOK || !strings.Contains(body, atomicQMSPageConnected) {
			t.Fatalf("callback = %d %q", status, body)
		}
	}
	cred, err := auth.GetCredential(credKey)
	if err != nil || cred == nil || cred.AccessToken == "" || cred.RefreshToken == "" {
		t.Fatalf("stored credential = %+v, %v", cred, err)
	}
	issued := []string{cred.AccessToken, cred.RefreshToken}

	if callbackURL != "" {
		status, body := e2eGet(t, callbackURL)
		if status != http.StatusBadRequest || !strings.Contains(body, atomicQMSPageFailed) {
			t.Fatalf("replayed callback = %d %q, want rejection", status, body)
		}
		t.Log("replayed callback rejected")
	}

	if got := chat.say(ctx, e2eSender, "/atomicqms status"); got != fmt.Sprintf(atomicQMSReplyConnectedAs, e2eUser) {
		t.Fatalf("status reply = %q", got)
	}

	// The agent reads the issue through gitea-mcp as the connected user.
	if got := chat.say(ctx, e2eSender, "What does issue 1 in qmsuser/qms-docs say?"); !strings.Contains(got, e2eIssueTitle) {
		t.Fatalf("agent reply = %q, want the issue title", got)
	}
	accessLog := e2eDockerLogs(t, giteaContainer)
	identity := regexp.MustCompile(`- ` + e2eUser + ` \[[^\]]+\] "GET /api/v1/repos/` + e2eUser + `/` + e2eRepo + `/issues/1 `)
	if !identity.MatchString(accessLog) {
		t.Fatalf("Gitea access log has no issue read by %s:\n%s", e2eUser, tailLines(accessLog, 20))
	}
	t.Logf("Gitea access log: %s", identity.FindString(accessLog))

	// Another sender has no credential and must not borrow this one.
	if got := chat.say(ctx, e2eOtherSender, "What does issue 1 in qmsuser/qms-docs say?"); !strings.Contains(got, atomicQMSToolNotConnectedLLM) {
		t.Fatalf("unconnected sender reply = %q", got)
	}

	// The issued token is read-only: a direct write is rejected, while the
	// same write with full credentials succeeds.
	issuePath := "/api/v1/repos/" + e2eUser + "/" + e2eRepo + "/issues"
	writeBody := `{"title":"write attempt"}`
	if status, body := e2eGitea(t, http.MethodPost, giteaURL+issuePath, "Bearer "+cred.AccessToken, writeBody); status != http.StatusForbidden {
		t.Fatalf("write with issued token = %d %s, want 403", status, body)
	} else {
		t.Logf("write with issued token: %d %s", status, strings.TrimSpace(body))
	}
	if status, body := e2eGitea(t, http.MethodPost, giteaURL+issuePath, e2eBasic(e2eUser, e2eUserPassword), writeBody); status != http.StatusCreated {
		t.Fatalf("control write with password = %d %s, want 201", status, body)
	}

	// An expiring token is refreshed. Gitea's default (INVALIDATE_REFRESH_TOKENS
	// = false, as in production) keeps the refresh token and mints identical
	// JWTs within one second, so each refresh is counted from Gitea's access
	// log instead of by comparing tokens.
	tokenGrants := func() int {
		return strings.Count(e2eDockerLogs(t, giteaContainer), `"POST /login/oauth/access_token `)
	}
	before := tokenGrants()
	cred.ExpiresAt = time.Now().Add(time.Minute)
	if err := auth.SetCredential(credKey, cred); err != nil {
		t.Fatal(err)
	}
	if got := chat.say(ctx, e2eSender, "What does issue 1 in qmsuser/qms-docs say?"); !strings.Contains(got, e2eIssueTitle) {
		t.Fatalf("agent reply after refresh = %q", got)
	}
	refreshed, _ := auth.GetCredential(credKey)
	if tokenGrants() != before+1 || refreshed == nil || !refreshed.ExpiresAt.After(time.Now().Add(30*time.Minute)) {
		t.Fatalf("expiring credential was not refreshed exactly once (token grants %d -> %d)", before, tokenGrants())
	}
	t.Log("expiring credential refreshed once")
	issued = append(issued, refreshed.AccessToken, refreshed.RefreshToken)

	// A token gitea-mcp rejects with 401 is refreshed once and the call retried.
	before = tokenGrants()
	rejected := *refreshed
	rejected.AccessToken = "rejected-access-token"
	if err := auth.SetCredential(credKey, &rejected); err != nil {
		t.Fatal(err)
	}
	if got := chat.say(ctx, e2eSender, "What does issue 1 in qmsuser/qms-docs say?"); !strings.Contains(got, e2eIssueTitle) {
		t.Fatalf("agent reply after 401 = %q", got)
	}
	retried, _ := auth.GetCredential(credKey)
	if tokenGrants() != before+1 || retried == nil || retried.AccessToken == rejected.AccessToken {
		t.Fatalf("401 did not trigger exactly one refresh (token grants %d -> %d)", before, tokenGrants())
	}
	t.Log("401 from gitea-mcp triggered one refresh and a successful retry")
	issued = append(issued, retried.AccessToken, retried.RefreshToken)

	if got := chat.say(ctx, e2eSender, "/atomicqms disconnect"); got != atomicQMSReplyDisconnected {
		t.Fatalf("disconnect reply = %q", got)
	}
	if got := chat.say(ctx, e2eSender, "/atomicqms status"); got != atomicQMSReplyNotConnected {
		t.Fatalf("status after disconnect = %q", got)
	}
	if got := chat.say(ctx, e2eSender, "What does issue 1 in qmsuser/qms-docs say?"); !strings.Contains(got, atomicQMSToolNotConnectedLLM) {
		t.Fatalf("agent reply after disconnect = %q", got)
	}
	if llm.startedJobFor("/atomicqms") {
		t.Fatal("an /atomicqms command reached the model")
	}

	writeE2EArtifact(t, artifacts, "chat.txt", chat.transcript())
	writeE2EArtifact(t, artifacts, "gitea.log", e2eDockerLogs(t, giteaContainer))

	// No issued token appears in any log, chat message or model input.
	haystacks := map[string]string{
		"chat transcript": chat.transcript(),
		"direct messages": dm.all(),
		"model input":     llm.allInput(),
		"gitea log":       e2eDockerLogs(t, giteaContainer),
	}
	for _, path := range []string{gatewayLog, webLog, mcpLog} {
		data, _ := os.ReadFile(path)
		haystacks[filepath.Base(path)] = string(data)
	}
	for name, text := range haystacks {
		for _, token := range issued {
			if token != "" && strings.Contains(text, token) {
				t.Fatalf("issued token found in %s", name)
			}
		}
	}
	t.Logf("no issued token in: gateway.log, web.log, gitea-mcp.log, gitea log, chat, DMs, model input")
}

// e2eChat publishes inbound messages the way a channel does and waits for the
// reply in the same chat.
type e2eChat struct {
	t   *testing.T
	bus *bus.MessageBus
	out chan bus.OutboundMessage

	mu  sync.Mutex
	log []string
}

func (c *e2eChat) say(ctx context.Context, sender, content string) string {
	c.t.Helper()
	chatID := "room-1"
	c.record(fmt.Sprintf("[%s] user %s: %s", time.Now().Format("15:04:05"), sender, content))
	if err := c.bus.PublishInbound(ctx, bus.InboundMessage{
		Channel:    "discord",
		SenderID:   sender,
		ChatID:     chatID,
		Content:    content,
		SessionKey: "discord:" + chatID + ":" + sender,
		Metadata:   map[string]string{"is_dm": "false"},
	}); err != nil {
		c.t.Fatal(err)
	}
	for {
		select {
		case msg := <-c.out:
			if msg.ChatID != chatID {
				continue
			}
			c.record(fmt.Sprintf("[%s] sciclaw: %s", time.Now().Format("15:04:05"), msg.Content))
			return strings.TrimSpace(msg.Content)
		case <-time.After(2 * time.Minute):
			c.t.Fatalf("no reply to %q", content)
		}
	}
}

func (c *e2eChat) record(line string) {
	c.mu.Lock()
	c.log = append(c.log, line)
	c.mu.Unlock()
}

func (c *e2eChat) transcript() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.log, "\n") + "\n"
}

type e2eDirect struct {
	mu   sync.Mutex
	msgs []string
}

func (d *e2eDirect) SendDirect(_ context.Context, userID, content string) error {
	d.mu.Lock()
	d.msgs = append(d.msgs, userID+": "+content)
	d.mu.Unlock()
	return nil
}

func (d *e2eDirect) lastLink(t *testing.T) string {
	t.Helper()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.msgs) == 0 {
		t.Fatal("no direct message sent")
	}
	last := d.msgs[len(d.msgs)-1]
	prefix := e2eSender + ": " + strings.TrimSuffix(atomicQMSDirectLink, "%s")
	if !strings.HasPrefix(last, prefix) {
		t.Fatalf("direct message = %q", last)
	}
	return strings.TrimPrefix(last, prefix)
}

func (d *e2eDirect) all() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.msgs, "\n")
}

// e2eLLM stands in for the model: it asks gitea-mcp for the issue and answers
// with what the tool returned.
type e2eLLM struct {
	mu    sync.Mutex
	input []string
}

func (l *e2eLLM) Chat(_ context.Context, messages []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	l.mu.Lock()
	for _, m := range messages {
		l.input = append(l.input, m.Role+": "+m.Content)
	}
	l.mu.Unlock()
	last := messages[len(messages)-1]
	if last.Role == "tool" {
		return &providers.LLMResponse{Content: "Tool result: " + firstLines(last.Content, 3), FinishReason: "stop"}, nil
	}
	return &providers.LLMResponse{
		ToolCalls: []providers.ToolCall{{
			ID:   "call_issue",
			Name: "mcp_atomicqms_issue_read",
			Arguments: map[string]interface{}{
				"method": "get", "owner": e2eUser, "repo": e2eRepo, "issue_number": 1,
			},
		}},
		FinishReason: "tool_calls",
	}, nil
}

func (l *e2eLLM) GetDefaultModel() string { return "e2e-scripted" }

func (l *e2eLLM) startedJobFor(prefix string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.input {
		if strings.HasPrefix(line, "user: "+prefix) {
			return true
		}
	}
	return false
}

func (l *e2eLLM) allInput() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.input, "\n")
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, " ")
}

func tailLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func startE2EGitea(t *testing.T, ctx context.Context) (string, string) {
	t.Helper()
	port := e2eFreePort(t)
	suffix := make([]byte, 4)
	_, _ = rand.Read(suffix)
	name := "sciclaw-e2e-gitea-" + hex.EncodeToString(suffix)
	giteaURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	args := []string{"run", "-d", "--name", name, "-p", fmt.Sprintf("127.0.0.1:%d:3000", port),
		"-e", "GITEA__security__INSTALL_LOCK=true",
		"-e", "GITEA__database__DB_TYPE=sqlite3",
		"-e", "GITEA__server__ROOT_URL=" + giteaURL + "/",
		"-e", "GITEA__service__DISABLE_REGISTRATION=true",
		"-e", "GITEA__oauth2__ENABLED=true",
		"-e", "GITEA__log__ACCESS=console",
		e2eGiteaImage}
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	waitE2EHTTP(t, giteaURL+"/api/v1/version")
	return giteaURL, name
}

func seedE2EGitea(t *testing.T, giteaURL, container, redirectURL string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", "-u", "git", container, "gitea", "admin", "user", "create",
		"--admin", "--username", e2eAdmin, "--password", e2eAdminPassword,
		"--email", "admin@example.test", "--must-change-password=false").CombinedOutput()
	if err != nil {
		t.Fatalf("create admin: %v\n%s", err, out)
	}
	admin := e2eBasic(e2eAdmin, e2eAdminPassword)
	user := e2eBasic(e2eUser, e2eUserPassword)
	steps := []struct {
		auth, path, body string
	}{
		{admin, "/api/v1/admin/users", `{"username":"` + e2eUser + `","password":"` + e2eUserPassword + `","email":"qmsuser@example.test","must_change_password":false}`},
		{user, "/api/v1/user/repos", `{"name":"` + e2eRepo + `","private":true}`},
		{user, "/api/v1/repos/" + e2eUser + "/" + e2eRepo + "/issues", `{"title":"` + e2eIssueTitle + `","body":"Balance B-7 drifted 0.3 mg."}`},
	}
	for _, step := range steps {
		if status, body := e2eGitea(t, http.MethodPost, giteaURL+step.path, step.auth, step.body); status != http.StatusCreated {
			t.Fatalf("POST %s = %d %s", step.path, status, body)
		}
	}
	status, body := e2eGitea(t, http.MethodPost, giteaURL+"/api/v1/user/applications/oauth2", admin,
		`{"name":"sciclaw-e2e","redirect_uris":["`+redirectURL+`"],"confidential_client":false}`)
	if status != http.StatusCreated {
		t.Fatalf("create OAuth app = %d %s", status, body)
	}
	var app struct {
		ClientID     string `json:"client_id"`
		Confidential bool   `json:"confidential_client"`
	}
	if err := json.Unmarshal([]byte(body), &app); err != nil || app.ClientID == "" || app.Confidential {
		t.Fatalf("OAuth app = %s", body)
	}
	return app.ClientID
}

var e2eHiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)"`)

// scriptedE2EConsent signs in as the test user, approves the consent screen
// and returns the callback URL Gitea redirects to.
func scriptedE2EConsent(t *testing.T, giteaURL, authURL string) string {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if !strings.HasPrefix(req.URL.String(), giteaURL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	form := func(page string) url.Values {
		v := url.Values{}
		for _, m := range e2eHiddenInput.FindAllStringSubmatch(page, -1) {
			v.Set(m[1], htmlUnescape(m[2]))
		}
		return v
	}
	read := func(resp *http.Response, err error) (*http.Response, string) {
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	_, loginPage := read(client.Get(giteaURL + "/user/login"))
	login := form(loginPage)
	login.Set("user_name", e2eUser)
	login.Set("password", e2eUserPassword)
	read(client.PostForm(giteaURL+"/user/login", login))

	resp, consent := read(client.Get(authURL))
	if resp.StatusCode != http.StatusOK || !strings.Contains(consent, "/login/oauth/grant") {
		t.Fatalf("consent page = %d", resp.StatusCode)
	}
	grant := form(consent)
	if grant.Get("scope") != auth.AtomicQMSScopes {
		t.Fatalf("consent scope = %q", grant.Get("scope"))
	}
	grant.Set("granted", "true")
	resp, _ = read(client.PostForm(giteaURL+"/login/oauth/grant", grant))
	location := resp.Header.Get("Location")
	if !strings.Contains(location, config.AtomicQMSCallbackPath) || !strings.Contains(location, "code=") {
		t.Fatalf("grant redirect = %d %q", resp.StatusCode, location)
	}
	return location
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&amp;", "&", "&#34;", `"`, "&#39;", "'", "&lt;", "<", "&gt;", ">").Replace(s)
}

func e2eBasic(user, password string) string {
	req, _ := http.NewRequest(http.MethodGet, "http://x", nil)
	req.SetBasicAuth(user, password)
	return req.Header.Get("Authorization")
}

func e2eGitea(t *testing.T, method, target, authorization, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func e2eGet(t *testing.T, target string) (int, string) {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func e2eDockerLogs(t *testing.T, container string) string {
	t.Helper()
	out, _ := exec.Command("docker", "logs", container).CombinedOutput()
	return string(out)
}

func waitE2ECredential(t *testing.T, key string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cred, _ := auth.GetCredential(key); cred != nil && cred.AccessToken != "" {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatal("consent was not approved in time")
}

func fetchGiteaMCP(t *testing.T, dir string) string {
	t.Helper()
	arch := map[string]string{"amd64": "x86_64", "arm64": "arm64"}[runtime.GOARCH]
	src := fmt.Sprintf("https://gitea.com/gitea/gitea-mcp/releases/download/v%s/gitea-mcp_Linux_%s.tar.gz", e2eGiteaMCPVersion, arch)
	resp, err := http.Get(src)
	if err != nil {
		t.Skipf("download gitea-mcp: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Skipf("download gitea-mcp: status %d %v", resp.StatusCode, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != e2eGiteaMCPSHA256[runtime.GOARCH] {
		t.Fatalf("gitea-mcp checksum mismatch for %s", src)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("gitea-mcp binary not in archive: %v", err)
		}
		if filepath.Base(hdr.Name) != "gitea-mcp" {
			continue
		}
		bin := filepath.Join(dir, "gitea-mcp")
		f, err := os.OpenFile(bin, os.O_CREATE|os.O_WRONLY, 0o755)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(f, tr); err != nil {
			t.Fatal(err)
		}
		f.Close()
		return bin
	}
}

func startE2EProcess(t *testing.T, ctx context.Context, logPath, bin string, args ...string) {
	t.Helper()
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		logFile.Close()
	})
}

func e2eFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitE2EHTTP(t *testing.T, target string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(target); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("%s did not come up", target)
}

func waitE2EPort(t *testing.T, port int) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
			conn.Close()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("port %d did not open", port)
}

func writeE2EArtifact(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
