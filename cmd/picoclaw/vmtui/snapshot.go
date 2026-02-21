package vmtui

import (
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// VMSnapshot holds the complete runtime state collected from the VM.
type VMSnapshot struct {
	// VM info (from multipass info)
	State  string
	IPv4   string
	Load   string
	Memory string

	// Agent version
	AgentVersion string

	// Config/workspace state
	ConfigExists    bool
	WorkspaceExists bool
	AuthStoreExists bool
	WorkspacePath   string

	// Provider state
	OpenAI    string // "ready" or "missing"
	Anthropic string // "ready" or "missing"

	// Channel state
	Discord  ChannelSnapshot
	Telegram ChannelSnapshot

	// Service state
	ServiceInstalled bool
	ServiceRunning   bool

	// Timestamp
	FetchedAt time.Time
}

// ChannelSnapshot captures a channel's state including parsed approved users.
type ChannelSnapshot struct {
	Status        string         // "ready", "open", "broken", "off"
	Enabled       bool           //
	HasToken      bool           //
	ApprovedUsers []ApprovedUser // parsed from config
}

// configJSON mirrors just the fields we need from config.json.
type configJSON struct {
	Agents struct {
		Defaults struct {
			Workspace string `json:"workspace"`
		} `json:"defaults"`
	} `json:"agents"`
	Providers struct {
		OpenAI    providerJSON `json:"openai"`
		Anthropic providerJSON `json:"anthropic"`
	} `json:"providers"`
	Channels struct {
		Discord  channelJSON `json:"discord"`
		Telegram channelJSON `json:"telegram"`
	} `json:"channels"`
}

type providerJSON struct {
	APIKey     string `json:"api_key"`
	AuthMethod string `json:"auth_method"`
}

type channelJSON struct {
	Enabled   bool            `json:"enabled"`
	Token     string          `json:"token"`
	AllowFrom flexStringSlice `json:"allow_from"`
}

// flexStringSlice handles JSON values that are either a string or an array of strings.
type flexStringSlice []string

func (f *flexStringSlice) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if s != "" {
			*f = []string{s}
		}
		return nil
	}
	return nil
}

// authJSON mirrors just the fields we need from auth.json.
type authJSON struct {
	Credentials map[string]authCredJSON `json:"credentials"`
}

type authCredJSON struct {
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
	APIKey      string `json:"api_key"`
}

// CollectSnapshot gathers all VM state. Safe to call from a goroutine.
func CollectSnapshot() VMSnapshot {
	snap := VMSnapshot{FetchedAt: time.Now()}

	// Fetch VM info and in-VM data in parallel.
	var wg sync.WaitGroup
	var vmInfo VMInfo
	var cfgRaw, authRaw string
	var cfgErr, authErr error

	wg.Add(4)
	go func() { defer wg.Done(); vmInfo = GetVMInfo() }()
	go func() { defer wg.Done(); cfgRaw, cfgErr = VMCatFile("/home/ubuntu/.picoclaw/config.json") }()
	go func() { defer wg.Done(); authRaw, authErr = VMCatFile("/home/ubuntu/.picoclaw/auth.json") }()
	go func() { defer wg.Done(); snap.AgentVersion = VMAgentVersion() }()
	wg.Wait()

	snap.State = vmInfo.State
	snap.IPv4 = vmInfo.IPv4
	snap.Load = vmInfo.Load
	snap.Memory = vmInfo.Memory

	if snap.State != "Running" {
		return snap
	}

	// Parse config.
	snap.ConfigExists = cfgErr == nil && strings.TrimSpace(cfgRaw) != ""
	var cfg configJSON
	if snap.ConfigExists {
		_ = json.Unmarshal([]byte(cfgRaw), &cfg)
	}

	// Parse auth.
	snap.AuthStoreExists = authErr == nil && strings.TrimSpace(authRaw) != ""
	var auth authJSON
	if snap.AuthStoreExists {
		_ = json.Unmarshal([]byte(authRaw), &auth)
	}

	// Workspace.
	snap.WorkspacePath = cfg.Agents.Defaults.Workspace
	if snap.WorkspacePath != "" {
		out, err := VMExecShell(3*time.Second, "test -d "+shellEscape(expandHome(snap.WorkspacePath))+" && echo yes || echo no")
		snap.WorkspaceExists = err == nil && strings.TrimSpace(out) == "yes"
	}

	// Provider state.
	snap.OpenAI = providerState(cfg.Providers.OpenAI, auth.Credentials["openai"])
	snap.Anthropic = providerState(cfg.Providers.Anthropic, auth.Credentials["anthropic"])

	// Channel state.
	snap.Discord = channelState(cfg.Channels.Discord)
	snap.Telegram = channelState(cfg.Channels.Telegram)

	// Service state (parallel).
	var wg2 sync.WaitGroup
	wg2.Add(2)
	go func() { defer wg2.Done(); snap.ServiceInstalled = VMServiceInstalled() }()
	go func() { defer wg2.Done(); snap.ServiceRunning = VMServiceActive() }()
	wg2.Wait()

	return snap
}

func providerState(prov providerJSON, cred authCredJSON) string {
	if strings.TrimSpace(prov.APIKey) != "" {
		return "ready"
	}
	if strings.TrimSpace(cred.AccessToken) != "" || strings.TrimSpace(cred.Token) != "" || strings.TrimSpace(cred.APIKey) != "" {
		return "ready"
	}
	return "missing"
}

func channelState(ch channelJSON) ChannelSnapshot {
	users := make([]ApprovedUser, 0, len(ch.AllowFrom))
	for _, entry := range ch.AllowFrom {
		users = append(users, ParseApprovedUser(entry))
	}

	hasToken := strings.TrimSpace(ch.Token) != ""
	var status string
	switch {
	case ch.Enabled && hasToken && len(users) > 0:
		status = "ready"
	case ch.Enabled && hasToken && len(users) == 0:
		status = "open"
	case ch.Enabled && !hasToken:
		status = "broken"
	default:
		status = "off"
	}
	return ChannelSnapshot{
		Status:        status,
		Enabled:       ch.Enabled,
		HasToken:      hasToken,
		ApprovedUsers: users,
	}
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		return "/home/ubuntu/" + path[2:]
	}
	return path
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// SuggestedStep returns a plain-English suggestion and the tab index to navigate to.
func (s *VMSnapshot) SuggestedStep() (message, detail string, tabIdx int) {
	if s.State == "NotFound" || s.State == "" {
		return "Create and start the VM", "Run 'sciclaw vm start' to provision your virtual machine.", -1
	}
	if s.State != "Running" {
		return "Start the VM", "The VM exists but is not running. Run 'sciclaw vm start'.", -1
	}
	if s.OpenAI != "ready" && s.Anthropic != "ready" {
		return "Log in to an AI provider", "You need credentials for OpenAI or Anthropic to use the agent.", 2
	}
	if s.Discord.Status != "ready" && s.Telegram.Status != "ready" {
		return "Set up a messaging app", "Connect Discord or Telegram so you can chat with your agent.", 1
	}
	if !s.ServiceInstalled {
		return "Install the agent service", "The background service lets your agent run continuously.", 3
	}
	if !s.ServiceRunning {
		return "Start the agent service", "Your agent is installed but not running yet.", 3
	}
	return "You're all set!", "Your agent is running and ready. Check the logs for activity.", 3
}
