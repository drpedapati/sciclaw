package migrate

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/paths"
)

type ActionType int

const (
	ActionCopy ActionType = iota
	ActionSkip
	ActionBackup
	ActionConvertConfig
	ActionCreateDir
	ActionMergeConfig
	ActionMoveFile
	ActionMoveDir
	ActionRemoveDir
	ActionSymlink
	ActionRewriteConfig
)

type Options struct {
	DryRun        bool
	ConfigOnly    bool
	WorkspaceOnly bool
	Force         bool
	Refresh       bool
	Unify         bool
	OpenClawHome  string
	PicoClawHome  string
}

type Action struct {
	Type        ActionType
	Source      string
	Destination string
	Description string
}

type Result struct {
	FilesCopied    int
	FilesSkipped   int
	BackupsCreated int
	ConfigMigrated bool
	DirsCreated    int
	Warnings       []string
	Errors         []error
}

func Run(opts Options) (*Result, error) {
	if opts.Unify {
		return runUnify(opts)
	}
	if opts.ConfigOnly && opts.WorkspaceOnly {
		return nil, fmt.Errorf("--config-only and --workspace-only are mutually exclusive")
	}

	if opts.Refresh {
		opts.WorkspaceOnly = true
	}

	openclawHome, err := resolveOpenClawHome(opts.OpenClawHome)
	if err != nil {
		return nil, err
	}

	picoClawHome, err := resolvePicoClawHome(opts.PicoClawHome)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(openclawHome); os.IsNotExist(err) {
		return nil, fmt.Errorf("OpenClaw installation not found at %s", openclawHome)
	}

	actions, warnings, err := Plan(opts, openclawHome, picoClawHome)
	if err != nil {
		return nil, err
	}

	fmt.Println("Migrating from OpenClaw to PicoClaw")
	fmt.Printf("  Source:      %s\n", openclawHome)
	fmt.Printf("  Destination: %s\n", picoClawHome)
	fmt.Println()

	if opts.DryRun {
		PrintPlan(actions, warnings)
		return &Result{Warnings: warnings}, nil
	}

	if !opts.Force {
		PrintPlan(actions, warnings)
		if !Confirm() {
			fmt.Println("Aborted.")
			return &Result{Warnings: warnings}, nil
		}
		fmt.Println()
	}

	result := Execute(actions, openclawHome, picoClawHome)
	result.Warnings = warnings
	return result, nil
}

func runUnify(opts Options) (*Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolving home directory: %w", err)
	}
	oldDir := filepath.Join(home, ".picoclaw")
	newDir := filepath.Join(home, "sciclaw")
	oldCfg := filepath.Join(oldDir, "config.json")
	newCfg := filepath.Join(newDir, "config.json")

	oldInfo, oldErr := os.Lstat(oldDir)
	if oldErr != nil {
		return nil, fmt.Errorf("legacy directory not found: %s", oldDir)
	}
	if oldInfo.Mode()&os.ModeSymlink != 0 {
		return &Result{Warnings: []string{"~/.picoclaw is already a symlink; no action needed"}}, nil
	}
	if _, err := os.Stat(oldCfg); err != nil {
		return nil, fmt.Errorf("legacy config not found: %s", oldCfg)
	}
	if _, err := os.Stat(newDir); err != nil {
		return nil, fmt.Errorf("target workspace directory not found: %s", newDir)
	}

	files := []string{"config.json", "auth.json", "gateway.log", "gateway.err.log", "web.log", "gateway.status.json", "routing.reload", "jobs.json"}
	dirs := []struct{ from, to string }{
		{"backups", "backups"},
		{"templates", "templates"},
		{"skills", "global-skills"},
	}
	actions := make([]Action, 0, len(files)+len(dirs)+3)
	for _, f := range files {
		src := filepath.Join(oldDir, f)
		if _, err := os.Stat(src); err == nil {
			actions = append(actions, Action{Type: ActionMoveFile, Source: src, Destination: filepath.Join(newDir, f)})
		}
	}
	for _, d := range dirs {
		src := filepath.Join(oldDir, d.from)
		if _, err := os.Stat(src); err == nil {
			actions = append(actions, Action{Type: ActionMoveDir, Source: src, Destination: filepath.Join(newDir, d.to)})
		}
	}
	if _, err := os.Stat(newCfg); err == nil {
		actions = append(actions, Action{Type: ActionRewriteConfig, Source: newCfg, Destination: newCfg})
	}
	actions = append(actions,
		Action{Type: ActionRemoveDir, Source: oldDir, Destination: oldDir},
		Action{Type: ActionSymlink, Source: newDir, Destination: oldDir},
	)

	fmt.Println("Unifying sciClaw directories")
	fmt.Printf("  Legacy: %s\n", oldDir)
	fmt.Printf("  Target: %s\n\n", newDir)

	if opts.DryRun {
		PrintPlan(actions, nil)
		return &Result{}, nil
	}
	if !opts.Force {
		PrintPlan(actions, nil)
		if !Confirm() {
			fmt.Println("Aborted.")
			return &Result{}, nil
		}
	}

	result := &Result{}
	for _, action := range actions {
		switch action.Type {
		case ActionMoveFile:
			if err := os.MkdirAll(filepath.Dir(action.Destination), 0o755); err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
			if err := copyFile(action.Source, action.Destination); err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
			result.FilesCopied++
		case ActionMoveDir:
			if err := copyDir(action.Source, action.Destination); err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
		case ActionRewriteConfig:
			if err := rewriteUnifiedConfigPath(action.Source); err != nil {
				result.Errors = append(result.Errors, err)
			}
		case ActionRemoveDir:
			if err := os.RemoveAll(action.Source); err != nil {
				result.Errors = append(result.Errors, err)
			}
		case ActionSymlink:
			if err := os.Symlink(action.Source, action.Destination); err != nil {
				result.Errors = append(result.Errors, err)
			}
		}
	}
	return result, nil
}

func Plan(opts Options, openclawHome, picoClawHome string) ([]Action, []string, error) {
	var actions []Action
	var warnings []string

	force := opts.Force || opts.Refresh

	if !opts.WorkspaceOnly {
		configPath, err := findOpenClawConfig(openclawHome)
		if err != nil {
			if opts.ConfigOnly {
				return nil, nil, err
			}
			warnings = append(warnings, fmt.Sprintf("Config migration skipped: %v", err))
		} else {
			actions = append(actions, Action{
				Type:        ActionConvertConfig,
				Source:      configPath,
				Destination: filepath.Join(picoClawHome, "config.json"),
				Description: "convert OpenClaw config to PicoClaw format",
			})

			data, err := LoadOpenClawConfig(configPath)
			if err == nil {
				_, configWarnings, _ := ConvertConfig(data)
				warnings = append(warnings, configWarnings...)
			}
		}
	}

	if !opts.ConfigOnly {
		srcWorkspace := resolveWorkspace(openclawHome)
		dstWorkspace := resolveWorkspace(picoClawHome)

		if _, err := os.Stat(srcWorkspace); err == nil {
			wsActions, err := PlanWorkspaceMigration(srcWorkspace, dstWorkspace, force)
			if err != nil {
				return nil, nil, fmt.Errorf("planning workspace migration: %w", err)
			}
			actions = append(actions, wsActions...)
		} else {
			warnings = append(warnings, "OpenClaw workspace directory not found, skipping workspace migration")
		}
	}

	return actions, warnings, nil
}

func Execute(actions []Action, openclawHome, picoClawHome string) *Result {
	result := &Result{}

	for _, action := range actions {
		switch action.Type {
		case ActionConvertConfig:
			if err := executeConfigMigration(action.Source, action.Destination, picoClawHome); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("config migration: %w", err))
				fmt.Printf("  ✗ Config migration failed: %v\n", err)
			} else {
				result.ConfigMigrated = true
				fmt.Printf("  ✓ Converted config: %s\n", action.Destination)
			}
		case ActionCreateDir:
			if err := os.MkdirAll(action.Destination, 0755); err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				result.DirsCreated++
			}
		case ActionBackup:
			bakPath := action.Destination + ".bak"
			if err := copyFile(action.Destination, bakPath); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("backup %s: %w", action.Destination, err))
				fmt.Printf("  ✗ Backup failed: %s\n", action.Destination)
				continue
			}
			result.BackupsCreated++
			fmt.Printf("  ✓ Backed up %s -> %s.bak\n", filepath.Base(action.Destination), filepath.Base(action.Destination))

			if err := os.MkdirAll(filepath.Dir(action.Destination), 0755); err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
			if err := copyFile(action.Source, action.Destination); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("copy %s: %w", action.Source, err))
				fmt.Printf("  ✗ Copy failed: %s\n", action.Source)
			} else {
				result.FilesCopied++
				fmt.Printf("  ✓ Copied %s\n", relPath(action.Source, openclawHome))
			}
		case ActionCopy:
			if err := os.MkdirAll(filepath.Dir(action.Destination), 0755); err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}
			if err := copyFile(action.Source, action.Destination); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("copy %s: %w", action.Source, err))
				fmt.Printf("  ✗ Copy failed: %s\n", action.Source)
			} else {
				result.FilesCopied++
				fmt.Printf("  ✓ Copied %s\n", relPath(action.Source, openclawHome))
			}
		case ActionSkip:
			result.FilesSkipped++
		}
	}

	return result
}

func executeConfigMigration(srcConfigPath, dstConfigPath, picoClawHome string) error {
	data, err := LoadOpenClawConfig(srcConfigPath)
	if err != nil {
		return err
	}

	incoming, _, err := ConvertConfig(data)
	if err != nil {
		return err
	}

	if _, err := os.Stat(dstConfigPath); err == nil {
		existing, err := config.LoadConfig(dstConfigPath)
		if err != nil {
			return fmt.Errorf("loading existing PicoClaw config: %w", err)
		}
		incoming = MergeConfig(existing, incoming)
	}

	if err := os.MkdirAll(filepath.Dir(dstConfigPath), 0755); err != nil {
		return err
	}
	return config.SaveConfig(dstConfigPath, incoming)
}

func Confirm() bool {
	fmt.Print("Proceed with migration? (y/n): ")
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(strings.TrimSpace(response)) == "y"
}

func PrintPlan(actions []Action, warnings []string) {
	fmt.Println("Planned actions:")
	copies := 0
	skips := 0
	backups := 0
	configCount := 0

	for _, action := range actions {
		switch action.Type {
		case ActionConvertConfig:
			fmt.Printf("  [config]  %s -> %s\n", action.Source, action.Destination)
			configCount++
		case ActionCopy:
			fmt.Printf("  [copy]    %s\n", filepath.Base(action.Source))
			copies++
		case ActionBackup:
			fmt.Printf("  [backup]  %s (exists, will backup and overwrite)\n", filepath.Base(action.Destination))
			backups++
			copies++
		case ActionSkip:
			if action.Description != "" {
				fmt.Printf("  [skip]    %s (%s)\n", filepath.Base(action.Source), action.Description)
			}
			skips++
		case ActionCreateDir:
			fmt.Printf("  [mkdir]   %s\n", action.Destination)
		case ActionMoveFile:
			fmt.Printf("  [move]    %s -> %s\n", action.Source, action.Destination)
		case ActionMoveDir:
			fmt.Printf("  [move]    %s/ -> %s/\n", action.Source, action.Destination)
		case ActionRemoveDir:
			fmt.Printf("  [remove]  %s\n", action.Source)
		case ActionSymlink:
			fmt.Printf("  [link]    %s -> %s\n", action.Destination, action.Source)
		case ActionRewriteConfig:
			fmt.Printf("  [rewrite] %s (workspace path)\n", action.Source)
		}
	}

	if len(warnings) > 0 {
		fmt.Println()
		fmt.Println("Warnings:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
	}

	fmt.Println()
	fmt.Printf("%d files to copy, %d configs to convert, %d backups needed, %d skipped\n",
		copies, configCount, backups, skips)
}

func PrintSummary(result *Result) {
	fmt.Println()
	parts := []string{}
	if result.FilesCopied > 0 {
		parts = append(parts, fmt.Sprintf("%d files copied", result.FilesCopied))
	}
	if result.ConfigMigrated {
		parts = append(parts, "1 config converted")
	}
	if result.BackupsCreated > 0 {
		parts = append(parts, fmt.Sprintf("%d backups created", result.BackupsCreated))
	}
	if result.FilesSkipped > 0 {
		parts = append(parts, fmt.Sprintf("%d files skipped", result.FilesSkipped))
	}

	if len(parts) > 0 {
		fmt.Printf("Migration complete! %s.\n", strings.Join(parts, ", "))
	} else {
		fmt.Println("Migration complete! No actions taken.")
	}

	if len(result.Errors) > 0 {
		fmt.Println()
		fmt.Printf("%d errors occurred:\n", len(result.Errors))
		for _, e := range result.Errors {
			fmt.Printf("  - %v\n", e)
		}
	}
}

func resolveOpenClawHome(override string) (string, error) {
	if override != "" {
		return expandHome(override), nil
	}
	if envHome := os.Getenv("OPENCLAW_HOME"); envHome != "" {
		return expandHome(envHome), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw"), nil
}

func resolvePicoClawHome(override string) (string, error) {
	if override != "" {
		return expandHome(override), nil
	}
	if envHome := os.Getenv("PICOCLAW_HOME"); envHome != "" {
		return expandHome(envHome), nil
	}
	return paths.AppHome(), nil
}

func resolveWorkspace(homeDir string) string {
	return filepath.Join(homeDir, "workspace")
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}

func backupFile(path string) error {
	bakPath := path + ".bak"
	return copyFile(path, bakPath)
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		return copyFile(path, target)
	})
}

func rewriteUnifiedConfigPath(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	agents, ok := raw["agents"].(map[string]interface{})
	if !ok {
		return nil
	}
	defaults, ok := agents["defaults"].(map[string]interface{})
	if !ok {
		return nil
	}
	workspace, _ := defaults["workspace"].(string)
	if strings.Contains(workspace, "~/.picoclaw/workspace") {
		defaults["workspace"] = "~/sciclaw"
		updated, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(configPath, updated, 0o644)
	}
	return nil
}

func relPath(path, base string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}
