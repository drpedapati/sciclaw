package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/config"
)

func promptCmd() {
	if len(os.Args) < 3 {
		promptHelp()
		return
	}
	switch os.Args[2] {
	case "inspect":
		promptInspectCmd(os.Args[3:])
	case "export-envelope":
		promptExportEnvelopeCmd(os.Args[3:])
	case "optimize-preview":
		promptOptimizePreviewCmd(os.Args[3:])
	case "help", "--help", "-h":
		promptHelp()
	default:
		fmt.Printf("Unknown prompt command: %s\n", os.Args[2])
		promptHelp()
	}
}

func promptHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nPrompt commands:")
	fmt.Printf("  %s prompt inspect --session <session-key> [--workspace /abs/path] [--json]\n", commandName)
	fmt.Printf("  %s prompt export-envelope --session <session-key> [--workspace /abs/path]\n", commandName)
	fmt.Printf("  %s prompt optimize-preview --session <session-key> [--workspace /abs/path] [--ctxclaw /path/to/ctxclaw] [--json]\n", commandName)
	fmt.Println()
	fmt.Println("Read-only prompt forensics and non-invasive ctxclaw adapter commands for real saved sessions.")
}

func promptInspectCmd(args []string) {
	fs := flag.NewFlagSet("prompt inspect", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	sessionKey := fs.String("session", "", "session key to inspect")
	workspace := fs.String("workspace", "", "absolute workspace path to inspect")
	jsonOut := fs.Bool("json", false, "emit JSON")
	if err := fs.Parse(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Printf("Usage: %s prompt inspect --session <session-key> [--workspace /abs/path] [--json]\n", invokedCLIName())
		return
	}
	if strings.TrimSpace(*sessionKey) == "" {
		fmt.Printf("Usage: %s prompt inspect --session <session-key> [--workspace /abs/path] [--json]\n", invokedCLIName())
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	resolvedWorkspace, err := resolvePromptInspectWorkspace(cfg, strings.TrimSpace(*sessionKey), strings.TrimSpace(*workspace))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	report, err := agent.InspectPrompt(cfg, agent.PromptInspectOptions{SessionKey: strings.TrimSpace(*sessionKey), Workspace: resolvedWorkspace})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	if *jsonOut {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
		return
	}
	printPromptInspectReport(report)
}

func promptExportEnvelopeCmd(args []string) {
	fs := flag.NewFlagSet("prompt export-envelope", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	sessionKey := fs.String("session", "", "session key to export")
	workspace := fs.String("workspace", "", "absolute workspace path to inspect")
	if err := fs.Parse(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Printf("Usage: %s prompt export-envelope --session <session-key> [--workspace /abs/path]\n", invokedCLIName())
		return
	}
	if strings.TrimSpace(*sessionKey) == "" {
		fmt.Printf("Usage: %s prompt export-envelope --session <session-key> [--workspace /abs/path]\n", invokedCLIName())
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	resolvedWorkspace, err := resolvePromptInspectWorkspace(cfg, strings.TrimSpace(*sessionKey), strings.TrimSpace(*workspace))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	env, err := agent.BuildPromptEnvelope(cfg, agent.PromptInspectOptions{SessionKey: strings.TrimSpace(*sessionKey), Workspace: resolvedWorkspace})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	data, _ := json.MarshalIndent(env, "", "  ")
	fmt.Println(string(data))
}

func promptOptimizePreviewCmd(args []string) {
	fs := flag.NewFlagSet("prompt optimize-preview", flag.ContinueOnError)
	fs.SetOutput(ioDiscard{})
	sessionKey := fs.String("session", "", "session key to inspect")
	workspace := fs.String("workspace", "", "absolute workspace path to inspect")
	ctxclawPath := fs.String("ctxclaw", "ctxclaw", "path to ctxclaw binary")
	jsonOut := fs.Bool("json", false, "emit raw ctxclaw JSON response")
	if err := fs.Parse(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Printf("Usage: %s prompt optimize-preview --session <session-key> [--workspace /abs/path] [--ctxclaw /path/to/ctxclaw] [--json]\n", invokedCLIName())
		return
	}
	if strings.TrimSpace(*sessionKey) == "" {
		fmt.Printf("Usage: %s prompt optimize-preview --session <session-key> [--workspace /abs/path] [--ctxclaw /path/to/ctxclaw] [--json]\n", invokedCLIName())
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	resolvedWorkspace, err := resolvePromptInspectWorkspace(cfg, strings.TrimSpace(*sessionKey), strings.TrimSpace(*workspace))
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	env, err := agent.BuildPromptEnvelope(cfg, agent.PromptInspectOptions{SessionKey: strings.TrimSpace(*sessionKey), Workspace: resolvedWorkspace})
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	input, _ := json.Marshal(env)
	cmd := exec.Command(strings.TrimSpace(*ctxclawPath), "optimize")
	cmd.Stdin = bytes.NewReader(input)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if strings.TrimSpace(stderr.String()) != "" {
			fmt.Printf("Error: %v: %s\n", err, strings.TrimSpace(stderr.String()))
		} else {
			fmt.Printf("Error: %v\n", err)
		}
		return
	}
	if *jsonOut {
		fmt.Println(strings.TrimSpace(stdout.String()))
		return
	}
	printCtxclawPreview(env, stdout.Bytes())
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func resolvePromptInspectWorkspace(cfg interface {
	WorkspacePath() string
	SharedWorkspacePath() string
}, sessionKey, explicit string) (string, error) {
	if explicit != "" {
		home := ""
		if strings.HasPrefix(explicit, "~") {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("could not resolve %q because the home directory is unavailable: %w", explicit, err)
			}
		}
		path := filepath.Clean(expandHomePath(explicit, home))
		if _, err := os.Stat(filepath.Join(path, "sessions", sessionKey+".json")); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("session %q not found under %s/sessions", sessionKey, path)
	}
	cfgObj, ok := cfg.(*config.Config)
	if !ok {
		return "", fmt.Errorf("internal error: unexpected config type")
	}
	candidates := make([]string, 0, len(cfgObj.Routing.Mappings)+2)
	addCandidate := func(path string) {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "" || path == "." {
			return
		}
		for _, existing := range candidates {
			if existing == path {
				return
			}
		}
		candidates = append(candidates, path)
	}
	addCandidate(cfgObj.WorkspacePath())
	addCandidate(cfgObj.SharedWorkspacePath())
	for _, m := range cfgObj.Routing.Mappings {
		addCandidate(m.Workspace)
	}
	matches := make([]string, 0, 2)
	for _, candidate := range candidates {
		if _, err := os.Stat(filepath.Join(candidate, "sessions", sessionKey+".json")); err == nil {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("session %q not found in configured workspaces; pass --workspace explicitly", sessionKey)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("session %q found in multiple workspaces (%s); pass --workspace explicitly", sessionKey, strings.Join(matches, ", "))
	}
}

func printPromptInspectReport(r *agent.PromptInspectReport) {
	fmt.Printf("Session: %s\n", r.SessionKey)
	fmt.Printf("Workspace: %s\n", r.Workspace)
	if strings.TrimSpace(r.SharedWorkspace) != "" {
		fmt.Printf("Shared workspace: %s\n", r.SharedWorkspace)
	}
	fmt.Printf("Session file: %s\n", r.SessionPath)
	if r.Channel != "" || r.ChatID != "" {
		fmt.Printf("Route: %s:%s\n", r.Channel, r.ChatID)
	}
	fmt.Println()
	fmt.Println("Latest user turn")
	fmt.Printf("  chars: %d\n", r.CurrentUserChars)
	fmt.Printf("  preview: %q\n", truncateMiddlePrompt(r.CurrentUser, 120))
	fmt.Println()
	fmt.Println("System prompt")
	fmt.Printf("  total: %d chars (~%d tokens)\n", r.SystemPrompt.TotalChars, r.SystemPrompt.EstimatedTokens)
	fmt.Printf("  identity: %d chars\n", r.SystemPrompt.IdentityChars)
	fmt.Printf("  bootstrap total: %d chars\n", r.SystemPrompt.BootstrapTotalChars)
	for _, item := range r.SystemPrompt.Bootstrap {
		fmt.Printf("    - %s: %d chars (~%d tokens) [%s]\n", item.Name, item.Chars, item.EstimatedTokens, item.SourceWorkspace)
	}
	fmt.Printf("  skills summary: %d chars\n", r.SystemPrompt.SkillsChars)
	fmt.Printf("  memory: %d chars (~%d tokens) [%s]\n", r.SystemPrompt.MemoryChars, r.SystemPrompt.Memory.EstimatedTokens, r.SystemPrompt.Memory.SourceWorkspace)
	fmt.Printf("  current-session block: %d chars\n", r.SystemPrompt.SessionBlockChars)
	fmt.Printf("  summary block: %d chars\n", r.SystemPrompt.SummaryChars)
	fmt.Printf("  section join separators: %d chars\n", r.SystemPrompt.JoinSeparatorChars)
	fmt.Println()
	fmt.Println("History before latest user turn")
	fmt.Printf("  messages: %d\n", r.History.MessageCount)
	fmt.Printf("  total: %d chars (~%d tokens)\n", r.History.TotalChars, r.History.EstimatedTokens)
	for _, item := range r.History.ByRole {
		fmt.Printf("    - %s: %d msgs, %d chars (~%d tokens)\n", item.Role, item.MessageCount, item.Chars, item.EstimatedTokens)
	}
	if len(r.History.ToolMessages) > 0 {
		fmt.Println("  tool-heavy history")
		for _, item := range r.History.ToolMessages {
			flags := make([]string, 0, 2)
			if item.WouldCompactRawNow {
				flags = append(flags, "would compact raw now")
			}
			if item.AlreadyCompacted {
				flags = append(flags, "already compacted")
			}
			suffix := ""
			if len(flags) > 0 {
				suffix = " [" + strings.Join(flags, "; ") + "]"
			}
			fmt.Printf("    - %s: %d msgs, %d chars (~%d tokens), largest=%d%s\n", item.ToolName, item.MessageCount, item.Chars, item.EstimatedTokens, item.LargestMessageChars, suffix)
		}
	}
	if r.History.LargestMessage.Chars > 0 {
		fmt.Printf("  largest message: #%d %s %d chars preview=%q\n", r.History.LargestMessage.MessageIndex, r.History.LargestMessage.Role, r.History.LargestMessage.Chars, r.History.LargestMessage.Preview)
	}
	fmt.Println()
	fmt.Println("Tool schemas")
	fmt.Printf("  count: %d\n", r.ToolSchemas.Count)
	fmt.Printf("  total: %d chars (~%d tokens)\n", r.ToolSchemas.TotalChars, r.ToolSchemas.EstimatedTokens)
	for _, item := range r.ToolSchemas.Largest {
		fmt.Printf("    - %s: %d chars (~%d tokens)\n", item.Name, item.Chars, item.EstimatedTokens)
	}
	fmt.Println()
	fmt.Println("Payload estimate")
	fmt.Printf("  messages JSON: %d chars\n", r.Payload.MessagesJSONChars)
	fmt.Printf("  tool schemas JSON: %d chars\n", r.Payload.ToolSchemasJSONChars)
	fmt.Printf("  wrapper overhead: %d chars\n", r.Payload.WrapperChars)
	fmt.Printf("  content-only estimate: ~%d tokens\n", r.Payload.EstimatedContentTokens)
	fmt.Printf("  payload estimate: ~%d tokens\n", r.Payload.EstimatedPayloadTokens)
	fmt.Println()
	fmt.Println("Top contributors")
	type row struct {
		name  string
		chars int
		toks  int
	}
	rows := []row{
		{name: "tool schemas", chars: r.ToolSchemas.TotalChars, toks: r.ToolSchemas.EstimatedTokens},
		{name: "bootstrap files", chars: r.SystemPrompt.BootstrapTotalChars, toks: estimateTokensForCmd(r.SystemPrompt.BootstrapTotalChars)},
		{name: "history", chars: r.History.TotalChars, toks: r.History.EstimatedTokens},
		{name: "memory", chars: r.SystemPrompt.MemoryChars, toks: r.SystemPrompt.Memory.EstimatedTokens},
		{name: "identity", chars: r.SystemPrompt.IdentityChars, toks: estimateTokensForCmd(r.SystemPrompt.IdentityChars)},
		{name: "skills summary", chars: r.SystemPrompt.SkillsChars, toks: estimateTokensForCmd(r.SystemPrompt.SkillsChars)},
		{name: "summary block", chars: r.SystemPrompt.SummaryChars, toks: estimateTokensForCmd(r.SystemPrompt.SummaryChars)},
		{name: "wrapper overhead", chars: r.Payload.WrapperChars, toks: estimateTokensForCmd(r.Payload.WrapperChars)},
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].chars > rows[j].chars })
	for _, item := range rows {
		if item.chars == 0 {
			continue
		}
		fmt.Printf("  - %s: %d chars (~%d tokens)\n", item.name, item.chars, item.toks)
	}
}

type ctxclawPreviewResponse struct {
	Version           string `json:"version"`
	CheckpointSummary string `json:"checkpoint_summary"`
	Archives          []struct {
		Path         string `json:"path"`
		Kind         string `json:"kind"`
		ToolName     string `json:"tool_name"`
		ToolCallID   string `json:"tool_call_id"`
		OriginalSize int    `json:"original_size"`
	} `json:"archives"`
	Stats struct {
		MessagesBefore           int      `json:"messages_before"`
		MessagesAfter            int      `json:"messages_after"`
		EstimatedTokensBefore    int      `json:"estimated_tokens_before"`
		EstimatedTokensAfter     int      `json:"estimated_tokens_after"`
		EstimatedSavings         int      `json:"estimated_savings"`
		CheckpointChars          int      `json:"checkpoint_chars"`
		CompactedMessageCount    int      `json:"compacted_message_count"`
		ArchivedArtifactCount    int      `json:"archived_artifact_count"`
		KeptRecentMessageCount   int      `json:"kept_recent_message_count"`
		OffloadedToolResultCount int      `json:"offloaded_tool_result_count"`
		Notes                    []string `json:"notes"`
	} `json:"stats"`
}

func printCtxclawPreview(env *agent.PromptEnvelope, raw []byte) {
	var resp ctxclawPreviewResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		fmt.Printf("ctxclaw returned invalid JSON: %v\n", err)
		fmt.Println(string(raw))
		return
	}
	fmt.Printf("Session: %s\n", env.SessionKey)
	fmt.Printf("Workspace: %s\n", env.Workspace)
	fmt.Println()
	fmt.Println("ctxclaw optimize preview")
	fmt.Printf("  messages: %d -> %d\n", resp.Stats.MessagesBefore, resp.Stats.MessagesAfter)
	fmt.Printf("  estimated tokens: %d -> %d (delta=%d)\n", resp.Stats.EstimatedTokensBefore, resp.Stats.EstimatedTokensAfter, resp.Stats.EstimatedSavings)
	fmt.Printf("  offloaded tool results: %d\n", resp.Stats.OffloadedToolResultCount)
	fmt.Printf("  compacted messages: %d\n", resp.Stats.CompactedMessageCount)
	fmt.Printf("  kept recent messages: %d\n", resp.Stats.KeptRecentMessageCount)
	if resp.Stats.CheckpointChars > 0 {
		fmt.Printf("  checkpoint summary chars: %d\n", resp.Stats.CheckpointChars)
	}
	if len(resp.Archives) > 0 {
		fmt.Println("  archives:")
		for _, a := range resp.Archives {
			label := a.ToolName
			if strings.TrimSpace(label) == "" {
				label = "tool"
			}
			fmt.Printf("    - %s %d chars -> %s\n", label, a.OriginalSize, a.Path)
		}
	}
	if len(resp.Stats.Notes) > 0 {
		fmt.Println("  notes:")
		for _, note := range resp.Stats.Notes {
			fmt.Printf("    - %s\n", note)
		}
	}
	if strings.TrimSpace(resp.CheckpointSummary) != "" {
		fmt.Println()
		fmt.Println("Checkpoint summary preview")
		fmt.Printf("  %q\n", truncateMiddlePrompt(strings.Join(strings.Fields(resp.CheckpointSummary), " "), 180))
	}
}

func estimateTokensForCmd(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 2) / 4
}

func truncateMiddlePrompt(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	left := (max - 3) / 2
	right := max - 3 - left
	return s[:left] + "..." + s[len(s)-right:]
}
