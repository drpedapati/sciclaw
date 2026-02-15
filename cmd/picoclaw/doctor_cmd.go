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
	fmt.Printf("  %s doctor checks your sciClaw deployment, workspace, and key external tools (docx-review, irl, pandoc, PubMed CLI).\n", commandName)
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

	// Config + workspace
	configPath := getConfigPath()
	if fileExists(configPath) {
		add(doctorCheck{Name: "config", Status: doctorOK, Message: configPath})
	} else {
		add(doctorCheck{Name: "config", Status: doctorErr, Message: fmt.Sprintf("missing: %s", configPath)})
		// Without config, most checks are still useful but are best-effort.
	}

	cfg, cfgErr := loadConfig()
	if cfgErr != nil {
		add(doctorCheck{Name: "config.load", Status: doctorErr, Message: cfgErr.Error()})
	} else {
		workspace := cfg.WorkspacePath()
		if fileExists(workspace) {
			add(doctorCheck{Name: "workspace", Status: doctorOK, Message: workspace})
		} else {
			add(doctorCheck{Name: "workspace", Status: doctorErr, Message: fmt.Sprintf("missing: %s", workspace)})
		}

		// Channels
		if cfg.Channels.Telegram.Enabled {
			if strings.TrimSpace(cfg.Channels.Telegram.Token) == "" {
				add(doctorCheck{Name: "telegram", Status: doctorWarn, Message: "enabled but token is empty"})
			} else {
				add(doctorCheck{Name: "telegram", Status: doctorOK, Message: "enabled"})
			}
		} else {
			add(doctorCheck{Name: "telegram", Status: doctorSkip, Message: "disabled"})
		}

		if cfg.Agents.Defaults.RestrictToWorkspace {
			add(doctorCheck{Name: "agent.restrict_to_workspace", Status: doctorOK, Message: "true"})
		} else {
			add(doctorCheck{Name: "agent.restrict_to_workspace", Status: doctorWarn, Message: "false (tools can access outside workspace)"})
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
	add(checkBinary("docx-review", []string{"--version"}, 3*time.Second))
	// PubMed CLI is usually `pubmed` from `pubmed-cli` formula; accept either name.
	pubmed := checkBinary("pubmed", []string{"--help"}, 3*time.Second)
	pubmedcli := checkBinary("pubmed-cli", []string{"--help"}, 3*time.Second)
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

	add(checkBinary("irl", []string{"--version"}, 3*time.Second))
	add(checkBinary("pandoc", []string{"-v"}, 3*time.Second))
	add(checkBinary("rg", []string{"--version"}, 3*time.Second))
	add(checkBinary("python3", []string{"-V"}, 3*time.Second))

	// Skills sanity + sync
	if cfgErr == nil {
		workspaceSkillsDir := filepath.Join(cfg.WorkspacePath(), "skills")
		add(checkBaselineSkills(workspaceSkillsDir, opts))
	}

	// Gateway log quick scan: common Telegram 409 conflict from multiple instances.
	add(checkGatewayLog())

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

	if strings.Contains(tail, "409 \"Conflict: terminated by other getUpdates request") {
		return doctorCheck{Name: "gateway.telegram", Status: doctorErr, Message: "Telegram getUpdates 409 conflict (multiple bot instances running?)", Data: map[string]string{"log": p}}
	}
	return doctorCheck{Name: "gateway.log", Status: doctorOK, Message: p}
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
