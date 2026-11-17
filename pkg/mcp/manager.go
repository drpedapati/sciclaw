// Package mcp connects to MCP servers and exposes their tools to the agent.
//
// Ported from sipeed/picoclaw pkg/mcp/manager.go and trimmed to streamable
// HTTP. Servers configured with an Auth credential get one client session per
// sender, and every HTTP request carries that sender's bearer token, read at
// call time. A session is per sender because the go-sdk closes a session after
// a 401, which would otherwise break the server for every other user.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// AuthAtomicQMS is the MCPServerConfig.Auth value for per-user AtomicQMS
// tokens.
const AuthAtomicQMS = "atomicqms"

// ErrNotConnected means the sender has no credential for an authenticated
// server.
var ErrNotConnected = tools.ErrMCPNotConnected

// TokenSource returns the bearer token for senderKey. When rejected is
// non-empty the server refused that token, and the source should return a
// refreshed one. It returns ErrNotConnected when the sender has no credential.
type TokenSource func(senderKey, rejected string) (string, error)

type server struct {
	name      string
	cfg       config.MCPServerConfig
	tools     []*mcp.Tool
	discovery *mcp.ClientSession
	tokens    TokenSource

	mu       sync.Mutex
	sessions map[string]*mcp.ClientSession
}

// Manager manages MCP server connections.
type Manager struct {
	tokens map[string]TokenSource

	mu      sync.RWMutex
	servers map[string]*server
	closed  bool
}

// NewManager creates a manager. tokens maps an MCPServerConfig.Auth value to
// the source of per-user bearer tokens for it.
func NewManager(tokens map[string]TokenSource) *Manager {
	return &Manager{tokens: tokens, servers: map[string]*server{}}
}

// LoadFromConfig connects to every enabled server and lists its tools. It
// returns an error only when every enabled server failed.
func (m *Manager) LoadFromConfig(ctx context.Context, cfg config.MCPConfig) error {
	if !cfg.Enabled || len(cfg.Servers) == 0 {
		return nil
	}
	var errs []error
	enabled := 0
	for name, serverCfg := range cfg.Servers {
		if !serverCfg.Enabled {
			continue
		}
		enabled++
		if err := m.ConnectServer(ctx, name, serverCfg); err != nil {
			logger.ErrorCF("mcp", "Failed to connect to MCP server", map[string]interface{}{
				"server": name,
				"error":  err.Error(),
			})
			errs = append(errs, fmt.Errorf("server %s: %w", name, err))
		}
	}
	if enabled > 0 && len(errs) == enabled {
		return errors.Join(errs...)
	}
	return nil
}

// ConnectServer connects to one server without credentials and lists its
// tools.
func (m *Manager) ConnectServer(ctx context.Context, name string, cfg config.MCPServerConfig) error {
	var tokens TokenSource
	if cfg.Auth != "" {
		tokens = m.tokens[cfg.Auth]
		if tokens == nil {
			return fmt.Errorf("unsupported auth %q", cfg.Auth)
		}
	}
	session, err := connect(ctx, cfg, http.DefaultTransport)
	if err != nil {
		return err
	}
	listed, err := listTools(ctx, session)
	if err != nil {
		_ = session.Close()
		return err
	}
	init := session.InitializeResult()
	logger.InfoCF("mcp", "Connected to MCP server", map[string]interface{}{
		"server":         name,
		"server_name":    init.ServerInfo.Name,
		"server_version": init.ServerInfo.Version,
		"tool_count":     len(listed),
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		_ = session.Close()
		return fmt.Errorf("manager is closed")
	}
	m.servers[name] = &server{
		name:      name,
		cfg:       cfg,
		tools:     listed,
		discovery: session,
		tokens:    tokens,
		sessions:  map[string]*mcp.ClientSession{},
	}
	return nil
}

func connect(ctx context.Context, cfg config.MCPServerConfig, rt http.RoundTripper) (*mcp.ClientSession, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("url is required")
	}
	var disableStandaloneSSE bool
	switch cfg.Type {
	case "", "http", "streamable-http":
		disableStandaloneSSE = true
	case "sse":
	default:
		return nil, fmt.Errorf("unsupported transport type %q (supported: http, sse)", cfg.Type)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "sciclaw", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             cfg.URL,
		DisableStandaloneSSE: disableStandaloneSSE,
		HTTPClient:           &http.Client{Transport: rt},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}
	return session, nil
}

func listTools(ctx context.Context, session *mcp.ClientSession) ([]*mcp.Tool, error) {
	if session.InitializeResult().Capabilities.Tools == nil {
		return nil, nil
	}
	var listed []*mcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("listing tools: %w", err)
		}
		listed = append(listed, tool)
	}
	return listed, nil
}

// Tools returns each connected server's tools, keyed by server name.
func (m *Manager) Tools() map[string][]*mcp.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string][]*mcp.Tool, len(m.servers))
	for name, s := range m.servers {
		if len(s.tools) > 0 {
			out[name] = s.tools
		}
	}
	return out
}

// CallTool calls a tool on a server. For authenticated servers it acts as
// senderKey and returns ErrNotConnected when that sender has no credential.
func (m *Manager) CallTool(ctx context.Context, serverName, toolName, senderKey string, args map[string]any) (*mcp.CallToolResult, error) {
	m.mu.RLock()
	s, ok := m.servers[serverName]
	closed := m.closed
	m.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("manager is closed")
	}
	if !ok {
		return nil, fmt.Errorf("server %s not found", serverName)
	}
	params := &mcp.CallToolParams{Name: toolName, Arguments: args}
	if s.tokens == nil {
		return s.discovery.CallTool(ctx, params)
	}

	if senderKey == "" {
		return nil, ErrNotConnected
	}
	if _, err := s.tokens(senderKey, ""); err != nil {
		return nil, err
	}
	session, err := s.sessionFor(ctx, senderKey)
	if err != nil {
		return nil, err
	}
	result, err := session.CallTool(ctx, params)
	if err != nil {
		// The session may be closed (the go-sdk closes it after a 401);
		// drop it so the sender's next call reconnects.
		s.dropSession(senderKey, session)
		return nil, err
	}
	return result, nil
}

func (s *server) sessionFor(ctx context.Context, senderKey string) (*mcp.ClientSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session := s.sessions[senderKey]; session != nil {
		return session, nil
	}
	session, err := connect(ctx, s.cfg, &bearerTransport{
		base:      http.DefaultTransport,
		senderKey: senderKey,
		tokens:    s.tokens,
	})
	if err != nil {
		return nil, err
	}
	s.sessions[senderKey] = session
	return session, nil
}

func (s *server) dropSession(senderKey string, session *mcp.ClientSession) {
	s.mu.Lock()
	if s.sessions[senderKey] == session {
		delete(s.sessions, senderKey)
	}
	s.mu.Unlock()
	_ = session.Close()
}

// Close closes all server connections.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	var errs []error
	for name, s := range m.servers {
		s.mu.Lock()
		for _, session := range s.sessions {
			_ = session.Close()
		}
		s.sessions = nil
		s.mu.Unlock()
		if err := s.discovery.Close(); err != nil {
			errs = append(errs, fmt.Errorf("server %s: %w", name, err))
		}
	}
	m.servers = map[string]*server{}
	return errors.Join(errs...)
}

// bearerTransport sets the sender's bearer token on every request. On a 401 it
// asks for a refreshed token once and retries the request.
type bearerTransport struct {
	base      http.RoundTripper
	senderKey string
	tokens    TokenSource
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.tokens(t.senderKey, "")
	if err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(withBearer(req, token))
	if err != nil || resp.StatusCode != http.StatusUnauthorized || req.GetBody == nil {
		return resp, err
	}
	fresh, err := t.tokens(t.senderKey, token)
	if err != nil || fresh == "" || fresh == token {
		return resp, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return resp, nil
	}
	_ = resp.Body.Close()
	retry := withBearer(req, fresh)
	retry.Body = body
	return t.base.RoundTrip(retry)
}

func withBearer(req *http.Request, token string) *http.Request {
	out := req.Clone(req.Context())
	out.Header.Set("Authorization", "Bearer "+token)
	return out
}
