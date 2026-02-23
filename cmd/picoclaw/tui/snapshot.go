package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// VMSnapshot holds the complete runtime state collected from the VM or local host.
type VMSnapshot struct {
	// VM info (from multipass info) — empty in local mode
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
	// Host-side channel state (outside VM), used to explain VM/host drift.
	HostConfigExists bool
	HostDiscord      ChannelSnapshot
	HostTelegram     ChannelSnapshot

	// Service state
	ServiceInstalled bool
	ServiceRunning   bool
	ServiceAutoStart bool

	// Mount state (VM-only)
	Mounts []MountInfo

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
	Proxy     string          `json:"proxy,omitempty"`
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

// CollectSnapshot gathers all state. Safe to call from a goroutine.
func CollectSnapshot(exec Executor) VMSnapshot {
	if exec.Mode() == ModeVM {
		return collectVMSnapshot(exec)
	}
	return collectLocalSnapshot(exec)
}

func collectVMSnapshot(exec Executor) VMSnapshot {
	snap := VMSnapshot{FetchedAt: time.Now()}

	var wg sync.WaitGroup
	var vmInfo VMInfo
	var cfgRaw, authRaw, hostCfgRaw string
	var cfgErr, authErr, hostCfgErr error

	wg.Add(5)
	go func() { defer wg.Done(); vmInfo = GetVMInfo() }()
	go func() { defer wg.Done(); cfgRaw, cfgErr = exec.ReadFile(exec.ConfigPath()) }()
	go func() { defer wg.Done(); authRaw, authErr = exec.ReadFile(exec.AuthPath()) }()
	go func() { defer wg.Done(); hostCfgRaw, hostCfgErr = hostConfigRaw() }()
	go func() { defer wg.Done(); snap.AgentVersion = exec.AgentVersion() }()
	wg.Wait()

	snap.State = vmInfo.State
	snap.IPv4 = vmInfo.IPv4
	snap.Load = vmInfo.Load
	snap.Memory = vmInfo.Memory
	snap.Mounts = vmInfo.Mounts

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

	// Parse host config (outside VM) to surface drift in the UI.
	snap.HostConfigExists = hostCfgErr == nil && strings.TrimSpace(hostCfgRaw) != ""
	var hostCfg configJSON
	if snap.HostConfigExists {
		_ = json.Unmarshal([]byte(hostCfgRaw), &hostCfg)
		snap.HostDiscord = channelState(hostCfg.Channels.Discord)
		snap.HostTelegram = channelState(hostCfg.Channels.Telegram)
	}

	// Workspace.
	snap.WorkspacePath = cfg.Agents.Defaults.Workspace
	if snap.WorkspacePath != "" {
		out, err := exec.ExecShell(3*time.Second, "test -d "+shellEscape(expandHome(snap.WorkspacePath))+" && echo yes || echo no")
		snap.WorkspaceExists = err == nil && strings.TrimSpace(out) == "yes"
	}

	// Provider state.
	snap.OpenAI = providerState(cfg.Providers.OpenAI, auth.Credentials["openai"])
	snap.Anthropic = providerState(cfg.Providers.Anthropic, auth.Credentials["anthropic"])

	// Channel state.
	snap.Discord = channelState(cfg.Channels.Discord)
	snap.Telegram = channelState(cfg.Channels.Telegram)

	// Service state from a single status snapshot.
	snap.ServiceInstalled, snap.ServiceRunning, snap.ServiceAutoStart = collectServiceState(exec)

	return snap
}

func collectLocalSnapshot(exec Executor) VMSnapshot {
	snap := VMSnapshot{
		State:     "Local",
		FetchedAt: time.Now(),
	}

	var wg sync.WaitGroup
	var cfgRaw, authRaw string
	var cfgErr, authErr error

	wg.Add(3)
	go func() { defer wg.Done(); cfgRaw, cfgErr = exec.ReadFile(exec.ConfigPath()) }()
	go func() { defer wg.Done(); authRaw, authErr = exec.ReadFile(exec.AuthPath()) }()
	go func() { defer wg.Done(); snap.AgentVersion = exec.AgentVersion() }()
	wg.Wait()

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
		expanded := expandHomeLocal(snap.WorkspacePath, exec.HomePath())
		if info, err := os.Stat(expanded); err == nil && info.IsDir() {
			snap.WorkspaceExists = true
		}
	}

	// Provider state.
	snap.OpenAI = providerState(cfg.Providers.OpenAI, auth.Credentials["openai"])
	snap.Anthropic = providerState(cfg.Providers.Anthropic, auth.Credentials["anthropic"])

	// Channel state.
	snap.Discord = channelState(cfg.Channels.Discord)
	snap.Telegram = channelState(cfg.Channels.Telegram)

	// Service state from a single status snapshot.
	snap.ServiceInstalled, snap.ServiceRunning, snap.ServiceAutoStart = collectServiceState(exec)

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

func collectServiceState(exec Executor) (installed, running, autoStart bool) {
	cmd := "HOME=" + exec.HomePath() + " sciclaw service status 2>&1"
	out, err := exec.ExecShell(8*time.Second, cmd)
	if err != nil {
		installed = exec.ServiceInstalled()
		running = exec.ServiceActive()
		autoStart = installed
		return installed, running, autoStart
	}

	parsedInstalled, hasInstalled := parseServiceStatusFlag(out, "installed")
	parsedRunning, hasRunning := parseServiceStatusFlag(out, "running")
	parsedEnabled, hasEnabled := parseServiceStatusFlag(out, "enabled")

	if hasInstalled {
		installed = parsedInstalled
	} else {
		installed = exec.ServiceInstalled()
	}

	if hasRunning {
		running = parsedRunning
	} else {
		running = exec.ServiceActive()
	}

	if hasEnabled {
		autoStart = parsedEnabled
	} else {
		autoStart = installed
	}

	return installed, running, autoStart
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

func expandHomeLocal(path, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func hostConfigRaw() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".picoclaw", "config.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SuggestedStep returns a plain-English suggestion and the tab index to navigate to.
func (s *VMSnapshot) SuggestedStep() (message, detail string, tabIdx int) {
	// VM-specific states — only reachable when running in VM mode.
	if s.State == "NotFound" || s.State == "" {
		return "Create and start the VM", "Run 'sciclaw vm start' to provision your virtual machine.", -1
	}
	if s.State != "Running" && s.State != "Local" {
		return "Start the VM", "The VM exists but is not running. Run 'sciclaw vm start'.", -1
	}

	// Mode-neutral suggestions from here on.
	if !s.ConfigExists {
		return "Create configuration file", "Your config file is missing. Run the setup wizard or go to Settings.", tabSettings
	}
	if !s.WorkspaceExists && s.WorkspacePath != "" {
		return "Create workspace folder", "The workspace directory does not exist yet.", tabSettings
	}
	if s.OpenAI != "ready" && s.Anthropic != "ready" {
		return "Log in to an AI provider", "You need credentials for OpenAI or Anthropic to use the agent.", tabLogin
	}
	if s.Discord.Status != "ready" && s.Telegram.Status != "ready" {
		return "Set up a channel", "Connect Discord or Telegram so you can chat with your agent.", tabChannels
	}
	if !s.ServiceInstalled {
		return "Install the gateway service", "The background service lets your gateway run continuously.", tabAgent
	}
	if !s.ServiceRunning {
		return "Start the gateway service", "The gateway service is installed but not running yet.", tabAgent
	}
	return "You're all set!", "Your gateway is running and ready. Check the logs for activity.", tabAgent
}
