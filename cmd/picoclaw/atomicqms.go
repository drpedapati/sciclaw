package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/mcp"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// The connect handshake spans two processes. The gateway receives the chat
// command and asks `sciclaw web` over loopback (POST /atomicqms/callback) to
// begin; the web process keeps the pending state and PKCE verifier in memory,
// receives Gitea's redirect (GET /atomicqms/callback), exchanges the code and
// writes the token to auth.json, which the gateway reads at tool-call time.

const (
	atomicQMSReplyUsage          = "Usage: /atomicqms connect|status|disconnect"
	atomicQMSReplyDMSent         = "I sent you a direct message."
	atomicQMSReplyDMFailed       = "I couldn't send you a direct message."
	atomicQMSReplyUnavailable    = "AtomicQMS connect is unavailable right now."
	atomicQMSReplyNotConnected   = "Not connected to AtomicQMS."
	atomicQMSReplyDisconnected   = "Disconnected from AtomicQMS."
	atomicQMSReplyConnectedAs    = "Connected to AtomicQMS as %s."
	atomicQMSDirectLink          = "Connect AtomicQMS: %s"
	atomicQMSPageConnected       = "AtomicQMS connected."
	atomicQMSPageFailed          = "AtomicQMS connection failed."
	atomicQMSToolNotConnectedLLM = "This user has not connected AtomicQMS. Tell them to run /atomicqms connect, then try again."
)

// directMessenger is implemented by channels that can message a user
// privately (Discord, Telegram).
type directMessenger interface {
	SendDirect(ctx context.Context, userID, content string) error
}

type atomicQMSCommands struct {
	cfg    config.AtomicQMSConfig
	client *http.Client
	direct func(channel string) (directMessenger, bool)
}

func newAtomicQMSCommands(cfg config.AtomicQMSConfig, channelManager *channels.Manager) *atomicQMSCommands {
	return &atomicQMSCommands{
		cfg:    cfg,
		client: &http.Client{Timeout: 10 * time.Second},
		direct: func(channel string) (directMessenger, bool) {
			ch, ok := channelManager.GetChannel(channel)
			if !ok {
				return nil, false
			}
			dm, ok := ch.(directMessenger)
			return dm, ok
		},
	}
}

// atomicQMSSenderKey is the per-user identity the agent loop passes to tools
// (processOptions.SenderID) and per-user profiles use.
func atomicQMSSenderKey(channel, senderID string) string {
	return channel + ":" + senderID
}

// handle runs one /atomicqms action and returns the line to show in the chat
// the command came from, or "" when the reply went by direct message.
func (a *atomicQMSCommands) handle(ctx context.Context, channel, senderID string, inDM bool, action string) string {
	senderKey := atomicQMSSenderKey(channel, senderID)
	switch action {
	case "connect":
		authURL, err := a.begin(ctx, channel, senderID)
		if err != nil {
			logger.WarnCF("atomicqms", "connect begin failed", map[string]interface{}{
				"channel": channel,
				"error":   err.Error(),
			})
			return atomicQMSReplyUnavailable
		}
		dm, ok := a.direct(channel)
		if !ok {
			return atomicQMSReplyDMFailed
		}
		if err := dm.SendDirect(ctx, senderID, fmt.Sprintf(atomicQMSDirectLink, authURL)); err != nil {
			logger.WarnCF("atomicqms", "connect direct message failed", map[string]interface{}{
				"channel": channel,
				"error":   err.Error(),
			})
			return atomicQMSReplyDMFailed
		}
		if inDM {
			return ""
		}
		return atomicQMSReplyDMSent
	case "status":
		cred, err := auth.GetCredential(auth.AtomicQMSCredentialKey(senderKey))
		if err != nil || cred == nil || cred.AccessToken == "" {
			return atomicQMSReplyNotConnected
		}
		return fmt.Sprintf(atomicQMSReplyConnectedAs, cred.AccountID)
	case "disconnect":
		if err := auth.DeleteCredential(auth.AtomicQMSCredentialKey(senderKey)); err != nil {
			return atomicQMSReplyUnavailable
		}
		return atomicQMSReplyDisconnected
	default:
		return atomicQMSReplyUsage
	}
}

type atomicQMSBeginRequest struct {
	Channel  string `json:"channel"`
	SenderID string `json:"sender_id"`
}

type atomicQMSBeginResponse struct {
	URL string `json:"url"`
}

func (a *atomicQMSCommands) begin(ctx context.Context, channel, senderID string) (string, error) {
	body, _ := json.Marshal(atomicQMSBeginRequest{Channel: channel, SenderID: senderID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.EffectiveWebURL()+config.AtomicQMSCallbackPath, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("web server returned %d", resp.StatusCode)
	}
	var out atomicQMSBeginResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.URL == "" {
		return "", fmt.Errorf("web server returned no URL")
	}
	return out.URL, nil
}

// chatHandler answers a typed /atomicqms command in the chat it came from.
func (a *atomicQMSCommands) chatHandler(msgBus *bus.MessageBus) routing.CommandHandler {
	return func(ctx context.Context, msg bus.InboundMessage, action string) {
		inDM := strings.EqualFold(strings.TrimSpace(msg.Metadata["is_dm"]), "true")
		reply := a.handle(ctx, msg.Channel, msg.SenderID, inDM, action)
		if reply == "" {
			return
		}
		_ = msgBus.PublishOutbound(ctx, bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: reply,
		})
	}
}

// handleAtomicQMSCallback serves config.AtomicQMSCallbackPath. GET is Gitea's
// redirect after consent. POST begins a connect and is accepted only from the
// gateway over loopback, never through a proxy.
func (s *webServer) handleAtomicQMSCallback(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.completeAtomicQMS(w, r)
	case http.MethodPost:
		s.beginAtomicQMS(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *webServer) beginAtomicQMS(w http.ResponseWriter, r *http.Request) {
	if !directLoopbackRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req atomicQMSBeginRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil ||
		strings.TrimSpace(req.Channel) == "" || strings.TrimSpace(req.SenderID) == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	cfg, err := loadConfig()
	if err != nil || !cfg.AtomicQMS.Configured() {
		http.Error(w, "atomicqms is not configured", http.StatusServiceUnavailable)
		return
	}
	oauthCfg := auth.AtomicQMSOAuthConfig(cfg.AtomicQMS.BaseURL, cfg.AtomicQMS.ClientID)
	authURL, err := s.atomicQMSStates.Begin(oauthCfg, cfg.AtomicQMS.EffectiveRedirectURL(), atomicQMSSenderKey(req.Channel, req.SenderID))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	jsonResp(w, atomicQMSBeginResponse{URL: authURL})
}

func (s *webServer) completeAtomicQMS(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	_, err := s.atomicQMSStates.Complete(q.Get("state"), q.Get("code"))
	status, line := http.StatusOK, atomicQMSPageConnected
	if err != nil {
		logger.WarnCF("atomicqms", "callback rejected", map[string]interface{}{"error": err.Error()})
		status, line = http.StatusBadRequest, atomicQMSPageFailed
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>AtomicQMS</title></head><body style="font-family:system-ui,sans-serif;display:flex;min-height:90vh;align-items:center;justify-content:center"><p>%s</p></body></html>`, html.EscapeString(line))
}

// directLoopbackRequest reports whether r came straight from this host rather
// than through a reverse proxy.
func directLoopbackRequest(r *http.Request) bool {
	for _, h := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// newMCPManager connects to the configured MCP servers. It returns nil when
// MCP is disabled or no server connected.
func newMCPManager(ctx context.Context, cfg *config.Config) *mcp.Manager {
	if !cfg.Tools.MCP.Enabled {
		return nil
	}
	atomicQMS := auth.AtomicQMSOAuthConfig(cfg.AtomicQMS.BaseURL, cfg.AtomicQMS.ClientID)
	manager := mcp.NewManager(map[string]mcp.TokenSource{
		mcp.AuthAtomicQMS: func(senderKey, rejected string) (string, error) {
			token, err := auth.AtomicQMSAccessToken(atomicQMS, senderKey, rejected)
			if errors.Is(err, auth.ErrAtomicQMSNotConnected) {
				return "", mcp.ErrNotConnected
			}
			return token, err
		},
	})
	if err := manager.LoadFromConfig(ctx, cfg.Tools.MCP); err != nil {
		logger.WarnCF("mcp", "MCP tools unavailable", map[string]interface{}{"error": err.Error()})
		_ = manager.Close()
		return nil
	}
	return manager
}

// attachMCPTools registers every MCP tool on al.
func attachMCPTools(al *agent.AgentLoop, manager *mcp.Manager, cfg config.MCPConfig) {
	if al == nil || manager == nil {
		return
	}
	for serverName, serverTools := range manager.Tools() {
		for _, t := range serverTools {
			tool := tools.NewMCPTool(manager, serverName, t)
			if cfg.Servers[serverName].Auth == mcp.AuthAtomicQMS {
				tool.NotConnectedMessage = atomicQMSToolNotConnectedLLM
			}
			al.RegisterTool(tool)
		}
	}
}
