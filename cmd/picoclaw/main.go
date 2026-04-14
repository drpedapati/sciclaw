// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/sipeed/picoclaw/cmd/picoclaw/tui"
	"github.com/sipeed/picoclaw/pkg/addons"
	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/hardware"
	"github.com/sipeed/picoclaw/pkg/heartbeat"
	"github.com/sipeed/picoclaw/pkg/irl"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/migrate"
	"github.com/sipeed/picoclaw/pkg/models"
	"github.com/sipeed/picoclaw/pkg/phi"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/profile"
	svcmgr "github.com/sipeed/picoclaw/pkg/service"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/tools"
	"github.com/sipeed/picoclaw/pkg/voice"
	"github.com/sipeed/picoclaw/pkg/workspacetpl"
)

var (
	version   = "dev"
	buildTime string
	goVersion string
)

var pgrepGatewayPIDs = func() ([]byte, error) {
	return exec.Command("pgrep", "-f", "(sciclaw|picoclaw)[[:space:]]+gateway([[:space:]]|$)").Output()
}

func init() {
	// Strip leading "v" set by ldflags so format strings can add their own.
	version = strings.TrimPrefix(version, "v")
	agent.Version = version
	tui.Version = version
}

const logo = "🔬"
const displayName = "sciClaw"
const cliName = "picoclaw"
const primaryCLIName = "sciclaw"

const docsURLBase = "https://drpedapati.github.io/sciclaw/docs.html"

var baselineScienceSkillNames = []string{
	"scientific-writing",
	"pubmed-cli",
	"biorxiv-database",
	"quarto-authoring",
	"pandoc-docx",
	"imagemagick",
	"beautiful-mermaid",
	"explainer-site",
	"experiment-provenance",
	"benchmark-logging",
	"humanize-text",
	"docx-review",
	"pptx",
	"pdf",
	"acroform-fill",
	"xlsx",
}

func invokedCLIName() string {
	if len(os.Args) == 0 {
		return primaryCLIName
	}
	base := strings.ToLower(filepath.Base(os.Args[0]))
	if strings.HasPrefix(base, primaryCLIName) {
		return primaryCLIName
	}
	if strings.HasPrefix(base, cliName) {
		return cliName
	}
	return primaryCLIName
}

func printVersion() {
	fmt.Printf("%s %s (%s; %s-compatible) v%s\n", logo, displayName, primaryCLIName, cliName, version)
	if buildTime != "" {
		fmt.Printf("  Build: %s\n", buildTime)
	}
	goVer := goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	if goVer != "" {
		fmt.Printf("  Go: %s\n", goVer)
	}
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]
	if shouldOfferConfigHealthRepair(command) {
		if err := maybeOfferConfigHealthRepair(); err != nil {
			fmt.Printf("Warning: config health check failed: %v\n", err)
		}
	}

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "gateway":
		gatewayCmd()
	case "service":
		serviceCmd()
	case "app", "tui":
		tuiCmd()
	case "web":
		webCmd()
	case "vm":
		vmCmd()
	case "docker":
		dockerCmd()
	case "channels":
		channelsCmd()
	case "status":
		statusCmd()
	case "doctor":
		doctorCmd()
	case "migrate":
		migrateCmd()
	case "auth":
		authCmd()
	case "cron":
		cronCmd()
	case "routing":
		routingCmd()
	case "addon", "addons":
		addonCmd()
	case "modes":
		modesCmd()
	case "models":
		modelsCmd()
	case "skills":
		if len(os.Args) < 3 {
			skillsHelp()
			return
		}

		subcommand := os.Args[2]

		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		workspace := cfg.WorkspacePath()
		installer := skills.NewSkillInstaller(workspace)
		globalDir := filepath.Dir(getConfigPath())
		globalSkillsDir := filepath.Join(globalDir, "skills")
		builtinSkillsDir := resolveBuiltinSkillsDir(workspace)
		skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

		switch subcommand {
		case "list":
			skillsListCmd(skillsLoader)
		case "install":
			skillsInstallCmd(installer)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Printf("Usage: %s skills remove <skill-name>\n", invokedCLIName())
				return
			}
			skillsRemoveCmd(installer, os.Args[3])
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "list-builtin":
			skillsListBuiltinCmd()
		case "search":
			skillsSearchCmd(installer)
		case "show":
			if len(os.Args) < 4 {
				fmt.Printf("Usage: %s skills show <skill-name>\n", invokedCLIName())
				return
			}
			skillsShowCmd(skillsLoader, os.Args[3])
		default:
			fmt.Printf("Unknown skills command: %s\n", subcommand)
			skillsHelp()
		}
	case "backup":
		backupCmd()
	case "archive":
		archiveCmd()
	case "prompt":
		promptCmd()
	case "version", "--version", "-v":
		printVersion()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func shouldOfferConfigHealthRepair(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "app", "tui", "gateway", "service", "status", "agent":
		return true
	default:
		return false
	}
}

type configHealthIssues struct {
	discordMentionMismatch []int
	unmappedBehaviorLegacy bool
	discordAllowlistEmpty  bool
	suggestedDiscordUsers  []string
}

func maybeOfferConfigHealthRepair() error {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return nil
	}

	configPath := getConfigPath()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	issues := detectConfigHealthIssues(cfg)
	if !issues.hasAny() {
		return nil
	}

	fmt.Println()
	fmt.Println("Config health check found settings that can cause unexpected Discord replies.")
	r := bufio.NewReader(os.Stdin)

	changed := false
	backupPath := ""
	ensureBackup := func() error {
		if backupPath != "" {
			return nil
		}
		var err error
		backupPath, err = backupFile(configPath)
		if err != nil {
			return fmt.Errorf("backup config: %w", err)
		}
		return nil
	}

	if len(issues.discordMentionMismatch) > 0 {
		fmt.Printf("- %d routed Discord mapping(s) are set to reply without @mention.\n", len(issues.discordMentionMismatch))
		if promptYesNo(r, "  Fix now by requiring @mention in those mappings?", true) {
			if err := ensureBackup(); err != nil {
				return err
			}
			if applyRoutingMentionRequired(cfg, issues.discordMentionMismatch) > 0 {
				changed = true
			}
		}
	}

	if issues.unmappedBehaviorLegacy {
		fmt.Println("- Unmapped behavior is set to full default fallback (unmapped rooms can reply without an explicit sciClaw mention).")
		if promptYesNo(r, "  Set unmapped behavior to mention_only now?", true) {
			if err := ensureBackup(); err != nil {
				return err
			}
			cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorMentionOnly
			changed = true
		}
	}

	if issues.discordAllowlistEmpty {
		fmt.Println("- Discord channel allowlist is empty (channel ingress currently allows any sender).")
		if len(issues.suggestedDiscordUsers) > 0 {
			fmt.Printf("  Suggested allowlist from routing mappings: %s\n", strings.Join(issues.suggestedDiscordUsers, ", "))
		}
		if promptYesNo(r, "  Set Discord allowlist from routed mapping users now?", false) {
			if err := ensureBackup(); err != nil {
				return err
			}
			cfg.Channels.Discord.AllowFrom = config.FlexibleStringSlice(issues.suggestedDiscordUsers)
			changed = true
		}
	}

	if !changed {
		fmt.Println()
		return nil
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	if err := requestRoutingReload(); err != nil {
		logger.WarnCF("routing", "failed to trigger routing reload marker", map[string]any{"error": err.Error()})
	}

	fmt.Println("Applied selected config fixes.")
	if backupPath != "" {
		fmt.Printf("Backup saved to: %s\n", backupPath)
	}
	fmt.Println()
	return nil
}

func (i configHealthIssues) hasAny() bool {
	return len(i.discordMentionMismatch) > 0 || i.unmappedBehaviorLegacy || i.discordAllowlistEmpty
}

func detectConfigHealthIssues(cfg *config.Config) configHealthIssues {
	issues := configHealthIssues{
		discordMentionMismatch: routingMentionMismatchIndexes(cfg),
	}
	if cfg == nil {
		return issues
	}

	hasRouting := cfg.Routing.Enabled && len(cfg.Routing.Mappings) > 0
	if hasRouting && strings.TrimSpace(cfg.Routing.UnmappedBehavior) == config.RoutingUnmappedBehaviorDefault {
		issues.unmappedBehaviorLegacy = true
	}

	hasDiscordRouting := false
	for _, m := range cfg.Routing.Mappings {
		if strings.EqualFold(strings.TrimSpace(m.Channel), "discord") {
			hasDiscordRouting = true
			break
		}
	}
	if hasRouting && hasDiscordRouting && cfg.Channels.Discord.Enabled && len(cfg.Channels.Discord.AllowFrom) == 0 {
		issues.discordAllowlistEmpty = true
		issues.suggestedDiscordUsers = collectDiscordRoutingSenders(cfg)
	}

	return issues
}

func collectDiscordRoutingSenders(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, m := range cfg.Routing.Mappings {
		if !strings.EqualFold(strings.TrimSpace(m.Channel), "discord") {
			continue
		}
		for _, sender := range m.AllowedSenders {
			v := strings.TrimSpace(sender)
			if v == "" {
				continue
			}
			if _, ok := seen[v]; ok {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

func routingMentionMismatchIndexes(cfg *config.Config) []int {
	if cfg == nil {
		return nil
	}
	out := make([]int, 0)
	for i, m := range cfg.Routing.Mappings {
		if strings.ToLower(strings.TrimSpace(m.Channel)) != "discord" {
			continue
		}
		if m.RequireMention != nil && !*m.RequireMention {
			out = append(out, i)
		}
	}
	return out
}

func applyRoutingMentionRequired(cfg *config.Config, indexes []int) int {
	if cfg == nil || len(indexes) == 0 {
		return 0
	}
	t := true
	updated := 0
	for _, idx := range indexes {
		if idx < 0 || idx >= len(cfg.Routing.Mappings) {
			continue
		}
		cfg.Routing.Mappings[idx].RequireMention = &t
		updated++
	}
	return updated
}

func requestRoutingReload() error {
	return requestRoutingReloadAt(getConfigPath())
}

func requestRoutingReloadAt(configPath string) error {
	triggerPath := filepath.Join(filepath.Dir(configPath), "routing.reload")
	if err := os.MkdirAll(filepath.Dir(triggerPath), 0o755); err != nil {
		return err
	}
	payload := []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	return os.WriteFile(triggerPath, payload, 0o600)
}

func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func printHelp() {
	commandName := invokedCLIName()
	fmt.Printf("%s %s - Paired Scientist Assistant v%s\n\n", logo, displayName, version)
	fmt.Printf("Primary command: %s\n", primaryCLIName)
	fmt.Printf("Compatibility alias: %s\n\n", cliName)
	fmt.Printf("Usage: %s <command>\n", commandName)
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  app         Open the graphical dashboard (alias: tui)")
	fmt.Println("  onboard     Initialize sciClaw configuration and workspace")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  modes       Manage inference mode (cloud, phi, vm)")
	fmt.Println("  models      Manage models (list, set, effort, status)")
	fmt.Println("  auth        Manage authentication (login, import-op, logout, status)")
	fmt.Println("  gateway     Start sciClaw gateway in foreground (debug/containers)")
	fmt.Println("  web         Start the sciClaw web UI server")
	fmt.Println("  service     Manage background gateway/web services (launchd/systemd, recommended)")
	fmt.Println("  vm          Manage a Multipass sciClaw VM (no repo checkout required)")
	fmt.Println("  docker      Convenience wrapper for sciClaw container workflows")
	fmt.Println("  channels    Setup and manage chat channels (Telegram, Discord, etc.)")
	fmt.Println("  status      Show sciClaw status")
	fmt.Println("  doctor      Check deployment health and dependencies")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  routing     Manage channel->workspace routing and ACLs")
	fmt.Println("  addon       Install and manage sciClaw addons (webtop, jupyter, etc.)")
	fmt.Println("  migrate     Migrate from OpenClaw to sciClaw")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  backup      Backup key sciClaw config/workspace files")
	fmt.Println("  archive     Manage Discord archive/recall memory")
	fmt.Println("  prompt      Inspect assembled prompt weight for a saved session")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Println("Agent flags:")
	fmt.Println("  --model <model>   Override model for this invocation")
	fmt.Println("  --effort <level>  Set GPT-5.x reasoning effort (none/minimal/low/medium/high/xhigh)")
	fmt.Println("  -m <message>      Send a single message (non-interactive)")
	fmt.Println("  -s <session>      Use a specific session key")
}

func onboard() {
	args := os.Args[2:]
	yes, force, showHelp, err := parseOnboardOptions(args)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		onboardHelp()
		os.Exit(2)
	}
	if showHelp {
		onboardHelp()
		return
	}

	configPath := getConfigPath()
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		fmt.Printf("Error creating config dir: %v\n", err)
		os.Exit(1)
	}

	exists := false
	if _, err := os.Stat(configPath); err == nil {
		exists = true
	}

	var cfg *config.Config
	switch {
	case exists && !force:
		// Idempotent default: never overwrite existing config (which may contain credentials).
		cfg, err = config.LoadConfig(configPath)
		if err != nil {
			fmt.Printf("Error loading existing config at %s: %v\n", configPath, err)
			fmt.Printf("Fix the JSON (or move it aside) then re-run: %s onboard\n", invokedCLIName())
			os.Exit(1)
		}
		fmt.Printf("Config already exists at %s (preserving credentials)\n", configPath)
		fmt.Printf("Reset to defaults (DANGEROUS): %s onboard --force\n", invokedCLIName())
	case exists && force:
		backupPath, berr := backupFile(configPath)
		if berr != nil {
			fmt.Printf("Error backing up existing config: %v\n", berr)
			os.Exit(1)
		}
		cfg = config.DefaultConfig()
		if err := config.SaveConfig(configPath, cfg); err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Reset config to defaults at %s\n", configPath)
		if backupPath != "" {
			fmt.Printf("Backup written to %s\n", backupPath)
		}
	default:
		cfg = config.DefaultConfig()
		if err := config.SaveConfig(configPath, cfg); err != nil {
			fmt.Printf("Error saving config: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Created config at %s\n", configPath)
	}

	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)
	os.MkdirAll(filepath.Join(workspace, "memory"), 0755)
	os.MkdirAll(filepath.Join(workspace, "skills"), 0755)

	createWorkspaceTemplates(workspace)

	if runtime.GOOS == "linux" {
		fmt.Println("  Preparing managed Python environment for scientific workflows...")
		if venvBin, err := ensureWorkspacePythonEnvironment(workspace); err != nil {
			fmt.Printf("  Python setup warning: %v\n", err)
			fmt.Printf("  You can retry with: %s doctor --fix\n", invokedCLIName())
		} else {
			fmt.Printf("  Python venv ready: %s\n", venvBin)
		}
	}

	if !yes {
		runOnboardWizard(cfg, configPath)
		// Reload to reflect any wizard edits.
		if cfg2, err := config.LoadConfig(configPath); err == nil && cfg2 != nil {
			cfg = cfg2
		}
	}

	fmt.Printf("%s %s is ready!\n", logo, displayName)
	fmt.Println("\nDocs:")
	fmt.Println(" ", docsLink("#scientist-setup"))
	fmt.Println(" ", docsLink("#authentication"))
	fmt.Println(" ", docsLink("#telegram"))
	fmt.Println(" ", docsLink("#discord"))
	fmt.Println(" ", docsLink("#doctor"))

	fmt.Println("\nNext steps:")
	step := 1
	defaultProvider := strings.ToLower(models.ResolveProvider(cfg.Agents.Defaults.Model, cfg))
	if method, ok := detectProviderAuth(defaultProvider, cfg); ok {
		fmt.Printf("  %d. Authentication already configured for %s (%s)\n", step, defaultProvider, method)
	} else if method != "" {
		fmt.Printf("  %d. Re-authenticate %s (%s): %s auth login --provider %s\n", step, defaultProvider, method, invokedCLIName(), defaultProvider)
	} else if defaultProvider == "openai" || defaultProvider == "anthropic" {
		fmt.Printf("  %d. Authenticate (recommended): %s auth login --provider %s\n", step, invokedCLIName(), defaultProvider)
		fmt.Printf("     Or edit: %s\n", configPath)
	} else {
		fmt.Printf("  %d. Configure provider credentials in: %s\n", step, configPath)
	}
	step++

	channelsReady := configuredChatChannels(cfg)
	if len(channelsReady) == 0 {
		fmt.Printf("  %d. Pair a chat app: %s channels setup telegram\n", step, invokedCLIName())
	} else {
		fmt.Printf("  %d. Chat app already configured: %s\n", step, strings.Join(channelsReady, ", "))
		if hasAnyWeakAllowlist(cfg) {
			fmt.Println("     Warning: one or more channels have an empty allow_from list.")
		}
	}
	step++
	fmt.Printf("  %d. Start gateway: %s gateway\n", step, invokedCLIName())
	fmt.Println("\nCompanion tools:")
	if runtime.GOOS == "linux" {
		fmt.Println("  If you installed via Homebrew, Quarto, ImageMagick, IRL, ripgrep, docx-review, xlsx-review, pptx-review, and pubmed-cli are installed automatically.")
	} else {
		fmt.Println("  If you installed via Homebrew, ImageMagick, IRL, ripgrep, docx-review, xlsx-review, pptx-review, and pubmed-cli are installed automatically.")
		fmt.Println("  Install Quarto with: brew install --cask quarto")
	}
	fmt.Println("  Optional: brew install pdf-form-filler  # inspect/schema/fill AcroForm PDFs")
	if strings.TrimSpace(cfg.Tools.PubMed.APIKey) == "" {
		fmt.Println("  Optional: export NCBI_API_KEY=\"your-key\"  # PubMed rate limit: 3/s -> 10/s")
	}
}

func runOnboardWizard(cfg *config.Config, configPath string) {
	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("Setup wizard:")

	// 1. Authentication (most important — nothing works without it)
	defaultProvider := strings.ToLower(models.ResolveProvider(cfg.Agents.Defaults.Model, cfg))
	authOK := false
	if method, ok := detectProviderAuth(defaultProvider, cfg); ok {
		fmt.Printf("  Authentication already configured for %s (%s).\n", defaultProvider, method)
		authOK = true
	} else {
		fmt.Printf("  Help: %s\n", docsLink("#authentication"))
		if promptYesNo(r, "Login to OpenAI now using device code (recommended)?", true) {
			if err := onboardAuthLoginOpenAI(); err != nil {
				fmt.Printf("  Login failed: %v\n", err)
			} else {
				authOK = true
			}
		} else {
			fmt.Println("  Skipped. Configure another provider in the docs:")
			fmt.Printf("    %s\n", docsLink("#authentication"))
		}
	}

	// 2. Smoke test (automatic if auth is configured)
	if authOK {
		fmt.Println("  Running smoke test...")
		msg := "Smoke test: reply with ONE short sentence (max 12 words) confirming you're ready as my paired-scientist. No tool calls."
		if err := runSelfAgentOneShot(msg); err != nil {
			fmt.Printf("  Smoke test failed: %v\n", err)
		}
	}

	// 3. PubMed API key
	if strings.TrimSpace(cfg.Tools.PubMed.APIKey) == "" {
		if promptYesNo(r, "Set an NCBI API key for faster PubMed searches?", false) {
			key := promptLine(r, "Paste NCBI_API_KEY (leave blank to skip):")
			key = strings.TrimSpace(key)
			if key != "" {
				cfg.Tools.PubMed.APIKey = key
				if err := config.SaveConfig(configPath, cfg); err != nil {
					fmt.Printf("  Warning: could not save PubMed API key: %v\n", err)
				} else {
					fmt.Println("  Saved PubMed API key to config.")
				}
			} else {
				fmt.Println("  Skipped PubMed API key.")
			}
		}
	}

	// 4. TinyTeX for PDF rendering
	if quartoPath, err := exec.LookPath("quarto"); err == nil {
		if !isTinyTeXInstalled(quartoPath) {
			pdfHelpLink := docsLink("#pdf-quarto")
			if !isTinyTeXAutoInstallSupported(runtime.GOOS, runtime.GOARCH) {
				fmt.Printf("  TinyTeX auto-install isn't supported on %s/%s.\n", runtime.GOOS, runtime.GOARCH)
				fmt.Printf("  Help: %s\n", pdfHelpLink)
			} else if promptYesNo(r, "Install TinyTeX for PDF rendering (recommended, ~250 MB)?", true) {
				fmt.Println("  Installing TinyTeX via Quarto...")
				unsupported, installErr := tryInstallTinyTeX(quartoPath)
				if unsupported {
					fmt.Printf("  TinyTeX auto-install isn't available on %s/%s.\n", runtime.GOOS, runtime.GOARCH)
					fmt.Printf("  Help: %s\n", pdfHelpLink)
				} else if installErr != nil {
					fmt.Printf("  TinyTeX install failed: %v\n", installErr)
					fmt.Printf("  Help: %s\n", pdfHelpLink)
				} else {
					fmt.Println("  TinyTeX installed.")
				}
			}
		}
	}

	// 5. Chat channels (messaging apps)
	fmt.Printf("  Help (messaging apps): %s\n", docsLink("#telegram"))
	if promptYesNo(r, "Set up messaging apps (Telegram/Discord) now?", false) {
		runChannelsWizard(r, cfg, configPath)
	}
}

func promptYesNo(r *bufio.Reader, question string, defaultYes bool) bool {
	def := "y/N"
	if defaultYes {
		def = "Y/n"
	}
	for {
		fmt.Printf("  %s [%s]: ", question, def)
		line, _ := r.ReadString('\n')
		s := strings.TrimSpace(strings.ToLower(line))
		if s == "" {
			return defaultYes
		}
		if s == "y" || s == "yes" {
			return true
		}
		if s == "n" || s == "no" {
			return false
		}
		fmt.Println("  Please answer y or n.")
	}
}

func promptLine(r *bufio.Reader, question string) string {
	fmt.Printf("  %s ", question)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

func onboardAuthLoginOpenAI() error {
	cfg := auth.OpenAIOAuthConfig()

	cred, err := auth.LoginDeviceCode(cfg)
	if err != nil {
		return err
	}
	if err := auth.SetCredential("openai", cred); err != nil {
		return err
	}

	appCfg, err := loadConfig()
	if err == nil && appCfg != nil {
		appCfg.Providers.OpenAI.AuthMethod = "oauth"
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("  Warning: could not update config auth_method: %v\n", err)
		}
	}

	fmt.Println("  OpenAI login successful!")
	if cred.AccountID != "" {
		fmt.Printf("  Account: %s\n", cred.AccountID)
	}
	return nil
}

func runSelfAgentOneShot(message string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "agent", "-m", message)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func isTinyTeXInstalled(quartoPath string) bool {
	out, err := exec.Command(quartoPath, "list", "tools").CombinedOutput()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "tinytex" && fields[1] != "Not" {
			return true
		}
	}
	return false
}

func isTinyTeXAutoInstallSupported(goos, goarch string) bool {
	// Quarto tinytex auto-install is not supported on Linux ARM.
	if goos == "linux" && (goarch == "arm64" || strings.HasPrefix(goarch, "arm")) {
		return false
	}
	return true
}

func tryInstallTinyTeX(quartoPath string) (unsupported bool, err error) {
	cmd := exec.Command(quartoPath, "install", "tinytex", "--no-prompt")
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return false, nil
	}

	rawOutput := string(out)
	if isTinyTeXUnsupportedOutput(rawOutput) {
		return true, nil
	}

	summary := summarizeTinyTeXInstallOutput(rawOutput)
	if summary != "" {
		return false, fmt.Errorf("%s", summary)
	}
	return false, runErr
}

func isTinyTeXUnsupportedOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "doesn't support installation at this time") ||
		strings.Contains(lower, "does not support installation at this time")
}

func summarizeTinyTeXInstallOutput(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "stack trace") ||
			strings.HasPrefix(lower, "at ") ||
			lower == "installing tinytex" ||
			strings.Contains(lower, "[non-error-thrown] undefined") {
			continue
		}
		return trimmed
	}
	return ""
}

func onboardHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nOnboard:")
	fmt.Printf("  %s onboard initializes your sciClaw config and workspace.\n", commandName)
	fmt.Printf("  It is idempotent by default: it preserves existing config/auth and only creates missing workspace files.\n")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --yes        Non-interactive; never prompts (safe defaults)")
	fmt.Println("  --force      Reset config.json to defaults (backs up existing file first)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s onboard\n", commandName)
	fmt.Printf("  %s onboard --yes\n", commandName)
	fmt.Printf("  %s onboard --yes --force\n", commandName)
}

func parseOnboardOptions(args []string) (yes bool, force bool, showHelp bool, err error) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes", "-y":
			yes = true
		case "--force", "-f":
			force = true
		case "help", "--help", "-h":
			showHelp = true
		default:
			return false, false, false, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return yes, force, showHelp, nil
}

func detectProviderAuth(provider string, cfg *config.Config) (string, bool) {
	var pc config.ProviderConfig
	switch provider {
	case "openai":
		pc = cfg.Providers.OpenAI
	case "anthropic":
		pc = cfg.Providers.Anthropic
	case "openrouter":
		pc = cfg.Providers.OpenRouter
	case "gemini":
		pc = cfg.Providers.Gemini
	case "groq":
		pc = cfg.Providers.Groq
	case "deepseek":
		pc = cfg.Providers.DeepSeek
	case "zhipu":
		pc = cfg.Providers.Zhipu
	default:
		return "", false
	}

	if strings.TrimSpace(pc.APIKey) != "" {
		return "api_key", true
	}

	if provider == "openai" || provider == "anthropic" {
		if cred, err := auth.GetCredential(provider); err == nil && cred != nil && strings.TrimSpace(cred.AccessToken) != "" {
			method := strings.TrimSpace(cred.AuthMethod)
			if method == "" {
				method = "token"
			}
			if cred.IsExpired() {
				return method + " expired", false
			}
			return method, true
		}
	}

	if pc.AuthMethod == "oauth" || pc.AuthMethod == "token" {
		return pc.AuthMethod, true
	}

	return "", false
}

func configuredChatChannels(cfg *config.Config) []string {
	out := make([]string, 0, 2)
	if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) != "" {
		out = append(out, "telegram")
	}
	if cfg.Channels.Discord.Enabled && strings.TrimSpace(cfg.Channels.Discord.Token) != "" {
		out = append(out, "discord")
	}
	return out
}

func hasAnyWeakAllowlist(cfg *config.Config) bool {
	if cfg.Channels.Telegram.Enabled && strings.TrimSpace(cfg.Channels.Telegram.Token) != "" && len(cfg.Channels.Telegram.AllowFrom) == 0 {
		return true
	}
	if cfg.Channels.Discord.Enabled && strings.TrimSpace(cfg.Channels.Discord.Token) != "" && len(cfg.Channels.Discord.AllowFrom) == 0 {
		return true
	}
	return false
}

func docsLink(anchor string) string {
	if strings.HasPrefix(anchor, "#") {
		return docsURLBase + anchor
	}
	if anchor == "" {
		return docsURLBase
	}
	return docsURLBase + "#" + anchor
}

func backupFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	perm := os.FileMode(0600)
	if st, err := os.Stat(path); err == nil {
		perm = st.Mode().Perm()
	}
	ts := time.Now().UTC().Format("20060102-150405Z")
	backupPath := fmt.Sprintf("%s.bak.%s", path, ts)
	if err := os.WriteFile(backupPath, b, perm); err != nil {
		return "", err
	}
	return backupPath, nil
}

func createWorkspaceTemplates(workspace string) {
	dirs := []string{"memory", "skills", "sessions", "cron"}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(workspace, dir), 0755); err != nil {
			fmt.Printf("  Failed to create %s/: %v\n", dir, err)
		}
	}

	templates, err := workspacetpl.Load()
	if err != nil {
		fmt.Printf("  Failed to load workspace templates: %v\n", err)
		return
	}

	for _, tpl := range templates {
		writeFileIfMissing(
			filepath.Join(workspace, tpl.RelativePath),
			tpl.Content,
			fmt.Sprintf("  Created %s\n", tpl.RelativePath),
		)
	}

	if err := ensureToolsCLIFirstPolicy(workspace); err != nil {
		fmt.Printf("  Failed to apply TOOLS.md CLI-first policy: %v\n", err)
	}

	ensureBaselineScienceSkills(workspace)
}

func writeFileIfMissing(path, content, successMsg string) {
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("  Failed to create %s: %v\n", filepath.Base(path), err)
		return
	}
	fmt.Print(successMsg)
}

func ensureBaselineScienceSkills(workspace string) {
	ensureBaselineScienceSkillsFromSources(workspace, baselineSkillSourceDirs(workspace))
}

func ensureBaselineScienceSkillsFromSources(workspace string, sourceRoots []string) {
	workspaceSkillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(workspaceSkillsDir, 0755); err != nil {
		fmt.Printf("  Failed to create workspace skills dir: %v\n", err)
		return
	}

	var missing []string
	for _, skillName := range baselineScienceSkillNames {
		dstDir := filepath.Join(workspaceSkillsDir, skillName)
		dstSkillFile := filepath.Join(dstDir, "SKILL.md")
		if _, err := os.Stat(dstSkillFile); err == nil {
			continue
		}

		srcDir, found := findBaselineSkillSource(skillName, sourceRoots)
		if !found {
			missing = append(missing, skillName)
			continue
		}

		if err := copyDirectory(srcDir, dstDir); err != nil {
			fmt.Printf("  Failed to install baseline skill %s: %v\n", skillName, err)
			continue
		}
		fmt.Printf("  Installed baseline skill: %s\n", skillName)
	}

	if len(missing) > 0 {
		fmt.Printf("  Baseline skill sources unavailable (skipped): %s\n", strings.Join(missing, ", "))
	}
}

func findBaselineSkillSource(skillName string, sourceRoots []string) (string, bool) {
	for _, root := range sourceRoots {
		skillDir := filepath.Join(root, skillName)
		skillFile := filepath.Join(skillDir, "SKILL.md")
		if info, err := os.Stat(skillFile); err == nil && !info.IsDir() {
			return skillDir, true
		}
	}
	return "", false
}

func baselineSkillSourceDirs(workspace string) []string {
	candidates := []string{}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(wd, "skills"))
	}

	if exePath, err := os.Executable(); err == nil {
		candidates = append(candidates, skillSourceDirsForExecutable(exePath)...)
	}

	// User-local fallback, e.g. ~/.picoclaw/skills
	candidates = append(candidates, filepath.Join(filepath.Dir(workspace), "skills"))

	dirs := []string{}
	seen := map[string]struct{}{}
	for _, dir := range candidates {
		cleaned := filepath.Clean(dir)
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		if info, err := os.Stat(cleaned); err == nil && info.IsDir() {
			dirs = append(dirs, cleaned)
		}
	}
	return dirs
}

func skillSourceDirsForExecutable(exePath string) []string {
	exeDir := filepath.Dir(exePath)
	shareDir := filepath.Clean(filepath.Join(exeDir, "..", "share"))

	dirs := []string{
		filepath.Join(shareDir, "sciclaw", "skills"),
		filepath.Join(shareDir, "picoclaw", "skills"),
		filepath.Join(shareDir, "sciclaw-dev", "skills"),
	}

	// Homebrew formula installs resources under share/<formula>/...
	// e.g. sciclaw, sciclaw-dev.
	formulaName := filepath.Base(filepath.Dir(filepath.Dir(shareDir)))
	if formulaName != "" && formulaName != "." && formulaName != string(filepath.Separator) {
		dirs = append(dirs, filepath.Join(shareDir, formulaName, "skills"))
	}

	return dirs
}

func resolveBuiltinSkillsDir(workspace string) string {
	for _, dir := range baselineSkillSourceDirs(workspace) {
		if dirHasSkillMarkdown(dir) {
			return dir
		}
	}
	return ""
}

func dirHasSkillMarkdown(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}

func migrateCmd() {
	if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
		migrateHelp()
		return
	}

	opts := migrate.Options{}

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			opts.DryRun = true
		case "--config-only":
			opts.ConfigOnly = true
		case "--workspace-only":
			opts.WorkspaceOnly = true
		case "--force":
			opts.Force = true
		case "--refresh":
			opts.Refresh = true
		case "--openclaw-home":
			if i+1 < len(args) {
				opts.OpenClawHome = args[i+1]
				i++
			}
		case "--picoclaw-home":
			if i+1 < len(args) {
				opts.PicoClawHome = args[i+1]
				i++
			}
		default:
			fmt.Printf("Unknown flag: %s\n", args[i])
			migrateHelp()
			os.Exit(1)
		}
	}

	result, err := migrate.Run(opts)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if !opts.DryRun {
		migrate.PrintSummary(result)
	}
}

func migrateHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nMigrate from OpenClaw to sciClaw")
	fmt.Println()
	fmt.Printf("Usage: %s migrate [options]\n", commandName)
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --dry-run          Show what would be migrated without making changes")
	fmt.Println("  --refresh          Re-sync workspace files from OpenClaw (repeatable)")
	fmt.Println("  --config-only      Only migrate config, skip workspace files")
	fmt.Println("  --workspace-only   Only migrate workspace files, skip config")
	fmt.Println("  --force            Skip confirmation prompts")
	fmt.Println("  --openclaw-home    Override OpenClaw home directory (default: ~/.openclaw)")
	fmt.Println("  --picoclaw-home    Override PicoClaw home directory (default: ~/.picoclaw)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s migrate              Detect and migrate from OpenClaw\n", commandName)
	fmt.Printf("  %s migrate --dry-run    Show what would be migrated\n", commandName)
	fmt.Printf("  %s migrate --refresh    Re-sync workspace files\n", commandName)
	fmt.Printf("  %s migrate --force      Migrate without confirmation\n", commandName)
}

func agentCmd() {
	message := ""
	sessionKey := "cli:default"
	modelOverride := ""
	effortOverride := ""

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-s", "--session":
			if i+1 < len(args) {
				sessionKey = args[i+1]
				i++
			}
		case "--model":
			if i+1 < len(args) {
				modelOverride = args[i+1]
				i++
			}
		case "--effort":
			if i+1 < len(args) {
				effortOverride = args[i+1]
				i++
			}
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	applyAgentCLIOverrides(cfg, modelOverride, effortOverride)

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Print agent startup info (only for interactive mode)
	startupInfo := agentLoop.GetStartupInfo()
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      startupInfo["tools"].(map[string]interface{})["count"],
			"skills_total":     startupInfo["skills"].(map[string]interface{})["total"],
			"skills_available": startupInfo["skills"].(map[string]interface{})["available"],
		})

	if message != "" {
		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, message, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n%s %s\n", logo, response)
	} else {
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		interactiveMode(agentLoop, sessionKey)
	}
}

func applyAgentCLIOverrides(cfg *config.Config, modelOverride, effortOverride string) {
	if modelOverride != "" {
		cfg.Agents.Defaults.Model = modelOverride
		if provider := models.ResolveProvider(modelOverride, cfg); provider != "unknown" {
			cfg.Agents.Defaults.Provider = provider
		}
	}
	if effortOverride != "" {
		cfg.Agents.Defaults.ReasoningEffort = effortOverride
	}
}

func interactiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	prompt := fmt.Sprintf("%s You: ", logo)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     filepath.Join(os.TempDir(), ".picoclaw_history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		fmt.Println("Falling back to simple input mode...")
		simpleInteractiveMode(agentLoop, sessionKey)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func simpleInteractiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(fmt.Sprintf("%s You: ", logo))
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func gatewayCmd() {
	// Check for --debug flag
	args := os.Args[2:]
	if len(args) > 0 && strings.EqualFold(strings.TrimSpace(args[0]), "status") {
		// Backward-compatible alias: avoid accidentally launching a second
		// gateway process when users ask for status.
		originalArgs := append([]string(nil), os.Args...)
		os.Args = []string{originalArgs[0], "service", "status"}
		defer func() { os.Args = originalArgs }()
		serviceCmd()
		return
	}

	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
			break
		}
	}

	// Guard against double-running channel pollers (e.g. Telegram 409 conflicts)
	// when users already have the managed service active.
	// Skip this check when we ARE the managed service (launchd sets XPC_SERVICE_NAME,
	// systemd sets INVOCATION_ID).
	if os.Getenv("XPC_SERVICE_NAME") == "" && os.Getenv("INVOCATION_ID") == "" {
		exePath, err := resolveServiceExecutablePath(os.Args[0], exec.LookPath, os.Executable)
		if err == nil {
			if mgr, mgrErr := svcmgr.NewManager(exePath); mgrErr == nil {
				if st, statusErr := mgr.Status(); statusErr == nil && st.Running {
					backend := strings.TrimSpace(st.Backend)
					if backend == "" {
						backend = mgr.Backend()
					}
					fmt.Fprintf(os.Stderr, "Gateway is already running via %s service.\n", backend)
					fmt.Fprintf(os.Stderr, "  Stop it first:  %s service stop\n", invokedCLIName())
					fmt.Fprintf(os.Stderr, "  View logs:      %s service logs\n", invokedCLIName())
					fmt.Fprintf(os.Stderr, "  Restart:        %s service restart\n", invokedCLIName())
					os.Exit(1)
				}
			}
		}
	}

	// Resolve home directory once for all path construction below (status
	// file, log file, etc.). If it fails, skip home-dependent setup.
	gwHome, gwHomeErr := os.UserHomeDir()
	picoDir := filepath.Join(gwHome, ".picoclaw")

	// Kill any stale gateway process from a previous run (e.g. orphaned SSH session).
	// The status file is written on startup and removed on clean shutdown — if it
	// still exists with a live PID, that process is a zombie competing for the
	// Discord/Telegram websocket.
	if gwHomeErr == nil {
		statusPath := filepath.Join(picoDir, "gateway.status.json")
		if data, err := os.ReadFile(statusPath); err == nil {
			var status struct {
				PID int `json:"pid"`
			}
			if json.Unmarshal(data, &status) == nil && status.PID > 0 && status.PID != os.Getpid() {
				if proc, err := os.FindProcess(status.PID); err == nil {
					if proc.Signal(syscall.Signal(0)) == nil {
						if ok, checkErr := isGatewayProcessPID(status.PID); checkErr != nil {
							fmt.Fprintf(os.Stderr, "Skipping stale PID %d: unable to verify process owner (%v)\n", status.PID, checkErr)
						} else if !ok {
							fmt.Fprintf(os.Stderr, "Skipping stale PID %d: process is not a sciClaw gateway instance\n", status.PID)
						} else {
							fmt.Fprintf(os.Stderr, "Stopping stale gateway (PID %d)...\n", status.PID)
							_ = proc.Signal(syscall.SIGTERM)
							time.Sleep(2 * time.Second)
						}
					}
				}
			}
			_ = os.Remove(statusPath)
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Enable structured JSON logging to file for all gateway runs (not just
	// launchd-managed ones). The launchd plist also redirects stdout/stderr to
	// this path, but EnableFileLogging writes structured JSON independently of
	// how the process was launched.
	if gwHomeErr == nil {
		if err := logger.EnableFileLogging(filepath.Join(picoDir, "gateway.log")); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not enable file logging: %v\n", err)
		}
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Initialize the addon hook dispatcher before any emission site can fire.
	//
	// The dispatcher is a no-op when the addon registry is empty, so it is
	// safe to wire in unconditionally — core code pays nothing until the user
	// installs an addon. Wave 4a wires the real SidecarRegistry into both
	// the Dispatcher.Lookup callback and the addon Lifecycle so that
	// `sciclaw addon enable <name>` actually spawns a process and hook
	// events can find the live sidecar.
	//
	// sciclawHome must match whatever path the CLI's sciclawHomeDir() uses
	// so gateway reconciler and CLI install/enable agree on where
	// registry.json lives. On unmigrated installs this is still ~/.picoclaw,
	// not ~/sciclaw — the directory-unification migration from the RFC has
	// not been implemented yet. Going through sciclawHomeDir() keeps the
	// two sides in sync automatically when that migration lands.
	sciclawHome := sciclawHomeDir()
	addonStore := addons.NewStore(sciclawHome)
	addonSidecarRegistry = addons.NewSidecarRegistry()
	SetAddonDispatcher(&addons.Dispatcher{
		Store:       addonStore,
		SciclawHome: sciclawHome,
		Lookup:      addonSidecarRegistry.Lookup,
		Timeout:     5 * time.Second,
		Log: func(name, event string, err error) {
			if err != nil {
				logger.WarnCF("addons", "hook dispatch error", map[string]interface{}{
					"addon": name,
					"event": event,
					"error": err.Error(),
				})
			}
		},
	})

	// Wave 4d: reconciliation control loop. The CLI (`sciclaw addon enable
	// …`) mutates registry.json in a short-lived process and does NOT own
	// sidecars — this Reconciler is the long-lived process that turns those
	// persisted state changes into real spawn/stop calls on the
	// addonSidecarRegistry. A ticker drives eventual consistency, and the
	// CLI also touches addons/reload via addons.TriggerReload so the loop
	// kicks off an immediate pass and `addon enable` feels synchronous.
	addonReconciler := &addons.Reconciler{
		Store:        addonStore,
		Registry:     addonSidecarRegistry,
		SciclawHome:  sciclawHome,
		Interval:     10 * time.Second,
		StartTimeout: 10 * time.Second,
		StopTimeout:  10 * time.Second,
		Log: func(name, event string, err error) {
			fields := map[string]interface{}{
				"addon": name,
				"event": event,
			}
			if err != nil {
				fields["error"] = err.Error()
				logger.WarnCF("addons", "reconcile", fields)
				return
			}
			logger.InfoCF("addons", "reconcile", fields)
		},
	}

	// Wire per-user profile store for answer theme resolution
	gatewayProfileStore := profile.NewStore(filepath.Join(picoDir, "profiles"))
	gatewayProfileStore.OnProfileUpdated = func(senderID string, p *profile.UserProfile) {
		fireAddonHook("profile_updated", map[string]any{
			"sender_id": senderID,
			"profile":   p,
		})
	}
	agentLoop.SetProfileStore(gatewayProfileStore)

	// Print agent startup info
	fmt.Println("\n📦 Agent Status:")
	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n",
		skillsInfo["available"],
		skillsInfo["total"])

	// Log to file as well
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})

	// Write gateway status file for TUI version mismatch detection.
	gatewayStatusPath := filepath.Join(picoDir, "gateway.status.json")
	if statusJSON, err := json.Marshal(map[string]interface{}{
		"version":    version,
		"pid":        os.Getpid(),
		"started_at": time.Now().UTC().Format(time.RFC3339),
	}); err == nil {
		_ = os.WriteFile(gatewayStatusPath, statusJSON, 0644)
	}

	// Setup cron tool and service
	cronService := setupCronTool(agentLoop, msgBus, cfg.WorkspacePath(), cfg.Agents.Defaults.RestrictToWorkspace)

	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	heartbeatService.SetBus(msgBus)
	heartbeatService.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		// Use cli:direct as fallback if no valid channel
		if channel == "" || chatID == "" {
			channel, chatID = "cli", "direct"
		}
		// Use ProcessHeartbeat - no session history, each heartbeat is independent
		response, err := agentLoop.ProcessHeartbeat(context.Background(), prompt, channel, chatID)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("Heartbeat error: %v", err))
		}
		if response == "HEARTBEAT_OK" {
			return tools.SilentResult("Heartbeat OK")
		}
		// For heartbeat, always return silent - the subagent result will be
		// sent to user via processSystemMessage when the async task completes
		return tools.SilentResult(response)
	})

	channelManager, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}

	var transcriber *voice.GroqTranscriber
	if cfg.Providers.Groq.APIKey != "" {
		transcriber = voice.NewGroqTranscriber(cfg.Providers.Groq.APIKey)
		logger.InfoC("voice", "Groq voice transcription enabled")
	}

	if transcriber != nil {
		if telegramChannel, ok := channelManager.GetChannel("telegram"); ok {
			if tc, ok := telegramChannel.(*channels.TelegramChannel); ok {
				tc.SetTranscriber(transcriber)
				logger.InfoC("voice", "Groq transcription attached to Telegram channel")
			}
		}
		if discordChannel, ok := channelManager.GetChannel("discord"); ok {
			if dc, ok := discordChannel.(*channels.DiscordChannel); ok {
				dc.SetTranscriber(transcriber)
				logger.InfoC("voice", "Groq transcription attached to Discord channel")
			}
		}
		if slackChannel, ok := channelManager.GetChannel("slack"); ok {
			if sc, ok := slackChannel.(*channels.SlackChannel); ok {
				sc.SetTranscriber(transcriber)
				logger.InfoC("voice", "Groq transcription attached to Slack channel")
			}
		}
	}

	var channelHistoryCB tools.FetchChannelHistoryCallback
	if discordCh, ok := channelManager.GetChannel("discord"); ok {
		if dc, ok := discordCh.(*channels.DiscordChannel); ok {
			channelHistoryCB = discordHistoryCallback(dc)
			attachChannelHistoryCallback(agentLoop, channelHistoryCB)
			dc.SetSlashSkillCatalogCallback(func(channelID, guildID, userID string) ([]channels.SlashSkillChoice, error) {
				return listDiscordSlashSkills(cfg, channelID, guildID, userID)
			})
			dc.SetThemeHandler(func(senderID, displayName, theme string) error {
				return gatewayProfileStore.SetAnswerTheme(senderID, displayName, theme)
			})
			logger.InfoC("tools", "Channel history tool attached to Discord")
		}
	}

	enabledChannels := channelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	fmt.Println("✓ Gateway started")
	fmt.Println("Press Ctrl+C to stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the addon reconciliation loop. Uses the same ctx that the
	// shutdown sequence below cancels, so the reconciler goroutine exits
	// before addonSidecarRegistry.StopAll runs and tears down the
	// sidecars. Run returns ctx.Err() on cancel; that is expected, not an
	// error condition, and is filtered below. The shutdown path blocks on
	// addonReconcilerDone so a reconcile pass that is mid-spawn cannot
	// race StopAll and leak a freshly-registered sidecar.
	addonReconcilerDone := make(chan struct{})
	go func() {
		defer close(addonReconcilerDone)
		if err := addonReconciler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.WarnCF("addons", "reconciler exited", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()

	var loopPool *routing.AgentLoopPool
	var jobManager *routing.JobManager
	btwLaneResolver := func(target routing.LoopTarget) (routing.JobRunner, error) {
		cloned, err := routing.CloneConfigForTarget(cfg, target)
		if err != nil {
			return nil, err
		}
		loopProvider, err := providers.CreateProvider(cloned)
		if err != nil {
			return nil, fmt.Errorf("creating provider for side-lane job: %w", err)
		}
		al := agent.NewAgentLoopWithOptions(cloned, msgBus, loopProvider, agent.LoopOptions{
			ToolProfile: agent.ToolProfileSideLane,
		})
		attachChannelHistoryCallback(al, channelHistoryCB)
		return al, nil
	}
	if cfg.Jobs.Enabled && cfg.Jobs.DiscordAsyncDefault {
		jm, jmErr := routing.NewJobManager(
			filepath.Join(filepath.Dir(getConfigPath()), "jobs.json"),
			cfg.Jobs,
			msgBus,
			channelManager,
			func(target routing.LoopTarget) (routing.JobRunner, error) {
				if loopPool != nil {
					return loopPool.ResolveJobHandler(target)
				}
				return agentLoop, nil
			},
		)
		if jmErr != nil {
			fmt.Printf("Warning: background jobs disabled: %v\n", jmErr)
		} else {
			jobManager = jm
			jobManager.SetSideLaneResolver(btwLaneResolver)
			fmt.Println("✓ Discord background jobs enabled")
		}
	}
	// watchRoutingReload is always started, even when routing is disabled,
	// so CLI-initiated routing changes (which touch the reload marker and
	// may also flip Routing.Enabled) reach addon hooks without requiring
	// a gateway restart. A nil dispatcher means "only fire addon hooks,
	// don't try to ReplaceResolver".
	var routingDispatcher *routing.Dispatcher
	if cfg.Routing.Enabled {
		resolver, err := routing.NewResolver(cfg)
		if err != nil {
			fmt.Printf("Error initializing routing: %v\n", err)
			os.Exit(1)
		}
		// Build a setup function that wires channel_history and profile store for each routed agent loop.
		var poolSetup []routing.LoopSetupFunc
		poolSetup = append(poolSetup, func(al *agent.AgentLoop) {
			al.SetProfileStore(gatewayProfileStore)
		})
		if channelHistoryCB != nil {
			poolSetup = append(poolSetup, func(al *agent.AgentLoop) {
				attachChannelHistoryCallback(al, channelHistoryCB)
			})
		}
		loopPool = routing.NewAgentLoopPool(cfg, msgBus, poolSetup...)
		if jobManager != nil {
			jobManager.SetSideLaneResolver(loopPool.ResolveBTWJobHandler)
		}
		routingDispatcher = routing.NewDispatcher(msgBus, resolver, loopPool)
		if jobManager != nil {
			routingDispatcher.SetJobManager(jobManager)
		}
		go func() {
			if err := routingDispatcher.Run(ctx); err != nil {
				logger.ErrorCF("routing", "route_invalid", map[string]interface{}{"reason": err.Error()})
			}
		}()
		fmt.Printf("✓ Routing enabled: %d mapping(s)\n", len(cfg.Routing.Mappings))
	}
	go watchRoutingReload(ctx, routingDispatcher)
	fmt.Printf("✓ Routing reload trigger: %s\n", routingReloadTriggerPath())
	if !cfg.Routing.Enabled {
		if jobManager != nil {
			go runGatewayDefaultDispatch(ctx, msgBus, agentLoop, jobManager, routing.LoopTarget{
				Workspace: cfg.WorkspacePath(),
				Runtime: routing.RuntimeProfile{
					Mode:         cfg.EffectiveMode(),
					LocalBackend: cfg.Agents.Defaults.LocalBackend,
					LocalModel:   cfg.Agents.Defaults.LocalModel,
					LocalPreset:  cfg.Agents.Defaults.LocalPreset,
				},
			})
		} else {
			go agentLoop.Run(ctx)
		}
	}

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("✓ Cron service started")

	if err := heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("✓ Heartbeat service started")

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	heartbeatService.Stop()
	cronService.Stop()
	if loopPool != nil {
		loopPool.Close()
	}
	agentLoop.Stop()
	channelManager.StopAll(ctx)
	// Wait for the reconciler goroutine to exit BEFORE StopAll, so an
	// in-flight reconcile pass cannot race StopAll's registry snapshot and
	// leave a freshly-spawned sidecar running. ctx was just cancelled above,
	// so Run will return promptly; the 5s cap is defense-in-depth.
	if addonReconcilerDone != nil {
		select {
		case <-addonReconcilerDone:
		case <-time.After(5 * time.Second):
			logger.WarnCF("addons", "reconciler shutdown timeout", nil)
		}
	}
	// Stop all live addon sidecars before exiting. Uses a fresh bounded
	// context because the gateway ctx was just cancelled above — StopAll
	// must still be able to reach its 10s deadline.
	if addonSidecarRegistry != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
		addonSidecarRegistry.StopAll(stopCtx, func(name string, err error) {
			logger.WarnCF("addons", "sidecar stop error on shutdown", map[string]interface{}{
				"addon": name,
				"error": err.Error(),
			})
		})
		stopCancel()
	}
	_ = os.Remove(gatewayStatusPath)
	logger.DisableFileLogging()
	fmt.Println("✓ Gateway stopped")
}

func modesCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	commandName := invokedCLIName()

	if len(os.Args) < 3 {
		modesStatus(cfg)
		return
	}

	switch os.Args[2] {
	case "status":
		modesStatus(cfg)
	case "set":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s modes set <cloud|phi|vm>\n", commandName)
			os.Exit(1)
		}
		modesSet(cfg, os.Args[3])
	case "phi-setup":
		modesPhiSetup(cfg)
	case "phi-status":
		modesPhiStatus(cfg)
	case "phi-eval":
		modesPhiEvalCmd(cfg, os.Args[3:])
	default:
		fmt.Printf("Unknown modes command: %s\n", os.Args[2])
		fmt.Printf("Usage: %s modes [status|set|phi-setup|phi-status|phi-eval]\n", commandName)
	}
}

func modesStatus(cfg *config.Config) {
	mode := cfg.EffectiveMode()
	switch mode {
	case config.ModePhi:
		fmt.Printf("Mode:     PHI (local inference)\n")
		fmt.Printf("Backend:  %s\n", cfg.Agents.Defaults.LocalBackend)
		fmt.Printf("Model:    %s\n", cfg.Agents.Defaults.LocalModel)
		if cfg.Agents.Defaults.LocalPreset != "" {
			fmt.Printf("Preset:   %s\n", cfg.Agents.Defaults.LocalPreset)
		}
		printHardwareInfo(hardware.Detect())
		if cfg.Agents.Defaults.LocalBackend != "" {
			printBackendHealth(cfg.Agents.Defaults.LocalBackend)
		}
	case config.ModeVM:
		fmt.Printf("Mode:     VM\n")
	default:
		fmt.Printf("Mode:     Cloud\n")
		fmt.Printf("Model:    %s\n", cfg.Agents.Defaults.Model)
		fmt.Printf("Provider: %s\n", models.ResolveProvider(cfg.Agents.Defaults.Model, cfg))
	}
}

func modesSet(cfg *config.Config, mode string) {
	configPath := getConfigPath()

	switch strings.ToLower(mode) {
	case config.ModeCloud:
		cfg.Agents.Defaults.Mode = ""
	case config.ModePhi, "local":
		if cfg.Agents.Defaults.LocalBackend == "" || cfg.Agents.Defaults.LocalModel == "" {
			fmt.Println("PHI mode not configured yet. Running setup...")
			modesPhiSetup(cfg)
			return
		}
		cfg.Agents.Defaults.Mode = config.ModePhi
	case config.ModeVM:
		cfg.Agents.Defaults.Mode = config.ModeVM
	default:
		fmt.Printf("Unknown mode: %s. Use: cloud, phi, or vm\n", mode)
		os.Exit(1)
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	switch cfg.EffectiveMode() {
	case config.ModePhi:
		fmt.Printf("Switched to PHI mode. Backend: %s, Model: %s\n",
			cfg.Agents.Defaults.LocalBackend, cfg.Agents.Defaults.LocalModel)
	case config.ModeVM:
		fmt.Println("Switched to VM mode.")
	default:
		fmt.Printf("Switched to cloud mode. Model: %s\n", cfg.Agents.Defaults.Model)
	}
}

func modesPhiSetup(cfg *config.Config) {
	configPath := getConfigPath()

	// 1. Detect hardware
	fmt.Println("Detecting hardware...")
	hw := hardware.Detect()
	printHardwareInfo(hw)

	// 2. Load catalog and match profile
	cat, err := phi.LoadCatalog()
	if err != nil {
		fmt.Printf("Error loading model catalog: %v\n", err)
		os.Exit(1)
	}

	profile := cat.MatchProfile(hw)
	if profile == nil {
		fmt.Println("\nNo suitable hardware profile found for this machine.")
		fmt.Println("PHI mode requires at least 6GB RAM. Consider using cloud mode.")
		os.Exit(1)
	}
	fmt.Printf("\nMatched profile: %s\n", profile.ProfileID)

	// 3. Select backend and model
	backend := cat.SelectBackend(profile)
	preset := "balanced"
	model, err := cat.SelectModel(profile, preset)
	if err != nil {
		fmt.Printf("Error selecting model: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Backend:  %s\n", backend)
	fmt.Printf("Model:    %s (preset: %s)\n", model.OllamaTag, preset)

	// 4. Check backend health
	fmt.Printf("\nChecking %s...\n", backend)
	status := phi.CheckBackend(backend)
	if !status.Installed {
		fmt.Printf("\n%s is not installed.\n", backend)
		if backend == config.BackendOllama {
			fmt.Println("Install Ollama from: https://ollama.com")
		}
		os.Exit(1)
	}
	if !status.Running {
		fmt.Printf("\n%s is installed but not running.\n", backend)
		if backend == config.BackendOllama {
			switch hw.OS {
			case "darwin":
				fmt.Println("Open the Ollama app to start the server.")
			case "linux":
				fmt.Println("Start Ollama with: systemctl start ollama")
			default:
				fmt.Println("Start the Ollama server and try again.")
			}
		}
		os.Exit(1)
	}
	fmt.Printf("  %s is running (%s)\n", backend, status.Version)

	// 5. Pull model
	if !phi.CheckModelReady(model.OllamaTag) {
		fmt.Printf("\nPulling model %s...\n", model.OllamaTag)
		err := phi.PullModel(context.Background(), model.OllamaTag, func(line string) {
			fmt.Printf("  %s\n", line)
		})
		if err != nil {
			fmt.Printf("Error pulling model: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("  Model pulled successfully.")
	} else {
		fmt.Printf("  Model %s is already available.\n", model.OllamaTag)
	}

	// 6. Warmup
	fmt.Println("\nVerifying model response...")
	if err := phi.WarmupModel(context.Background(), model.OllamaTag); err != nil {
		fmt.Printf("Warning: warmup failed: %v\n", err)
		fmt.Println("The model may still work. Continuing with setup.")
	} else {
		fmt.Println("  Model responded successfully.")
	}

	// 7. Persist config
	cfg.Agents.Defaults.Mode = config.ModePhi
	cfg.Agents.Defaults.LocalBackend = backend
	cfg.Agents.Defaults.LocalModel = model.OllamaTag
	cfg.Agents.Defaults.LocalPreset = preset
	if err := config.SaveConfig(configPath, cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\nPHI mode is ready!")
	fmt.Printf("  Mode:    phi\n")
	fmt.Printf("  Backend: %s\n", backend)
	fmt.Printf("  Model:   %s\n", model.OllamaTag)
	fmt.Printf("  Preset:  %s\n", preset)
}

func modesPhiStatus(cfg *config.Config) {
	mode := cfg.EffectiveMode()
	if mode != config.ModePhi {
		fmt.Println("Not in PHI mode. Current mode:", mode)
		return
	}

	backend := cfg.Agents.Defaults.LocalBackend
	fmt.Printf("Backend: %s\n", backend)
	fmt.Printf("Model:   %s\n", cfg.Agents.Defaults.LocalModel)

	status := phi.CheckBackend(backend)
	fmt.Printf("Installed: %v\n", status.Installed)
	fmt.Printf("Running:   %v\n", status.Running)
	if status.Version != "" {
		fmt.Printf("Version:   %s\n", status.Version)
	}

	if status.Running && cfg.Agents.Defaults.LocalModel != "" {
		ready := phi.CheckModelReady(cfg.Agents.Defaults.LocalModel)
		fmt.Printf("Model ready: %v\n", ready)
	}

	printHardwareInfo(hardware.Detect())
}

func printHardwareInfo(hw hardware.Profile) {
	fmt.Printf("Hardware: %s %s, %dGB RAM, GPU: %s\n", hw.OS, hw.Arch, hw.MemoryGB, hw.GPUVendor)
	if hw.VRAMGB > 0 {
		fmt.Printf("  VRAM: %d GB\n", hw.VRAMGB)
	}
}

func printBackendHealth(backend string) {
	status := phi.CheckBackend(backend)
	if status.Running {
		fmt.Printf("Status:   running (%s)\n", status.Version)
	} else if status.Installed {
		fmt.Printf("Status:   installed but not running\n")
	} else {
		fmt.Printf("Status:   not installed\n")
	}
	if status.Error != "" {
		fmt.Printf("Error:    %s\n", status.Error)
	}
}

func modelsCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	if len(os.Args) < 3 {
		models.PrintList(cfg)
		return
	}

	commandName := invokedCLIName()
	switch os.Args[2] {
	case "list":
		models.PrintList(cfg)
	case "discover":
		jsonOut := false
		if len(os.Args) >= 4 && os.Args[3] == "--json" {
			jsonOut = true
		}
		result := models.Discover(cfg)
		if jsonOut {
			data, err := json.Marshal(result)
			if err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println(string(data))
			return
		}
		models.PrintDiscover(result)
	case "set":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s models set <model>\n", commandName)
			os.Exit(1)
		}
		configPath := getConfigPath()
		if err := models.SetModel(cfg, configPath, os.Args[3]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "effort":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s models effort <level>\n", commandName)
			fmt.Println("  GPT-5.x levels: none, minimal, low, medium, high, xhigh")
			os.Exit(1)
		}
		configPath := getConfigPath()
		if err := models.SetEffort(cfg, configPath, os.Args[3]); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		models.PrintStatus(cfg)
	default:
		fmt.Printf("Unknown models command: %s\n", os.Args[2])
		fmt.Printf("Usage: %s models [list|discover|set|effort|status]\n", commandName)
	}
}

// discordHistoryCallback builds a FetchChannelHistoryCallback that reads
// messages from the Discord REST API via the given DiscordChannel.
func attachChannelHistoryCallback(al *agent.AgentLoop, cb tools.FetchChannelHistoryCallback) {
	if al == nil || cb == nil {
		return
	}
	al.SetChannelHistoryCallback(cb)
}

func discordHistoryCallback(dc *channels.DiscordChannel) tools.FetchChannelHistoryCallback {
	return func(channelID string, limit int, beforeID string) ([]tools.ChannelHistoryMessage, error) {
		msgs, err := dc.GetChannelMessages(channelID, limit, beforeID)
		if err != nil {
			return nil, err
		}
		// Discord API returns newest-first; reverse to chronological order.
		result := make([]tools.ChannelHistoryMessage, len(msgs))
		for i, msg := range msgs {
			author := "unknown"
			if msg.Author != nil {
				author = msg.Author.Username
			}
			result[len(msgs)-1-i] = tools.ChannelHistoryMessage{
				ID:        msg.ID,
				Author:    author,
				Content:   msg.Content,
				Timestamp: msg.Timestamp.Format("2006-01-02 15:04"),
			}
		}
		return result, nil
	}
}

func listDiscordSlashSkills(cfg *config.Config, channelID, guildID, userID string) ([]channels.SlashSkillChoice, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	workspace := cfg.WorkspacePath()
	if cfg.Routing.Enabled {
		resolver, err := routing.NewResolver(cfg)
		if err != nil {
			return nil, err
		}
		decision := resolver.Resolve(bus.InboundMessage{
			Channel:  "discord",
			ChatID:   strings.TrimSpace(channelID),
			SenderID: strings.TrimSpace(userID),
			Metadata: map[string]string{
				"is_dm":              fmt.Sprintf("%t", strings.TrimSpace(guildID) == ""),
				"is_mention":         "true",
				"has_direct_mention": "true",
				"reply_to_bot":       "false",
			},
		})
		if !decision.Allowed {
			return nil, nil
		}
		workspace = decision.Workspace
	}
	globalSkillsDir := filepath.Join(filepath.Dir(getConfigPath()), "skills")
	builtinSkillsDir := resolveBuiltinSkillsDir(workspace)
	skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)
	infos := skillsLoader.ListSkills()
	choices := make([]channels.SlashSkillChoice, 0, len(infos))
	for _, info := range infos {
		choices = append(choices, channels.SlashSkillChoice{
			Name:        info.Name,
			Description: info.Description,
		})
	}
	sort.Slice(choices, func(i, j int) bool {
		return strings.ToLower(choices[i].Name) < strings.ToLower(choices[j].Name)
	})
	return choices, nil
}

func isGatewayProcessPID(pid int) (bool, error) {
	// Use pgrep to find all gateway processes and check if our PID is among them.
	// pgrep -f matches against the full command line.
	out, err := pgrepGatewayPIDs()
	if err != nil {
		// pgrep exits 1 when no processes match — that's not an error for us.
		type exitCoder interface{ ExitCode() int }
		if exitErr, ok := err.(exitCoder); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}

	// Check if our PID is in the list of matching PIDs.
	pidStr := strconv.Itoa(pid)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == pidStr {
			return true, nil
		}
	}
	return false, nil
}

func isGatewayProcessCommandLine(cmdline string) bool {
	fields := strings.Fields(strings.TrimSpace(strings.ToLower(cmdline)))
	if len(fields) < 2 {
		return false
	}

	for i := 0; i < len(fields)-1; i++ {
		cur := strings.Trim(fields[i], "\"'")
		next := strings.Trim(fields[i+1], "\"'")
		base := strings.ToLower(filepath.Base(cur))
		if (strings.Contains(base, "sciclaw") || strings.Contains(base, "picoclaw")) && next == "gateway" {
			return true
		}
	}
	return false
}

func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s %s Status (%s CLI)\n\n", logo, displayName, invokedCLIName())

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("Model: %s\n", cfg.Agents.Defaults.Model)
		fmt.Printf("Provider: %s\n", models.ResolveProvider(cfg.Agents.Defaults.Model, cfg))
		if cfg.Agents.Defaults.ReasoningEffort != "" {
			fmt.Printf("Reasoning Effort: %s\n", cfg.Agents.Defaults.ReasoningEffort)
		}

		hasOpenRouter := cfg.Providers.OpenRouter.APIKey != ""
		hasAnthropic := cfg.Providers.Anthropic.APIKey != ""
		hasOpenAI := cfg.Providers.OpenAI.APIKey != ""
		hasGemini := cfg.Providers.Gemini.APIKey != ""
		hasZhipu := cfg.Providers.Zhipu.APIKey != ""
		hasGroq := cfg.Providers.Groq.APIKey != ""
		hasVLLM := cfg.Providers.VLLM.APIBase != ""

		status := func(enabled bool) string {
			if enabled {
				return "✓"
			}
			return "not set"
		}
		fmt.Println("OpenRouter API:", status(hasOpenRouter))
		fmt.Println("Anthropic API:", status(hasAnthropic))
		fmt.Println("OpenAI API:", status(hasOpenAI))
		fmt.Println("Gemini API:", status(hasGemini))
		fmt.Println("Zhipu API:", status(hasZhipu))
		fmt.Println("Groq API:", status(hasGroq))
		if hasVLLM {
			fmt.Printf("vLLM/Local: ✓ %s\n", cfg.Providers.VLLM.APIBase)
		} else {
			fmt.Println("vLLM/Local: not set")
		}

		if irlPath, err := resolveIRLRuntimePath(cfg.WorkspacePath()); err == nil {
			fmt.Printf("IRL Runtime: ✓ %s\n", irlPath)
		} else {
			fmt.Println("IRL Runtime: missing (reinstall/update your sciClaw Homebrew package)")
		}

		store, _ := auth.LoadStore()
		if store != nil && len(store.Credentials) > 0 {
			fmt.Println("\nOAuth/Token Auth:")
			for provider, cred := range store.Credentials {
				status := "authenticated"
				if cred.IsExpired() {
					status = "expired"
				} else if cred.NeedsRefresh() {
					status = "needs refresh"
				}
				fmt.Printf("  %s (%s): %s\n", provider, cred.AuthMethod, status)
			}
		}
	}
}

func resolveIRLRuntimePath(workspace string) (string, error) {
	return irl.NewClient(workspace).ResolveBinaryPath()
}

func authCmd() {
	if len(os.Args) < 3 {
		authHelp()
		return
	}

	switch os.Args[2] {
	case "login":
		authLoginCmd()
	case "logout":
		authLogoutCmd()
	case "status":
		authStatusCmd()
	case "import-op":
		authImportOPCmd()
	default:
		fmt.Printf("Unknown auth command: %s\n", os.Args[2])
		authHelp()
	}
}

func authHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nAuth commands:")
	fmt.Println("  login       Login via device code or paste token")
	fmt.Println("  import-op   Import credentials from 1Password item JSON")
	fmt.Println("  logout      Remove stored credentials")
	fmt.Println("  status      Show current auth status")
	fmt.Println()
	fmt.Println("Login options:")
	fmt.Println("  --provider <name>    Provider to login with (openai, anthropic)")
	fmt.Println("  --device-code        Compatibility flag (OpenAI already uses device code by default)")
	fmt.Println()
	fmt.Println("Import-op options:")
	fmt.Println("  --provider <name>    Provider to import (openai, anthropic)")
	fmt.Println("  --item <item-ref>    1Password item reference (title/UUID)")
	fmt.Println("  --vault <vault>      Optional vault name/UUID")
	fmt.Println("  --auth-method <m>    Optional method override (oauth, token)")
	fmt.Println("  requires env: OP_SERVICE_ACCOUNT_TOKEN")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s auth login --provider openai\n", commandName)
	fmt.Printf("  %s auth login --provider anthropic\n", commandName)
	fmt.Printf("  %s auth import-op --provider openai --item \"OpenAI Creds\"\n", commandName)
	fmt.Printf("  %s auth import-op --provider anthropic --item \"Anthropic Token\" --vault \"AI\" --auth-method token\n", commandName)
	fmt.Println("  Anthropic API keys stay on the direct API path; Claude.ai oat tokens reroute through sciclaw-claude-agent")
	fmt.Printf("  %s auth logout --provider openai\n", commandName)
	fmt.Printf("  %s auth status\n", commandName)
	fmt.Printf("  (Compatibility alias also works: %s)\n", cliName)
}

func authLoginCmd() {
	provider := ""
	useDeviceCode := false

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--device-code":
			useDeviceCode = true
		}
	}

	if provider == "" {
		fmt.Println("Error: --provider is required")
		fmt.Println("Supported providers: openai, anthropic")
		return
	}

	switch provider {
	case "openai":
		authLoginOpenAI(useDeviceCode)
	case "anthropic":
		authLoginPasteToken(provider)
	default:
		fmt.Printf("Unsupported provider: %s\n", provider)
		fmt.Println("Supported providers: openai, anthropic")
	}
}

func authLoginOpenAI(useDeviceCode bool) {
	cfg := auth.OpenAIOAuthConfig()

	if !useDeviceCode {
		fmt.Println("Note: OpenAI login now uses device code flow by default.")
		fmt.Println("Tip: open https://auth.openai.com/codex/device and enter the code when prompted.")
	}

	cred, err := auth.LoginDeviceCode(cfg)

	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential("openai", cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		appCfg.Providers.OpenAI.AuthMethod = "oauth"
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
}

func authLoginPasteToken(provider string) {
	cred, err := auth.LoginPasteToken(provider, os.Stdin)
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential(provider, cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		switch provider {
		case "anthropic":
			appCfg.Providers.Anthropic.AuthMethod = "token"
		case "openai":
			appCfg.Providers.OpenAI.AuthMethod = "token"
		}
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Printf("Token saved for %s!\n", provider)
}

func authImportOPCmd() {
	provider := ""
	itemRef := ""
	vault := ""
	authMethod := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
		case "--item":
			if i+1 < len(args) {
				itemRef = strings.TrimSpace(args[i+1])
				i++
			}
		case "--vault":
			if i+1 < len(args) {
				vault = strings.TrimSpace(args[i+1])
				i++
			}
		case "--auth-method":
			if i+1 < len(args) {
				authMethod = strings.ToLower(strings.TrimSpace(args[i+1]))
				i++
			}
		default:
			fmt.Printf("Unknown option: %s\n", args[i])
			fmt.Printf("Usage: %s auth import-op --provider <openai|anthropic> --item <item-ref> [--vault <vault>] [--auth-method <oauth|token>]\n", invokedCLIName())
			return
		}
	}

	if provider == "" {
		fmt.Println("Error: --provider is required")
		fmt.Println("Supported providers: openai, anthropic")
		return
	}
	if provider != "openai" && provider != "anthropic" {
		fmt.Printf("Unsupported provider: %s\n", provider)
		fmt.Println("Supported providers: openai, anthropic")
		return
	}
	if itemRef == "" {
		fmt.Println("Error: --item is required")
		return
	}
	if authMethod != "" && authMethod != "oauth" && authMethod != "token" {
		fmt.Printf("Unsupported auth method: %s\n", authMethod)
		fmt.Println("Supported auth methods: oauth, token")
		return
	}
	if strings.TrimSpace(os.Getenv("OP_SERVICE_ACCOUNT_TOKEN")) == "" {
		fmt.Println("Error: OP_SERVICE_ACCOUNT_TOKEN is required for auth import-op")
		return
	}

	opPath, err := exec.LookPath("op")
	if err != nil {
		fmt.Println("Error: 1Password CLI `op` not found in PATH")
		fmt.Println("Install from https://developer.1password.com/docs/cli/get-started/")
		os.Exit(1)
	}

	opArgs := []string{"item", "get", itemRef}
	if vault != "" {
		opArgs = append(opArgs, "--vault", vault)
	}
	opArgs = append(opArgs, "--format", "json")

	output, err := exec.Command(opPath, opArgs...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		fmt.Printf("Failed to read 1Password item: %s\n", msg)
		os.Exit(1)
	}

	cred, err := parseOPItemCredentialCompat(output, provider, authMethod)
	if err != nil {
		fmt.Printf("Failed to parse 1Password item: %v\n", err)
		os.Exit(1)
	}
	if err := auth.SetCredential(provider, cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		switch provider {
		case "openai":
			appCfg.Providers.OpenAI.AuthMethod = cred.AuthMethod
		case "anthropic":
			appCfg.Providers.Anthropic.AuthMethod = cred.AuthMethod
		}
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Printf("Imported credentials for %s via 1Password (method: %s)\n", provider, cred.AuthMethod)
}

// parseOPItemCredentialCompat mirrors expected 1Password item parsing behavior for auth import.
func parseOPItemCredentialCompat(raw []byte, provider, authMethodOverride string) (*auth.AuthCredential, error) {
	type opField struct {
		ID      string      `json:"id"`
		Label   string      `json:"label"`
		Purpose string      `json:"purpose"`
		Type    string      `json:"type"`
		Value   interface{} `json:"value"`
	}
	type opItem struct {
		Fields []opField `json:"fields"`
	}

	var item opItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, fmt.Errorf("invalid op item JSON: %w", err)
	}

	values := make(map[string]string)
	for _, field := range item.Fields {
		value := strings.TrimSpace(fmt.Sprintf("%v", field.Value))
		if value == "" || value == "<nil>" {
			continue
		}
		for _, name := range []string{field.ID, field.Label} {
			key := normalizeOPFieldKey(name)
			if key == "" {
				continue
			}
			if _, exists := values[key]; !exists {
				values[key] = value
			}
		}
		if strings.EqualFold(strings.TrimSpace(field.Purpose), "PASSWORD") {
			if _, exists := values["password"]; !exists {
				values["password"] = value
			}
		}
	}

	findValue := func(keys ...string) string {
		for _, key := range keys {
			if value := strings.TrimSpace(values[key]); value != "" {
				return value
			}
		}
		return ""
	}

	accessToken := findValue(
		"access_token",
		"token",
		"api_key",
		"apikey",
		"password",
		"session_token",
	)
	if accessToken == "" {
		return nil, fmt.Errorf("no token-like field found (expected one of: access_token, token, api_key, password)")
	}

	refreshToken := findValue("refresh_token")
	accountID := findValue("account_id", "chatgpt_account_id", "organization_id", "org_id")
	authMethod := strings.ToLower(strings.TrimSpace(authMethodOverride))
	if authMethod == "" {
		authMethod = strings.ToLower(strings.TrimSpace(findValue("auth_method", "method")))
	}
	if authMethod == "" {
		if refreshToken != "" {
			authMethod = "oauth"
		} else {
			authMethod = "token"
		}
	}
	if authMethod != "oauth" && authMethod != "token" {
		return nil, fmt.Errorf("unsupported auth method %q", authMethod)
	}

	return &auth.AuthCredential{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccountID:    accountID,
		Provider:     provider,
		AuthMethod:   authMethod,
	}, nil
}

func normalizeOPFieldKey(raw string) string {
	key := strings.TrimSpace(strings.ToLower(raw))
	replacer := strings.NewReplacer("-", "_", " ", "_", ".", "_")
	key = replacer.Replace(key)
	for strings.Contains(key, "__") {
		key = strings.ReplaceAll(key, "__", "_")
	}
	return strings.Trim(key, "_")
}

func authLogoutCmd() {
	provider := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		}
	}

	if provider != "" {
		if err := auth.DeleteCredential(provider); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			switch provider {
			case "openai":
				appCfg.Providers.OpenAI.AuthMethod = ""
			case "anthropic":
				appCfg.Providers.Anthropic.AuthMethod = ""
			}
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Printf("Logged out from %s\n", provider)
	} else {
		if err := auth.DeleteAllCredentials(); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			appCfg.Providers.OpenAI.AuthMethod = ""
			appCfg.Providers.Anthropic.AuthMethod = ""
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Println("Logged out from all providers")
	}
}

func authStatusCmd() {
	store, err := auth.LoadStore()
	if err != nil {
		fmt.Printf("Error loading auth store: %v\n", err)
		return
	}

	if len(store.Credentials) == 0 {
		fmt.Println("No authenticated providers.")
		fmt.Printf("Run: %s auth login --provider <name>\n", invokedCLIName())
		return
	}

	fmt.Println("\nAuthenticated Providers:")
	fmt.Println("------------------------")
	for provider, cred := range store.Credentials {
		status := "active"
		if cred.IsExpired() {
			status = "expired"
		} else if cred.NeedsRefresh() {
			status = "needs refresh"
		}

		fmt.Printf("  %s:\n", provider)
		fmt.Printf("    Method: %s\n", cred.AuthMethod)
		fmt.Printf("    Status: %s\n", status)
		if cred.AccountID != "" {
			fmt.Printf("    Account: %s\n", cred.AccountID)
		}
		if !cred.ExpiresAt.IsZero() {
			fmt.Printf("    Expires: %s\n", cred.ExpiresAt.Format("2006-01-02 15:04"))
		}
	}
}

func getConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".picoclaw", "config.json")
}

func runGatewayDefaultDispatch(ctx context.Context, msgBus *bus.MessageBus, agentLoop *agent.AgentLoop, jobManager *routing.JobManager, target routing.LoopTarget) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, ok := msgBus.ConsumeInbound(ctx)
			if !ok {
				continue
			}
			if jobManager != nil && jobManager.ShouldHandleChannel(msg.Channel) {
				if err := jobManager.Submit(ctx, target, msg); err != nil && !constants.IsInternalChannel(msg.Channel) {
					_ = msgBus.PublishOutbound(ctx, bus.OutboundMessage{
						Channel: msg.Channel,
						ChatID:  msg.ChatID,
						Content: fmt.Sprintf("Failed to start background job: %v", err),
					})
				}
				continue
			}
			agentLoop.HandleInbound(ctx, msg)
		}
	}
}

func routingReloadTriggerPath() string {
	return filepath.Join(filepath.Dir(getConfigPath()), "routing.reload")
}

// watchRoutingReload polls the reload marker every 2s and reacts to mtime
// changes. If dispatcher is non-nil, it replaces the resolver so live
// routing picks up the new mappings without a gateway restart. Regardless
// of dispatcher state, it fires the routing_changed addon hook so addons
// see the change. This is always started at gateway boot, even when
// routing is currently disabled, because the user may enable routing
// at runtime and CLI addon hooks should reach addons as soon as routing
// marker updates appear.
func watchRoutingReload(ctx context.Context, dispatcher *routing.Dispatcher) {
	triggerPath := routingReloadTriggerPath()
	lastSeen := time.Time{}
	if st, err := os.Stat(triggerPath); err == nil {
		lastSeen = st.ModTime()
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			st, err := os.Stat(triggerPath)
			if err != nil {
				continue
			}
			if !st.ModTime().After(lastSeen) {
				continue
			}
			lastSeen = st.ModTime()

			cfg, err := loadConfig()
			if err != nil {
				logger.ErrorCF("routing", "route_reload_failure", map[string]interface{}{
					"reason": err.Error(),
				})
				continue
			}

			// If a dispatcher exists, rebuild the resolver and swap it in
			// so routing dispatch picks up the new mappings.
			if dispatcher != nil {
				resolver, rerr := routing.NewResolver(cfg)
				if rerr != nil {
					logger.ErrorCF("routing", "route_reload_failure", map[string]interface{}{
						"reason": rerr.Error(),
					})
					continue
				}
				dispatcher.ReplaceResolver(resolver)
			}
			logger.InfoCF("routing", "route_reload_success", map[string]interface{}{
				"mappings":   len(cfg.Routing.Mappings),
				"dispatcher": dispatcher != nil,
			})

			// Fire routing_changed addon hook from inside the gateway
			// process, where addonDispatcher is actually initialized.
			// CLI-initiated routing changes reach addons via this path
			// (CLI writes config.json + touches routing.reload → this
			// watcher picks it up → fires the hook to subscribed addons).
			fireAddonHook("routing_changed", RoutingChangedPayload{Rules: cfg.Routing.Mappings})
		}
	}
}

func setupCronTool(agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, workspace string, restrict bool) *cron.CronService {
	cronStorePath := filepath.Join(workspace, "cron", "jobs.json")

	// Create cron service
	cronService := cron.NewCronService(cronStorePath, nil)

	// Create and register CronTool
	cronTool := tools.NewCronTool(cronService, agentLoop, msgBus, workspace, restrict)
	agentLoop.RegisterTool(cronTool)

	// Set the onJob handler
	cronService.SetOnJob(func(job *cron.CronJob) (string, error) {
		result := cronTool.ExecuteJob(context.Background(), job)
		return result, nil
	})

	return cronService
}

func loadConfig() (*config.Config, error) {
	return config.LoadConfig(getConfigPath())
}

func cronCmd() {
	if len(os.Args) < 3 {
		cronHelp()
		return
	}

	subcommand := os.Args[2]

	// Load config to get workspace path
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	cronStorePath := filepath.Join(cfg.WorkspacePath(), "cron", "jobs.json")

	switch subcommand {
	case "list":
		cronListCmd(cronStorePath)
	case "add":
		cronAddCmd(cronStorePath)
	case "remove":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s cron remove <job_id>\n", invokedCLIName())
			return
		}
		cronRemoveCmd(cronStorePath, os.Args[3])
	case "enable":
		cronEnableCmd(cronStorePath, false)
	case "disable":
		cronEnableCmd(cronStorePath, true)
	default:
		fmt.Printf("Unknown cron command: %s\n", subcommand)
		cronHelp()
	}
}

func cronHelp() {
	fmt.Println("\nCron commands:")
	fmt.Println("  list              List all scheduled jobs")
	fmt.Println("  add              Add a new scheduled job")
	fmt.Println("  remove <id>       Remove a job by ID")
	fmt.Println("  enable <id>      Enable a job")
	fmt.Println("  disable <id>     Disable a job")
	fmt.Println()
	fmt.Println("Add options:")
	fmt.Println("  -n, --name       Job name")
	fmt.Println("  -m, --message    Message for agent")
	fmt.Println("  -e, --every      Run every N seconds")
	fmt.Println("  -c, --cron       Cron expression (e.g. '0 9 * * *')")
	fmt.Println("  -d, --deliver     Deliver response to channel")
	fmt.Println("  --to             Recipient for delivery")
	fmt.Println("  --channel        Channel for delivery")
}

func cronListCmd(storePath string) {
	cs := cron.NewCronService(storePath, nil)
	jobs := cs.ListJobs(true) // Show all jobs, including disabled

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}

	fmt.Println("\nScheduled Jobs:")
	fmt.Println("----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}

		nextRun := "scheduled"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		fmt.Printf("  %s (%s)\n", job.Name, job.ID)
		fmt.Printf("    Schedule: %s\n", schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Next run: %s\n", nextRun)
	}
}

func cronAddCmd(storePath string) {
	name := ""
	message := ""
	var everySec *int64
	cronExpr := ""
	deliver := false
	channel := ""
	to := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-e", "--every":
			if i+1 < len(args) {
				var sec int64
				fmt.Sscanf(args[i+1], "%d", &sec)
				everySec = &sec
				i++
			}
		case "-c", "--cron":
			if i+1 < len(args) {
				cronExpr = args[i+1]
				i++
			}
		case "-d", "--deliver":
			deliver = true
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		}
	}

	if name == "" {
		fmt.Println("Error: --name is required")
		return
	}

	if message == "" {
		fmt.Println("Error: --message is required")
		return
	}

	if everySec == nil && cronExpr == "" {
		fmt.Println("Error: Either --every or --cron must be specified")
		return
	}

	var schedule cron.CronSchedule
	if everySec != nil {
		everyMS := *everySec * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	}

	cs := cron.NewCronService(storePath, nil)
	job, err := cs.AddJob(name, schedule, message, deliver, channel, to)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}

	fmt.Printf("✓ Added job '%s' (%s)\n", job.Name, job.ID)
}

func cronRemoveCmd(storePath, jobID string) {
	cs := cron.NewCronService(storePath, nil)
	if cs.RemoveJob(jobID) {
		fmt.Printf("✓ Removed job %s\n", jobID)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func cronEnableCmd(storePath string, disable bool) {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s cron enable/disable <job_id>\n", invokedCLIName())
		return
	}

	jobID := os.Args[3]
	cs := cron.NewCronService(storePath, nil)
	enabled := !disable

	job := cs.EnableJob(jobID, enabled)
	if job != nil {
		status := "enabled"
		if disable {
			status = "disabled"
		}
		fmt.Printf("✓ Job '%s' %s\n", job.Name, status)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func skillsCmd() {
	if len(os.Args) < 3 {
		skillsHelp()
		return
	}

	subcommand := os.Args[2]

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	workspace := cfg.WorkspacePath()
	installer := skills.NewSkillInstaller(workspace)
	// 获取全局配置目录和内置 skills 目录
	globalDir := filepath.Dir(getConfigPath())
	globalSkillsDir := filepath.Join(globalDir, "skills")
	builtinSkillsDir := filepath.Join(globalDir, "picoclaw", "skills")
	skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

	switch subcommand {
	case "list":
		skillsListCmd(skillsLoader)
	case "install":
		skillsInstallCmd(installer)
	case "remove", "uninstall":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s skills remove <skill-name>\n", invokedCLIName())
			return
		}
		skillsRemoveCmd(installer, os.Args[3])
	case "search":
		skillsSearchCmd(installer)
	case "show":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s skills show <skill-name>\n", invokedCLIName())
			return
		}
		skillsShowCmd(skillsLoader, os.Args[3])
	default:
		fmt.Printf("Unknown skills command: %s\n", subcommand)
		skillsHelp()
	}
}

func skillsHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nSkills commands:")
	fmt.Println("  list                    List installed skills")
	fmt.Println("  install <repo>          Install skill from GitHub")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>           Remove installed skill")
	fmt.Println("  search                  Search available skills")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s skills list\n", commandName)
	fmt.Printf("  %s skills install drpedapati/sciclaw-skills/weather\n", commandName)
	fmt.Printf("  %s skills install-builtin\n", commandName)
	fmt.Printf("  %s skills list-builtin\n", commandName)
	fmt.Printf("  %s skills remove weather\n", commandName)
	fmt.Printf("  (Compatibility alias also works: %s)\n", cliName)
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

func skillsInstallCmd(installer *skills.SkillInstaller) {
	if len(os.Args) < 4 {
		commandName := invokedCLIName()
		fmt.Printf("Usage: %s skills install <github-repo>\n", commandName)
		fmt.Printf("Example: %s skills install drpedapati/sciclaw-skills/weather\n", commandName)
		return
	}

	repo := os.Args[3]
	fmt.Printf("Installing skill from %s...\n", repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installer.InstallFromGitHub(ctx, repo); err != nil {
		fmt.Printf("✗ Failed to install skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' installed successfully!\n", filepath.Base(repo))
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
}

func skillsInstallBuiltinCmd(workspace string) {
	builtinSkillsDir := resolveBuiltinSkillsDir(workspace)
	if strings.TrimSpace(builtinSkillsDir) == "" {
		fmt.Println("✗ No builtin skills source detected.")
		fmt.Println("  Reinstall sciclaw via Homebrew or run from a repo checkout that contains ./skills.")
		return
	}
	workspaceSkillsDir := filepath.Join(workspace, "skills")

	fmt.Printf("Copying builtin skills to workspace from %s...\n", builtinSkillsDir)

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("✗ Failed to read builtin skills: %v\n", err)
		return
	}

	installed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		if _, err := os.Stat(filepath.Join(builtinPath, "SKILL.md")); err != nil {
			continue
		}
		workspacePath := filepath.Join(workspaceSkillsDir, skillName)

		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}

		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
			continue
		}
		fmt.Printf("  ✓ %s\n", skillName)
		installed++
	}

	if installed == 0 {
		fmt.Println("⊘ No builtin skills found to install.")
		return
	}

	fmt.Printf("\n✓ Installed %d builtin skills.\n", installed)
	fmt.Println("Now you can use them in your workspace.")
}

func skillsListBuiltinCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	builtinSkillsDir := resolveBuiltinSkillsDir(cfg.WorkspacePath())
	if strings.TrimSpace(builtinSkillsDir) == "" {
		fmt.Println("\nAvailable Builtin Skills:")
		fmt.Println("-----------------------")
		fmt.Println("No builtin skills source detected.")
		return
	}

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")
	fmt.Printf("Source: %s\n", builtinSkillsDir)

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("Error reading builtin skills: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillFile := filepath.Join(builtinSkillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}
		fmt.Printf("  ✓  %s\n", entry.Name())
	}
}

func skillsSearchCmd(installer *skills.SkillInstaller) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	availableSkills, err := installer.ListAvailableSkills(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(availableSkills) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(availableSkills))
	fmt.Println("--------------------")
	for _, skill := range availableSkills {
		fmt.Printf("  📦 %s\n", skill.Name)
		fmt.Printf("     %s\n", skill.Description)
		fmt.Printf("     Repo: %s\n", skill.Repository)
		if skill.Author != "" {
			fmt.Printf("     Author: %s\n", skill.Author)
		}
		if len(skill.Tags) > 0 {
			fmt.Printf("     Tags: %v\n", skill.Tags)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}
