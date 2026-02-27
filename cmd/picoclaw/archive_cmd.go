package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sipeed/picoclaw/pkg/archive/discordarchive"
	"github.com/sipeed/picoclaw/pkg/session"
)

type archiveRunOptions struct {
	SessionKey string
	All        bool
	OverLimit  bool
	DryRun     bool
}

type archiveRecallOptions struct {
	SessionKey string
	TopK       int
	MaxChars   int
	JSON       bool
	Query      string
}

func archiveCmd() {
	if len(os.Args) < 3 {
		archiveHelp()
		return
	}
	switch strings.ToLower(strings.TrimSpace(os.Args[2])) {
	case "discord":
		archiveDiscordCmd(os.Args[3:])
	case "help", "--help", "-h":
		archiveHelp()
	default:
		fmt.Printf("Unknown archive command: %s\n", os.Args[2])
		archiveHelp()
	}
}

func archiveDiscordCmd(args []string) {
	if len(args) == 0 {
		archiveDiscordHelp()
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	workspace := cfg.WorkspacePath()
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	manager := discordarchive.NewManager(workspace, sm, cfg.Channels.Discord.Archive)

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	switch sub {
	case "list":
		overLimitOnly := false
		jsonOut := false
		for _, arg := range args[1:] {
			switch strings.ToLower(strings.TrimSpace(arg)) {
			case "--over-limit":
				overLimitOnly = true
			case "--json":
				jsonOut = true
			}
		}
		stats := manager.ListDiscordSessions(overLimitOnly)
		if jsonOut {
			out, _ := json.MarshalIndent(stats, "", "  ")
			fmt.Println(string(out))
			return
		}
		if len(stats) == 0 {
			fmt.Println("No Discord sessions found.")
			return
		}
		fmt.Println("Discord sessions:")
		for _, stat := range stats {
			over := "no"
			if stat.OverLimit {
				over = "yes"
			}
			fmt.Printf("  - %s | messages=%d tokens~%d over_limit=%s\n", stat.SessionKey, stat.Messages, stat.Tokens, over)
		}
	case "run":
		opts, err := parseArchiveRunOptions(args[1:])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			archiveDiscordHelp()
			return
		}
		if opts.SessionKey == "" && !opts.All && !opts.OverLimit {
			opts.OverLimit = true
		}
		if opts.All {
			opts.OverLimit = false
		}

		if opts.SessionKey != "" {
			result, err := manager.ArchiveSession(opts.SessionKey, opts.DryRun)
			if err != nil {
				fmt.Printf("Archive failed: %v\n", err)
				return
			}
			if result == nil {
				fmt.Println("No archive action taken.")
				return
			}
			printArchiveResult(*result)
			return
		}

		results, err := manager.ArchiveAll(opts.OverLimit, opts.DryRun)
		if err != nil {
			fmt.Printf("Archive failed: %v\n", err)
			return
		}
		if len(results) == 0 {
			fmt.Println("No sessions archived.")
			return
		}
		for _, result := range results {
			printArchiveResult(result)
		}
	case "recall":
		opts, err := parseArchiveRecallOptions(args[1:], cfg.Channels.Discord.Archive.RecallTopK, cfg.Channels.Discord.Archive.RecallMaxChars)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			archiveDiscordHelp()
			return
		}
		hits := manager.Recall(opts.Query, opts.SessionKey, opts.TopK, opts.MaxChars)
		if opts.JSON {
			out, _ := json.MarshalIndent(hits, "", "  ")
			fmt.Println(string(out))
			return
		}
		if len(hits) == 0 {
			fmt.Println("No recall hits.")
			return
		}
		for i, hit := range hits {
			fmt.Printf("%d) score=%d  session=%s  file=%s\n", i+1, hit.Score, hit.SessionKey, hit.SourcePath)
			fmt.Printf("   %s\n\n", hit.Text)
		}
	case "index":
		// Phase 1 uses lexical recall directly over archive markdown.
		fmt.Println("Index step is not required for phase-1 lexical recall (on-demand scan).")
	case "help", "--help", "-h":
		archiveDiscordHelp()
	default:
		fmt.Printf("Unknown archive discord command: %s\n", sub)
		archiveDiscordHelp()
	}
}

func parseArchiveRunOptions(args []string) (archiveRunOptions, error) {
	opts := archiveRunOptions{}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--session-key":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--session-key requires a value")
			}
			opts.SessionKey = strings.TrimSpace(args[i+1])
			i++
		case "--all":
			opts.All = true
		case "--over-limit":
			opts.OverLimit = true
		case "--dry-run":
			opts.DryRun = true
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	return opts, nil
}

func parseArchiveRecallOptions(args []string, defaultTopK, defaultMaxChars int) (archiveRecallOptions, error) {
	opts := archiveRecallOptions{
		TopK:     defaultTopK,
		MaxChars: defaultMaxChars,
	}
	queryParts := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--top-k":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--top-k requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("invalid --top-k value: %s", args[i+1])
			}
			opts.TopK = n
			i++
		case "--max-chars":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--max-chars requires a value")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return opts, fmt.Errorf("invalid --max-chars value: %s", args[i+1])
			}
			opts.MaxChars = n
			i++
		case "--session-key":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--session-key requires a value")
			}
			opts.SessionKey = strings.TrimSpace(args[i+1])
			i++
		case "--json":
			opts.JSON = true
		default:
			queryParts = append(queryParts, args[i])
		}
	}

	opts.Query = strings.TrimSpace(strings.Join(queryParts, " "))
	if opts.Query == "" {
		return opts, fmt.Errorf("query is required")
	}
	if opts.TopK <= 0 {
		opts.TopK = 6
	}
	if opts.MaxChars <= 0 {
		opts.MaxChars = 3000
	}
	return opts, nil
}

func printArchiveResult(result discordarchive.ArchiveResult) {
	mode := "archived"
	if result.DryRun {
		mode = "dry-run"
	}
	fmt.Printf(
		"%s: %s | archived=%d kept=%d tokens~%d->%d file=%s\n",
		mode,
		result.SessionKey,
		result.ArchivedMessages,
		result.KeptMessages,
		result.TokensBefore,
		result.TokensAfter,
		result.ArchivePath,
	)
}

func archiveHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nArchive commands:")
	fmt.Printf("  %s archive discord <list|run|recall|index>\n", commandName)
	fmt.Printf("  %s archive discord help\n", commandName)
}

func archiveDiscordHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nArchive Discord commands:")
	fmt.Printf("  %s archive discord list [--over-limit] [--json]\n", commandName)
	fmt.Printf("  %s archive discord run [--session-key <key> | --all | --over-limit] [--dry-run]\n", commandName)
	fmt.Printf("  %s archive discord recall <query> [--top-k <n>] [--max-chars <n>] [--session-key <key>] [--json]\n", commandName)
	fmt.Printf("  %s archive discord index\n", commandName)
}
