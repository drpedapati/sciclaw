package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
	svcmgr "github.com/sipeed/picoclaw/pkg/service"
)

type doctorOptions struct {
	JSON    bool
	Fix     bool
	Verbose bool
}

type doctorCheckStatus string

const (
	doctorOK   doctorCheckStatus = "ok"
	doctorWarn doctorCheckStatus = "warn"
	doctorErr  doctorCheckStatus = "error"
	doctorSkip doctorCheckStatus = "skip"
)

type doctorCheck struct {
	Name    string            `json:"name"`
	Status  doctorCheckStatus `json:"status"`
	Message string            `json:"message,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

type doctorReport struct {
	CLI       string        `json:"cli"`
	Version   string        `json:"version"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Timestamp string        `json:"timestamp"`
	Checks    []doctorCheck `json:"checks"`
}

func doctorCmd() {
	opts, showHelp, err := parseDoctorOptions(os.Args[2:])
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		doctorHelp()
		os.Exit(2)
	}
	if showHelp {
		doctorHelp()
		return
	}

	rep := runDoctor(opts)
	if opts.JSON {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(b))
	} else {
		printDoctorReport(rep)
	}

	// Exit non-zero if any hard error.
	for _, c := range rep.Checks {
		if c.Status == doctorErr {
			os.Exit(1)
		}
	}
}

func doctorHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nDoctor:")
	fmt.Printf("  %s doctor checks your sciClaw deployment, workspace, service health, and key external tools (docx-review, quarto, irl, pandoc, PubMed CLI).\n", commandName)
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --json        Machine-readable output")
	fmt.Println("  --fix         Apply safe fixes (sync baseline skills, remove legacy skill names when possible)")
	fmt.Println("  --verbose     Include extra details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s doctor\n", commandName)
	fmt.Printf("  %s doctor --fix\n", commandName)
	fmt.Printf("  %s doctor --json\n", commandName)
	fmt.Printf("  (Compatibility alias also works: %s)\n", cliName)
}

func parseDoctorOptions(args []string) (doctorOptions, bool, error) {
	opts := doctorOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			opts.JSON = true
		case "--fix":
			opts.Fix = true
		case "--verbose", "-v":
			opts.Verbose = true
		case "help", "--help", "-h":
			return opts, true, nil
		default:
			return opts, false, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return opts, false, nil
}

func runDoctor(opts doctorOptions) doctorReport {
	rep := doctorReport{
		CLI:       invokedCLIName(),
		Version:   version,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	add := func(c doctorCheck) {
		rep.Checks = append(rep.Checks, c)
	}

	workspacePath := ""

	// Config + workspace
	configPath := getConfigPath()
	configExists := fileExists(configPath)
	if configExists {
		add(doctorCheck{Name: "config", Status: doctorOK, Message: configPath})
	} else {
		add(doctorCheck{
			Name:    "config",
			Status:  doctorWarn,
			Message: fmt.Sprintf("missing: %s (run: %s onboard --yes)", configPath, invokedCLIName()),
		})
	}

	var cfg *config.Config
	var cfgErr error
	if configExists {
		cfg, cfgErr = loadConfig()
		if cfgErr != nil {
			add(doctorCheck{Name: "config.load", Status: doctorErr, Message: cfgErr.Error()})
		}
	}

	if cfgErr == nil && cfg != nil {
		workspace := cfg.WorkspacePath()
		workspacePath = workspace
		if fileExists(workspace) {
			add(doctorCheck{Name: "workspace", Status: doctorOK, Message: workspace})
		} else {
			add(doctorCheck{
				Name:    "workspace",
				Status:  doctorWarn,
				Message: fmt.Sprintf("missing: %s (run: %s onboard --yes)", workspace, invokedCLIName()),
			})
		}

		// Channels
		if cfg.Channels.Telegram.Enabled {
			if strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
				add(doctorCheck{Name: "telegram", Status: doctorWarn, Message: "enabled but token is empty"})
			} else {
				add(doctorCheck{Name: "telegram", Status: doctorOK, Message: "enabled"})
				if len(cfg.Channels.Telegram.AllowFrom) == 0 {
					add(doctorCheck{Name: "telegram.allow_from", Status: doctorWarn, Message: "empty (any Telegram user can talk to your bot); run: sciclaw channels setup telegram"})
				} else {
					add(doctorCheck{Name: "telegram.allow_from", Status: doctorOK, Message: fmt.Sprintf("%d entries", len(cfg.Channels.Telegram.AllowFrom))})
				}
			}
		} else {
			add(doctorCheck{Name: "telegram", Status: doctorSkip, Message: "disabled"})
		}

		if cfg.Channels.Discord.Enabled {
			if strings.TrimSpace(cfg.Channels.Discord.Token) == "" {
				add(doctorCheck{Name: "discord", Status: doctorWarn, Message: "enabled but token is empty"})
			} else {
				add(doctorCheck{Name: "discord", Status: doctorOK, Message: "enabled"})
				if len(cfg.Channels.Discord.AllowFrom) == 0 {
					add(doctorCheck{Name: "discord.allow_from", Status: doctorWarn, Message: "empty (any Discord user can talk to your bot); run: sciclaw channels setup discord"})
				} else {
					add(doctorCheck{Name: "discord.allow_from", Status: doctorOK, Message: fmt.Sprintf("%d entries", len(cfg.Channels.Discord.AllowFrom))})
				}
			}
		} else {
			add(doctorCheck{Name: "discord", Status: doctorSkip, Message: "disabled"})
		}

		if cfg.Agents.Defaults.RestrictToWorkspace {
			add(doctorCheck{Name: "agent.restrict_to_workspace", Status: doctorOK, Message: "true"})
		} else {
			add(doctorCheck{Name: "agent.restrict_to_workspace", Status: doctorWarn, Message: "false (tools can access outside workspace)"})
		}

		// Optional: PubMed API key (improves rate limits).
		if strings.TrimSpace(cfg.Tools.PubMed.APIKey) != "" || strings.TrimSpace(os.Getenv("NCBI_API_KEY")) != "" {
			add(doctorCheck{Name: "pubmed.api_key", Status: doctorOK, Message: "set"})
		} else {
			add(doctorCheck{Name: "pubmed.api_key", Status: doctorWarn, Message: "not set (optional, improves PubMed rate limits)"})
		}
	} else if !configExists {
		// Best-effort workspace check with default path.
		home, _ := os.UserHomeDir()
		if home != "" {
			defaultWorkspace := filepath.Join(home, "sciclaw")
			workspacePath = defaultWorkspace
			if fileExists(defaultWorkspace) {
				add(doctorCheck{Name: "workspace", Status: doctorOK, Message: defaultWorkspace})
			} else {
				add(doctorCheck{
					Name:    "workspace",
					Status:  doctorWarn,
					Message: fmt.Sprintf("missing: %s (run: %s onboard --yes)", defaultWorkspace, invokedCLIName()),
				})
			}
		}
	}

	// Auth store
	store, err := auth.LoadStore()
	if err != nil {
		add(doctorCheck{Name: "auth.store", Status: doctorWarn, Message: err.Error()})
	} else if store == nil || len(store.Credentials) == 0 {
		add(doctorCheck{Name: "auth.store", Status: doctorWarn, Message: "no credentials stored"})
	} else {
		// Prefer openai oauth signal since that's your primary path.
		cred, ok := store.Credentials["openai"]
		if !ok {
			add(doctorCheck{Name: "auth.openai", Status: doctorWarn, Message: "missing"})
		} else {
			st := doctorOK
			msg := "authenticated"
			if cred.IsExpired() {
				st, msg = doctorErr, "expired"
			} else if cred.NeedsRefresh() {
				st, msg = doctorWarn, "needs refresh"
			}
			add(doctorCheck{Name: "auth.openai", Status: st, Message: fmt.Sprintf("%s (%s)", msg, cred.AuthMethod)})
		}
	}

	// Key external CLIs
	add(checkBinaryWithHint("docx-review", []string{"--version"}, 3*time.Second, "brew tap drpedapati/tap && brew install sciclaw-docx-review"))
	quartoHint := "brew install --cask quarto"
	if runtime.GOOS == "linux" {
		quartoHint = "brew tap drpedapati/tap && brew install sciclaw-quarto"
	}
	add(checkBinaryWithHint("quarto", []string{"--version"}, 3*time.Second, quartoHint))
	// PubMed CLI is usually `pubmed` from `pubmed-cli` formula; accept either name.
	pubmed := checkBinary("pubmed", []string{"--help"}, 3*time.Second)
	pubmedcli := checkBinaryWithHint("pubmed-cli", []string{"--help"}, 3*time.Second, "brew tap drpedapati/tap && brew install sciclaw-pubmed-cli")
	if pubmedcli.Status == doctorOK {
		add(pubmedcli)
	} else if pubmed.Status == doctorOK {
		pubmed.Name = "pubmed-cli"
		pubmed.Status = doctorWarn
		pubmed.Message = "found `pubmed` but not `pubmed-cli` (consider adding a shim/symlink)"
		if opts.Fix {
			if err := tryCreatePubmedCLIShim(); err == nil {
				pubmed.Status = doctorOK
				pubmed.Message = "shimmed `pubmed-cli` -> `pubmed`"
			} else {
				pubmed.Data = map[string]string{"fix_error": err.Error()}
			}
		}
		add(pubmed)
	} else {
		add(pubmedcli)
	}

	add(checkBinaryWithHint("irl", []string{"--version"}, 3*time.Second, "brew install irl"))
	add(checkBinaryWithHint("pandoc", []string{"-v"}, 3*time.Second, "brew install pandoc"))
	add(checkBinaryWithHint("rg", []string{"--version"}, 3*time.Second, "brew install ripgrep"))
	if runtime.GOOS == "linux" {
		add(checkBinaryWithHint("uv", []string{"--version"}, 3*time.Second, "brew install uv"))
	}
	add(checkBinaryWithHint("python3", []string{"-V"}, 3*time.Second, "install python3 (e.g. Homebrew, python.org, or system package manager)"))
	if runtime.GOOS == "linux" && strings.TrimSpace(workspacePath) != "" {
		add(checkWorkspacePythonVenv(workspacePath, opts))
	}

	// Skills sanity + sync
	if cfgErr == nil {
		if cfg != nil {
			workspaceSkillsDir := filepath.Join(cfg.WorkspacePath(), "skills")
			add(checkBaselineSkills(workspaceSkillsDir, opts))
		}
	}

	// Gateway log quick scan: common Telegram 409 conflict from multiple instances.
	add(checkGatewayLog())
	for _, c := range checkServiceStatus() {
		add(c)
	}

	// Optional: Homebrew outdated status (best-effort).
	add(checkHomebrewOutdated())

	// Stable output order.
	sort.SliceStable(rep.Checks, func(i, j int) bool { return rep.Checks[i].Name < rep.Checks[j].Name })
	return rep
}

func printDoctorReport(rep doctorReport) {
	fmt.Printf("%s %s Doctor (%s)\n\n", logo, displayName, rep.CLI)
	fmt.Printf("Version: %s\n", rep.Version)
	fmt.Printf("OS/Arch: %s/%s\n", rep.OS, rep.Arch)
	fmt.Printf("Time: %s\n\n", rep.Timestamp)

	// Group by severity.
	for _, st := range []doctorCheckStatus{doctorErr, doctorWarn, doctorOK, doctorSkip} {
		title := map[doctorCheckStatus]string{doctorErr: "Errors", doctorWarn: "Warnings", doctorOK: "OK", doctorSkip: "Skipped"}[st]
		any := false
		for _, c := range rep.Checks {
			if c.Status != st {
				continue
			}
			if !any {
				fmt.Println(title + ":")
				any = true
			}
			mark := map[doctorCheckStatus]string{doctorErr: "✗", doctorWarn: "!", doctorOK: "✓", doctorSkip: "-"}[st]
			if c.Message != "" {
				fmt.Printf("  %s %s: %s\n", mark, c.Name, c.Message)
			} else {
				fmt.Printf("  %s %s\n", mark, c.Name)
			}
			if len(c.Data) > 0 {
				keys := make([]string, 0, len(c.Data))
				for k := range c.Data {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					fmt.Printf("    %s=%s\n", k, c.Data[k])
				}
			}
		}
		if any {
			fmt.Println()
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func checkBinaryWithHint(name string, args []string, timeout time.Duration, installHint string) doctorCheck {
	c := checkBinary(name, args, timeout)
	if c.Status == doctorErr && installHint != "" {
		if c.Data == nil {
			c.Data = map[string]string{}
		}
		// Only mention brew if it is present; otherwise keep the message generic.
		if findBrew() != "" || strings.HasPrefix(installHint, "install ") {
			c.Data["install_hint"] = installHint
		}
	}
	return c
}

func checkBinary(name string, args []string, timeout time.Duration) doctorCheck {
	p, err := exec.LookPath(name)
	if err != nil {
		return doctorCheck{Name: name, Status: doctorErr, Message: "not found in PATH"}
	}
	c := doctorCheck{Name: name, Status: doctorOK, Message: p}

	if len(args) == 0 {
		return c
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, args...)
	// Avoid blocking on tools that write lots of help output.
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			c.Status = doctorWarn
			c.Message = fmt.Sprintf("%s (timeout)", p)
			return c
		}
		c.Status = doctorWarn
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		c.Data = map[string]string{"run_error": truncateOneLine(msg, 220)}
		return c
	}

	out := firstNonEmptyLine(stdout.String())
	if out == "" {
		out = firstNonEmptyLine(stderr.String())
	}
	if out != "" {
		if c.Data == nil {
			c.Data = map[string]string{}
		}
		c.Data["output"] = truncateOneLine(out, 180)
	}
	return c
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func truncateOneLine(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func checkWorkspacePythonVenv(workspace string, opts doctorOptions) doctorCheck {
	venvPython := workspaceVenvPythonPath(workspace)
	venvDir := workspaceVenvDir(workspace)
	data := map[string]string{
		"workspace": workspace,
		"venv":      venvDir,
	}

	validate := func() error {
		if !fileExists(venvPython) {
			return fmt.Errorf("missing venv python: %s", venvPython)
		}
		code := "import requests, bs4, docx, lxml, yaml"
		out, err := runCommandWithOutput(8*time.Second, venvPython, "-c", code)
		if err != nil {
			return fmt.Errorf("%s", truncateOneLine(out, 220))
		}
		return nil
	}

	if err := validate(); err == nil {
		return doctorCheck{Name: "python.venv", Status: doctorOK, Message: "workspace venv ready", Data: data}
	}

	if opts.Fix {
		venvBin, err := ensureWorkspacePythonEnvironment(workspace)
		if err == nil {
			data["venv_bin"] = venvBin
			if err2 := validate(); err2 == nil {
				return doctorCheck{Name: "python.venv", Status: doctorOK, Message: "workspace venv bootstrapped", Data: data}
			}
		}
		if err != nil {
			data["fix_error"] = err.Error()
		}
	}

	data["hint"] = fmt.Sprintf("run: %s doctor --fix (or %s onboard)", invokedCLIName(), invokedCLIName())
	return doctorCheck{
		Name:    "python.venv",
		Status:  doctorWarn,
		Message: "workspace Python venv missing/incomplete",
		Data:    data,
	}
}

func checkGatewayLog() doctorCheck {
	home, err := os.UserHomeDir()
	if err != nil {
		return doctorCheck{Name: "gateway.log", Status: doctorSkip, Message: "home directory unavailable"}
	}
	p := filepath.Join(home, ".picoclaw", "gateway.log")
	if !fileExists(p) {
		return doctorCheck{Name: "gateway.log", Status: doctorSkip, Message: "not found"}
	}

	tail, err := readTail(p, 128*1024)
	if err != nil {
		return doctorCheck{Name: "gateway.log", Status: doctorWarn, Message: p, Data: map[string]string{"read_error": err.Error()}}
	}

	conflictNeedle := "409 \"Conflict: terminated by other getUpdates request"
	connectNeedle := "telegram: Telegram bot connected"
	conflictAt := strings.LastIndex(tail, conflictNeedle)
	connectedAt := strings.LastIndex(tail, connectNeedle)
	// Only treat 409 as a current error if it appears after the last successful connect in the log tail.
	if conflictAt >= 0 && (connectedAt < 0 || connectedAt < conflictAt) {
		return doctorCheck{Name: "gateway.telegram", Status: doctorErr, Message: "Telegram getUpdates 409 conflict (multiple bot instances running?)", Data: map[string]string{"log": p}}
	}
	return doctorCheck{Name: "gateway.log", Status: doctorOK, Message: p}
}

func checkServiceStatus() []doctorCheck {
	checks := make([]doctorCheck, 0, 4)
	add := func(c doctorCheck) { checks = append(checks, c) }

	exePath, err := os.Executable()
	if err != nil {
		add(doctorCheck{Name: "service.backend", Status: doctorWarn, Message: "unable to resolve executable path", Data: map[string]string{"error": err.Error()}})
		return checks
	}

	mgr, err := svcmgr.NewManager(exePath)
	if err != nil {
		add(doctorCheck{Name: "service.backend", Status: doctorWarn, Message: "unable to initialize service manager", Data: map[string]string{"error": err.Error()}})
		return checks
	}

	st, err := mgr.Status()
	if err != nil {
		add(doctorCheck{Name: "service.backend", Status: doctorWarn, Message: "service status check failed", Data: map[string]string{"error": err.Error()}})
		return checks
	}

	backendStatus := doctorOK
	if st.Backend == svcmgr.BackendUnsupported {
		backendStatus = doctorSkip
	}
	add(doctorCheck{Name: "service.backend", Status: backendStatus, Message: st.Backend})

	if st.Backend == svcmgr.BackendUnsupported {
		msg := st.Detail
		if strings.TrimSpace(msg) == "" {
			msg = "service backend unavailable on this platform"
		}
		add(doctorCheck{Name: "service.installed", Status: doctorSkip, Message: msg})
		add(doctorCheck{Name: "service.running", Status: doctorSkip, Message: msg})
		add(doctorCheck{Name: "service.enabled", Status: doctorSkip, Message: msg})
		return checks
	}

	if st.Installed {
		add(doctorCheck{Name: "service.installed", Status: doctorOK, Message: "installed"})
	} else {
		add(doctorCheck{Name: "service.installed", Status: doctorWarn, Message: fmt.Sprintf("not installed (run: %s service install)", invokedCLIName())})
	}

	if !st.Installed {
		add(doctorCheck{Name: "service.running", Status: doctorSkip, Message: "service is not installed"})
	} else if st.Running {
		add(doctorCheck{Name: "service.running", Status: doctorOK, Message: "running"})
	} else {
		add(doctorCheck{Name: "service.running", Status: doctorWarn, Message: fmt.Sprintf("not running (run: %s service start)", invokedCLIName())})
	}

	if !st.Installed {
		add(doctorCheck{Name: "service.enabled", Status: doctorSkip, Message: "service is not installed"})
	} else if st.Enabled {
		add(doctorCheck{Name: "service.enabled", Status: doctorOK, Message: "enabled"})
	} else {
		add(doctorCheck{Name: "service.enabled", Status: doctorWarn, Message: fmt.Sprintf("not enabled (run: %s service install)", invokedCLIName())})
	}

	if strings.TrimSpace(st.Detail) != "" {
		add(doctorCheck{Name: "service.detail", Status: doctorSkip, Message: st.Detail})
	}
	return checks
}

func readTail(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	if size <= maxBytes {
		b, err := io.ReadAll(f)
		return string(b), err
	}
	// Seek to last maxBytes.
	if _, err := f.Seek(size-maxBytes, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(f)
	return string(b), err
}

func checkHomebrewOutdated() doctorCheck {
	brewPath := findBrew()
	if brewPath == "" {
		return doctorCheck{Name: "homebrew", Status: doctorSkip, Message: "brew not found"}
	}

	// `brew outdated --quiet sciclaw` prints name if outdated, nothing if up-to-date.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, brewPath, "outdated", "--quiet", "sciclaw")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	s := strings.TrimSpace(out.String())
	if s == "" {
		return doctorCheck{Name: "homebrew.sciclaw", Status: doctorOK, Message: "not outdated"}
	}
	return doctorCheck{Name: "homebrew.sciclaw", Status: doctorWarn, Message: "outdated (run: brew upgrade sciclaw)"}
}

func findBrew() string {
	// PATH first
	if p, err := exec.LookPath("brew"); err == nil {
		return p
	}
	// Common locations
	for _, p := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func checkBaselineSkills(workspaceSkillsDir string, opts doctorOptions) doctorCheck {
	if !fileExists(workspaceSkillsDir) {
		return doctorCheck{Name: "skills.workspace", Status: doctorWarn, Message: fmt.Sprintf("missing: %s", workspaceSkillsDir)}
	}

	missing := []string{}
	for _, name := range baselineScienceSkillNames {
		if !fileExists(filepath.Join(workspaceSkillsDir, name, "SKILL.md")) {
			missing = append(missing, name)
		}
	}

	legacy := []string{}
	for _, name := range []string{"docx", "pubmed-database"} {
		if fileExists(filepath.Join(workspaceSkillsDir, name, "SKILL.md")) {
			legacy = append(legacy, name)
		}
	}

	// Best-effort: if installed via Homebrew, we can locate bundled skills beside the executable.
	shareSkills := resolveBundledSkillsDir()
	data := map[string]string{"workspace": workspaceSkillsDir}
	if shareSkills != "" {
		data["bundled"] = shareSkills
	}

	if opts.Fix {
		if shareSkills != "" {
			// Sync baseline skills only; do not delete user-added skills.
			_ = syncBaselineSkills(shareSkills, workspaceSkillsDir)
			// Remove legacy skill directories that cause ambiguity.
			_ = os.RemoveAll(filepath.Join(workspaceSkillsDir, "docx"))
			_ = os.RemoveAll(filepath.Join(workspaceSkillsDir, "pubmed-database"))
		}
		// Recompute after fix.
		missing = missing[:0]
		for _, name := range baselineScienceSkillNames {
			if !fileExists(filepath.Join(workspaceSkillsDir, name, "SKILL.md")) {
				missing = append(missing, name)
			}
		}
		legacy = legacy[:0]
		for _, name := range []string{"docx", "pubmed-database"} {
			if fileExists(filepath.Join(workspaceSkillsDir, name, "SKILL.md")) {
				legacy = append(legacy, name)
			}
		}
	}

	if len(missing) == 0 && len(legacy) == 0 {
		return doctorCheck{Name: "skills.baseline", Status: doctorOK, Message: "baseline present", Data: data}
	}

	msgParts := []string{}
	if len(missing) > 0 {
		msgParts = append(msgParts, fmt.Sprintf("missing: %s", strings.Join(missing, ", ")))
	}
	if len(legacy) > 0 {
		msgParts = append(msgParts, fmt.Sprintf("legacy present: %s", strings.Join(legacy, ", ")))
	}
	st := doctorWarn
	if len(missing) > 0 {
		st = doctorErr
	}
	if shareSkills == "" {
		data["hint"] = "bundled skills dir not detected; run onboard or re-install skills"
	} else {
		data["hint"] = "run: sciclaw doctor --fix"
	}
	return doctorCheck{Name: "skills.baseline", Status: st, Message: strings.Join(msgParts, "; "), Data: data}
}

func resolveBundledSkillsDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	realExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		realExe = exe
	}
	// Typical Homebrew layout: .../Cellar/sciclaw/<ver>/bin/sciclaw
	// Share dir: .../Cellar/sciclaw/<ver>/share/sciclaw/skills
	share := filepath.Clean(filepath.Join(filepath.Dir(realExe), "..", "share", primaryCLIName, "skills"))
	if fileExists(share) {
		return share
	}
	return ""
}

func syncBaselineSkills(srcSkillsDir, dstSkillsDir string) error {
	// Only sync baseline skills to keep user-installed extras intact.
	for _, name := range baselineScienceSkillNames {
		src := filepath.Join(srcSkillsDir, name)
		dst := filepath.Join(dstSkillsDir, name)
		if !fileExists(filepath.Join(src, "SKILL.md")) {
			continue
		}
		_ = os.RemoveAll(dst)
		if err := copyDirectory(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func tryCreatePubmedCLIShim() error {
	// Best-effort: if `pubmed` exists and `pubmed-cli` does not, create a symlink next to it.
	pubmedPath, err := exec.LookPath("pubmed")
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("pubmed-cli"); err == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		return fmt.Errorf("shim not supported on windows")
	}
	dir := filepath.Dir(pubmedPath)
	link := filepath.Join(dir, "pubmed-cli")
	// If link exists but is broken, replace.
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	}
	if err := os.Symlink(filepath.Base(pubmedPath), link); err != nil {
		// Retry with absolute target.
		_ = os.Remove(link)
		if err2 := os.Symlink(pubmedPath, link); err2 != nil {
			return err2
		}
	}
	return nil
}
