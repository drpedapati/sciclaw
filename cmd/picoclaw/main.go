// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
	"github.com/sipeed/picoclaw/pkg/heartbeat"
	"github.com/sipeed/picoclaw/pkg/irl"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/migrate"
	"github.com/sipeed/picoclaw/pkg/models"
	"github.com/sipeed/picoclaw/pkg/providers"
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

	switch command {
	case "onboard":
		onboard()
	case "agent":
		agentCmd()
	case "gateway":
		gatewayCmd()
	case "service":
		serviceCmd()
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
	case "version", "--version", "-v":
		printVersion()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	commandName := invokedCLIName()
	fmt.Printf("%s %s - Paired Scientist Assistant v%s\n\n", logo, displayName, version)
	fmt.Printf("Primary command: %s\n", primaryCLIName)
	fmt.Printf("Compatibility alias: %s\n\n", cliName)
	fmt.Printf("Usage: %s <command>\n", commandName)
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  onboard     Initialize sciClaw configuration and workspace")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  models      Manage models (list, set, effort, status)")
	fmt.Println("  auth        Manage authentication (login, logout, status)")
	fmt.Println("  gateway     Start sciClaw gateway")
	fmt.Println("  service     Manage background gateway service (launchd/systemd)")
	fmt.Println("  channels    Setup and manage chat channels (Telegram, Discord, etc.)")
	fmt.Println("  status      Show sciClaw status")
	fmt.Println("  doctor      Check deployment health and dependencies")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  migrate     Migrate from OpenClaw to sciClaw (PicoClaw-compatible)")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  backup      Backup key sciClaw config/workspace files")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Println("Agent flags:")
	fmt.Println("  --model <model>   Override model for this invocation")
	fmt.Println("  --effort <level>  Set GPT-5.2 reasoning effort (none/minimal/low/medium/high/xhigh)")
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
		fmt.Println("  If you installed via Homebrew, Quarto, ImageMagick, IRL, ripgrep, docx-review, and pubmed-cli are installed automatically.")
	} else {
		fmt.Println("  If you installed via Homebrew, ImageMagick, IRL, ripgrep, docx-review, and pubmed-cli are installed automatically.")
		fmt.Println("  Install Quarto with: brew install --cask quarto")
	}
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
	if promptYesNo(r, "Set up messaging apps (Telegram/Discord/Slack) now?", false) {
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
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Clean(filepath.Join(exeDir, "..", "share", "sciclaw", "skills")),
			filepath.Clean(filepath.Join(exeDir, "..", "share", "picoclaw", "skills")),
		)
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
	fmt.Println("\nMigrate from OpenClaw to sciClaw (PicoClaw-compatible)")
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

	// Apply CLI overrides before provider creation
	if modelOverride != "" {
		cfg.Agents.Defaults.Model = modelOverride
	}
	if effortOverride != "" {
		cfg.Agents.Defaults.ReasoningEffort = effortOverride
	}

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
	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
			break
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

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

	go agentLoop.Run(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	heartbeatService.Stop()
	cronService.Stop()
	agentLoop.Stop()
	channelManager.StopAll(ctx)
	fmt.Println("✓ Gateway stopped")
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
			fmt.Println("  GPT-5.2 levels: none, minimal, low, medium, high, xhigh")
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
		fmt.Printf("Usage: %s models [list|set|effort|status]\n", commandName)
	}
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
	default:
		fmt.Printf("Unknown auth command: %s\n", os.Args[2])
		authHelp()
	}
}

func authHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nAuth commands:")
	fmt.Println("  login       Login via device code or paste token")
	fmt.Println("  logout      Remove stored credentials")
	fmt.Println("  status      Show current auth status")
	fmt.Println()
	fmt.Println("Login options:")
	fmt.Println("  --provider <name>    Provider to login with (openai, anthropic)")
	fmt.Println("  --device-code        Compatibility flag (OpenAI already uses device code by default)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s auth login --provider openai\n", commandName)
	fmt.Printf("  %s auth login --provider anthropic\n", commandName)
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
	fmt.Printf("  %s skills install sipeed/picoclaw-skills/weather\n", commandName)
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
		fmt.Printf("Example: %s skills install sipeed/picoclaw-skills/weather\n", commandName)
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
	builtinSkillsDir := "./picoclaw/skills"
	workspaceSkillsDir := filepath.Join(workspace, "skills")

	fmt.Printf("Copying builtin skills to workspace...\n")

	skillsToInstall := []string{
		"weather",
		"news",
		"stock",
		"calculator",
	}

	for _, skillName := range skillsToInstall {
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		workspacePath := filepath.Join(workspaceSkillsDir, skillName)

		if _, err := os.Stat(builtinPath); err != nil {
			fmt.Printf("⊘ Builtin skill '%s' not found: %v\n", skillName, err)
			continue
		}

		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}

		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
		}
	}

	fmt.Println("\n✓ All builtin skills installed!")
	fmt.Println("Now you can use them in your workspace.")
}

func skillsListBuiltinCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	builtinSkillsDir := filepath.Join(filepath.Dir(cfg.WorkspacePath()), "picoclaw", "skills")

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

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
		if entry.IsDir() {
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			description := "No description"
			if _, err := os.Stat(skillFile); err == nil {
				data, err := os.ReadFile(skillFile)
				if err == nil {
					content := string(data)
					if idx := strings.Index(content, "\n"); idx > 0 {
						firstLine := content[:idx]
						if strings.Contains(firstLine, "description:") {
							descLine := strings.Index(content[idx:], "\n")
							if descLine > 0 {
								description = strings.TrimSpace(content[idx+descLine : idx+descLine])
							}
						}
					}
				}
			}
			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("     %s\n", description)
			}
		}
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
