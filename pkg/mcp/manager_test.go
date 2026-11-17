package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
)

// authServer is a stateless streamable-HTTP MCP server that, like gitea-mcp,
// passes Authorization through per request and answers 401 for a token it
// does not know. Its whoami tool returns the user the token belongs to.
type authServer struct {
	*httptest.Server
	users map[string]string // bearer token -> user

	mu         sync.Mutex
	anonymous  int // requests without Authorization
	toolCalls  int
	rejections int
}

func newAuthServer(t *testing.T, users map[string]string) *authServer {
	t.Helper()
	s := &authServer{users: users}
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "whoami", Description: "Return the caller"},
		func(_ context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
			user := s.users[strings.TrimPrefix(req.Extra.Header.Get("Authorization"), "Bearer ")]
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: user}}}, nil, nil
		})
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		s.mu.Lock()
		switch {
		case header == "":
			s.anonymous++
		case s.users[strings.TrimPrefix(header, "Bearer ")] == "":
			s.rejections++
			s.mu.Unlock()
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		s.mu.Unlock()
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

// tokenStore is a TokenSource backed by a map, with an optional refresh.
type tokenStore struct {
	mu        sync.Mutex
	tokens    map[string]string
	refresh   map[string]string // rejected token -> refreshed token
	refreshes atomic.Int32
}

func (s *tokenStore) source(senderKey, rejected string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	token, ok := s.tokens[senderKey]
	if !ok {
		return "", ErrNotConnected
	}
	if rejected != "" && rejected == token {
		s.refreshes.Add(1)
		if fresh, ok := s.refresh[token]; ok {
			s.tokens[senderKey] = fresh
			return fresh, nil
		}
	}
	return token, nil
}

func (s *tokenStore) set(senderKey, token string) {
	s.mu.Lock()
	s.tokens[senderKey] = token
	s.mu.Unlock()
}

func newTestManager(t *testing.T, srv *authServer, tokens *tokenStore) *Manager {
	t.Helper()
	m := NewManager(map[string]TokenSource{AuthAtomicQMS: tokens.source})
	err := m.LoadFromConfig(context.Background(), config.MCPConfig{
		Enabled: true,
		Servers: map[string]config.MCPServerConfig{
			"atomicqms": {Enabled: true, Type: "http", URL: srv.URL, Auth: AuthAtomicQMS},
		},
	})
	if err != nil {
		t.Fatalf("LoadFromConfig: %v", err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func callWhoami(t *testing.T, m *Manager, sender string) (string, error) {
	t.Helper()
	result, err := m.CallTool(context.Background(), "atomicqms", "whoami", sender, map[string]any{})
	if err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", nil
	}
	return result.Content[0].(*mcp.TextContent).Text, nil
}

func TestManagerDiscoversToolsWithoutCredentials(t *testing.T) {
	srv := newAuthServer(t, map[string]string{})
	m := newTestManager(t, srv, &tokenStore{tokens: map[string]string{}})

	tools := m.Tools()["atomicqms"]
	if len(tools) != 1 || tools[0].Name != "whoami" {
		t.Fatalf("tools = %v", tools)
	}
	if srv.anonymous == 0 {
		t.Fatal("discovery sent credentials")
	}
}

func TestManagerCallsActAsEachSender(t *testing.T) {
	srv := newAuthServer(t, map[string]string{"tok-a": "alice", "tok-b": "bob"})
	m := newTestManager(t, srv, &tokenStore{tokens: map[string]string{"discord:a": "tok-a", "discord:b": "tok-b"}})

	for sender, want := range map[string]string{"discord:a": "alice", "discord:b": "bob"} {
		got, err := callWhoami(t, m, sender)
		if err != nil || got != want {
			t.Errorf("whoami as %s = %q, %v; want %q", sender, got, err, want)
		}
	}
}

func TestManagerNotConnectedSendsNoRequest(t *testing.T) {
	srv := newAuthServer(t, map[string]string{"tok-a": "alice"})
	m := newTestManager(t, srv, &tokenStore{tokens: map[string]string{"discord:a": "tok-a"}})
	before := srv.anonymous

	for _, sender := range []string{"", "discord:unknown", "subagent:task-1"} {
		if _, err := callWhoami(t, m, sender); !errors.Is(err, ErrNotConnected) {
			t.Errorf("sender %q: err = %v, want ErrNotConnected", sender, err)
		}
	}
	if srv.anonymous != before || srv.rejections != 0 {
		t.Errorf("unconnected calls reached the server: anonymous %d->%d, rejections %d", before, srv.anonymous, srv.rejections)
	}
}

func TestManagerRefreshesOnceOn401(t *testing.T) {
	srv := newAuthServer(t, map[string]string{"fresh": "alice"})
	tokens := &tokenStore{
		tokens:  map[string]string{"discord:a": "stale"},
		refresh: map[string]string{"stale": "fresh"},
	}
	m := newTestManager(t, srv, tokens)

	got, err := callWhoami(t, m, "discord:a")
	if err != nil || got != "alice" {
		t.Fatalf("whoami = %q, %v", got, err)
	}
	if tokens.refreshes.Load() != 1 {
		t.Errorf("refreshes = %d, want 1", tokens.refreshes.Load())
	}
}

func TestManagerRejectedSenderDoesNotBreakOthers(t *testing.T) {
	srv := newAuthServer(t, map[string]string{"tok-a": "alice", "tok-b": "bob"})
	tokens := &tokenStore{tokens: map[string]string{"discord:a": "tok-a", "discord:b": "tok-b"}}
	m := newTestManager(t, srv, tokens)
	if _, err := callWhoami(t, m, "discord:a"); err != nil {
		t.Fatal(err)
	}

	// Alice's grant is revoked and cannot be refreshed.
	tokens.set("discord:a", "revoked")
	if _, err := callWhoami(t, m, "discord:a"); err == nil {
		t.Fatal("call with a revoked token succeeded")
	}
	if tokens.refreshes.Load() != 1 {
		t.Errorf("refreshes = %d, want 1", tokens.refreshes.Load())
	}
	if got, err := callWhoami(t, m, "discord:b"); err != nil || got != "bob" {
		t.Errorf("bob after alice's 401 = %q, %v", got, err)
	}

	// After reconnecting, Alice gets a new session.
	tokens.set("discord:a", "tok-a")
	if got, err := callWhoami(t, m, "discord:a"); err != nil || got != "alice" {
		t.Errorf("alice after reconnect = %q, %v", got, err)
	}
}

func TestManagerRejectsUnknownAuth(t *testing.T) {
	srv := newAuthServer(t, map[string]string{})
	m := NewManager(nil)
	defer m.Close()
	err := m.ConnectServer(context.Background(), "x", config.MCPServerConfig{Enabled: true, URL: srv.URL, Auth: "static"})
	if err == nil || !strings.Contains(err.Error(), "unsupported auth") {
		t.Fatalf("err = %v", err)
	}
}

func TestManagerSessionOutlivesCallContext(t *testing.T) {
	srv := newAuthServer(t, map[string]string{"tok-a": "alice"})
	m := newTestManager(t, srv, &tokenStore{tokens: map[string]string{"discord:a": "tok-a"}})

	// Each agent job runs under its own cancellable context; the cached
	// session must not die with the first one.
	jobCtx, cancel := context.WithCancel(context.Background())
	if _, err := m.CallTool(jobCtx, "atomicqms", "whoami", "discord:a", map[string]any{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	cancel()
	if got, err := callWhoami(t, m, "discord:a"); err != nil || got != "alice" {
		t.Fatalf("call after first job context ended = %q, %v", got, err)
	}
}
