package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	iofs "io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/cmd/picoclaw/tui"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/models"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/workspacetpl"
	webui "github.com/sipeed/picoclaw/web"
)

// webCmd starts the web UI server.
func webCmd() {
	listen := "127.0.0.1:4142"
	distDir := ""

	// Parse flags
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--listen", "-l":
			if i+1 < len(args) {
				i++
				listen = args[i]
			}
		case "--dist", "-d":
			if i+1 < len(args) {
				i++
				distDir = args[i]
			}
		case "--help", "-h":
			fmt.Println("Usage: sciclaw web [options]")
			fmt.Println("")
			fmt.Println("Options:")
			fmt.Println("  --listen, -l  Address to listen on (default: 127.0.0.1:4142)")
			fmt.Println("  --dist, -d    Path to built web UI dist folder")
			return
		}
	}

	exec := tui.NewLocalExecutor()
	srv := newWebServer(exec, distDir)

	fmt.Printf("sciClaw web UI starting on http://%s\n", listen)
	if err := http.ListenAndServe(listen, srv); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type webServer struct {
	exec           tui.Executor
	mux            *http.ServeMux
	distDir        string
	staticFS       iofs.FS
	static         http.Handler
	liteChatRunner func(context.Context, string) (*liteChatResult, error)

	// Cached snapshot
	snapMu   sync.RWMutex
	snapshot *tui.VMSnapshot
	snapTime time.Time
}

type liteChatResult struct {
	Response string
	Model    string
	Usage    *providers.UsageInfo
}

func newWebServer(exec tui.Executor, distDir string) *webServer {
	staticFS, err := resolveStaticFS(distDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: web assets unavailable: %v\n", err)
	}
	s := &webServer{
		exec:     exec,
		mux:      http.NewServeMux(),
		distDir:  distDir,
		staticFS: staticFS,
	}
	s.liteChatRunner = s.runLiteChat
	if staticFS != nil {
		s.static = http.FileServer(http.FS(staticFS))
	}
	s.registerRoutes()
	return s
}

func (s *webServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *webServer) registerRoutes() {
	// API routes
	s.mux.HandleFunc("/api/snapshot", s.handleSnapshot)
	s.mux.HandleFunc("/api/home/checklist", s.handleChecklist)
	s.mux.HandleFunc("/api/home/smoke-test", s.handleSmokeTest)
	s.mux.HandleFunc("/api/chat", s.handleChat)
	s.mux.HandleFunc("/api/channels/", s.handleChannels)
	s.mux.HandleFunc("/api/email", s.handleEmail)
	s.mux.HandleFunc("/api/email/test", s.handleEmailTest)
	s.mux.HandleFunc("/api/auth", s.handleAuth)
	s.mux.HandleFunc("/api/doctor", s.handleDoctor)
	s.mux.HandleFunc("/api/service/", s.handleService)
	s.mux.HandleFunc("/api/jobs", s.handleJobs)
	s.mux.HandleFunc("/api/jobs/", s.handleJobs)
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("/api/models/", s.handleModelsAction)
	s.mux.HandleFunc("/api/phi", s.handlePhi)
	s.mux.HandleFunc("/api/phi/", s.handlePhiAction)
	s.mux.HandleFunc("/api/skills", s.handleSkills)
	s.mux.HandleFunc("/api/skills/", s.handleSkillsAction)
	s.mux.HandleFunc("/api/cron", s.handleCron)
	s.mux.HandleFunc("/api/cron/", s.handleCronAction)
	s.mux.HandleFunc("/api/routing/", s.handleRouting)
	s.mux.HandleFunc("/api/settings", s.handleSettings)
	s.mux.HandleFunc("/api/home/onboard", s.handleOnboard)
	s.mux.HandleFunc("/api/system", s.handleSystem)
	s.mux.HandleFunc("/api/system/", s.handleSystemAction)

	// Static files
	if s.static != nil {
		s.mux.HandleFunc("/", s.handleStatic)
	}
}

func resolveStaticFS(distDir string) (iofs.FS, error) {
	if distDir != "" {
		return os.DirFS(distDir), nil
	}
	return webui.DistFS()
}

// ── Helpers ──

func jsonResp(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func readBody(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func toString(v interface{}) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		return fmt.Sprintf("%v", value)
	}
}

func getIntValue(m map[string]interface{}, key string) int {
	switch value := m[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func coerceStringSlice(v interface{}) []string {
	switch value := v.(type) {
	case nil:
		return []string{}
	case []string:
		out := make([]string, 0, len(value))
		for _, item := range value {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(value))
		for _, item := range value {
			trimmed := strings.TrimSpace(toString(item))
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	case string:
		raw := strings.NewReplacer("\r\n", "\n", ",", "\n").Replace(value)
		lines := strings.Split(raw, "\n")
		out := make([]string, 0, len(lines))
		for _, item := range lines {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	default:
		return []string{}
	}
}

func (s *webServer) runCLI(timeout time.Duration, args ...string) (string, error) {
	cmd := "HOME=" + shellQuote(s.exec.HomePath()) + " " + shellQuote(s.exec.BinaryPath()) + " " + strings.Join(args, " ") + " 2>&1"
	return s.exec.ExecShell(timeout, cmd)
}

func (s *webServer) runCLIQuiet(timeout time.Duration, args ...string) (string, error) {
	cmd := "HOME=" + shellQuote(s.exec.HomePath()) + " " + shellQuote(s.exec.BinaryPath()) + " " + strings.Join(args, " ") + " 2>/dev/null"
	return s.exec.ExecShell(timeout, cmd)
}

func (s *webServer) getSnapshot() *tui.VMSnapshot {
	s.snapMu.RLock()
	if s.snapshot != nil && time.Since(s.snapTime) < 5*time.Second {
		snap := s.snapshot
		s.snapMu.RUnlock()
		return snap
	}
	s.snapMu.RUnlock()

	s.snapMu.Lock()
	defer s.snapMu.Unlock()
	// Re-check: another goroutine may have refreshed while we waited.
	if s.snapshot != nil && time.Since(s.snapTime) < 5*time.Second {
		return s.snapshot
	}
	snap := tui.CollectSnapshot(s.exec)
	s.snapshot = &snap
	s.snapTime = time.Now()
	return &snap
}

func jobsStorePath() string {
	return filepath.Join(filepath.Dir(getConfigPath()), "jobs.json")
}

func loadJobRecords() ([]routing.JobRecord, error) {
	path := jobsStorePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []routing.JobRecord{}, nil
		}
		return nil, err
	}
	var payload struct {
		Jobs []routing.JobRecord `json:"jobs"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Jobs, nil
}

func jobLane(class routing.JobClass) string {
	switch strings.TrimSpace(string(class)) {
	case string(routing.JobClassBTW), "external_readonly":
		return "btw"
	default:
		return "main"
	}
}

func jobStateRank(state routing.JobState) int {
	switch state {
	case routing.JobStateRunning:
		return 0
	case routing.JobStateQueued:
		return 1
	case routing.JobStateFailed:
		return 2
	case routing.JobStateInterrupted:
		return 3
	case routing.JobStateCancelled:
		return 4
	case routing.JobStateDone:
		return 5
	default:
		return 6
	}
}

func jobSenderInfo(record routing.JobRecord) (string, string) {
	userID := strings.TrimSpace(record.Message.SenderID)
	if md := record.Message.Metadata; md != nil {
		if trimmed := strings.TrimSpace(md["user_id"]); trimmed != "" {
			userID = trimmed
		}
	}

	nameCandidates := []string{}
	if md := record.Message.Metadata; md != nil {
		nameCandidates = append(nameCandidates,
			md["display_name"],
			md["username"],
		)
	}
	nameCandidates = append(nameCandidates, record.Message.Metadata["sender_name"])
	for _, candidate := range nameCandidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return userID, trimmed
		}
	}
	return userID, ""
}

// ── Snapshot ──

func (s *webServer) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	snap := s.getSnapshot()
	jsonResp(w, snap)
}

// ── Home ──

func (s *webServer) handleChecklist(w http.ResponseWriter, r *http.Request) {
	snap := s.getSnapshot()
	checklist := map[string]bool{
		"config":  snap.ConfigExists,
		"auth":    snap.OpenAI == "ready" || snap.Anthropic == "ready",
		"channel": snap.Discord.Status == "ready" || snap.Telegram.Status == "ready",
		"service": snap.ServiceInstalled,
	}
	jsonResp(w, checklist)
}

func (s *webServer) handleOnboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	out, err := s.runCLI(30*time.Second, "onboard", "-y")
	if err != nil {
		jsonErr(w, out+": "+err.Error(), 500)
		return
	}
	jsonResp(w, map[string]interface{}{"ok": true, "output": out})
}

func (s *webServer) handleSmokeTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	readBody(r, &body)

	args := []string{"agent", "-m", "'Hello, are you there?'", "-s", "web:smoke"}
	if body.Model != "" {
		args = append(args, "--model", body.Model)
	}
	out, err := s.runCLI(60*time.Second, args...)
	ok := err == nil
	jsonResp(w, map[string]interface{}{"ok": ok, "output": out})
}

// ── Chat ──

func (s *webServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	var body struct {
		Message string `json:"message"`
	}
	if err := readBody(r, &body); err != nil || body.Message == "" {
		jsonErr(w, "message required", 400)
		return
	}

	if shouldUseLightweightWebChat(body.Message) {
		result, err := s.liteChatRunner(r.Context(), body.Message)
		if err == nil && result != nil && strings.TrimSpace(result.Response) != "" {
			jsonResp(w, map[string]interface{}{"response": result.Response, "mode": "lite"})
			return
		}
		logger.WarnCF("web", "Lightweight web chat failed; falling back to full agent", map[string]interface{}{
			"error":   fmt.Sprint(err),
			"message": strings.TrimSpace(body.Message),
		})
	}

	out, err := s.runCLIQuiet(120*time.Second, "agent", "-m", shellQuote(body.Message), "-s", "web:chat")
	if err != nil {
		jsonResp(w, map[string]interface{}{"response": "Error: " + err.Error() + "\n" + out})
		return
	}
	jsonResp(w, map[string]interface{}{"response": out, "mode": "full"})
}

var lightweightGreetingPattern = regexp.MustCompile(`(?i)^\s*(hi|hello|hey|yo|sup|ping|test|thanks|thank you|how are you|are you there|who are you|what can you do|help|good (morning|afternoon|evening))([!.?\s]*)$`)

func shouldUseLightweightWebChat(message string) bool {
	normalized := strings.TrimSpace(message)
	if normalized == "" || len(normalized) > 120 {
		return false
	}
	if strings.Contains(normalized, "\n") || strings.Contains(normalized, "http://") || strings.Contains(normalized, "https://") {
		return false
	}
	if lightweightGreetingPattern.MatchString(normalized) {
		return true
	}
	lower := strings.ToLower(normalized)
	blocked := []string{
		"search", "find", "read", "write", "save", "create", "draft", "edit", "revise", "summarize", "analyze",
		"compare", "export", "download", "upload", "attach", "pubmed", "pmid", "doi", "ris", "pdf", "docx",
		"xlsx", "pptx", "folder", "dropbox", "file", "abstract", "manuscript", "protocol", "skill", "poster",
		"conference", "review", "citation", "references", "web search", "weather",
	}
	for _, token := range blocked {
		if strings.Contains(lower, token) {
			return false
		}
	}
	return lower == "hello" || lower == "hi" || lower == "hey" || lower == "help"
}

func (s *webServer) runLiteChat(ctx context.Context, message string) (*liteChatResult, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(cfg.Agents.Defaults.Model)
	if model == "" {
		model = provider.GetDefaultModel()
	}
	messages := []providers.Message{
		{
			Role:    "system",
			Content: "You are sciClaw, a paired-scientist assistant in a web chat. This is a lightweight conversational turn. Reply naturally and briefly. Do not claim to have run tools, changed files, searched the web, or completed background work unless the user explicitly asked a simple conversational question about capabilities. Keep it to 1-3 short paragraphs.",
		},
		{Role: "user", Content: strings.TrimSpace(message)},
	}
	resp, err := provider.Chat(ctx, messages, nil, model, map[string]interface{}{
		"max_tokens":  384,
		"temperature": 0.7,
	})
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return nil, fmt.Errorf("empty lightweight response")
	}
	logFields := map[string]interface{}{
		"model":   model,
		"message": strings.TrimSpace(message),
	}
	if resp.Usage != nil {
		logFields["input_tokens"] = resp.Usage.PromptTokens
		logFields["output_tokens"] = resp.Usage.CompletionTokens
		logFields["total_tokens"] = resp.Usage.TotalTokens
	}
	logger.InfoCF("web", "Lightweight web chat completed", logFields)
	return &liteChatResult{Response: content, Model: model, Usage: resp.Usage}, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// ── Channels ──

func (s *webServer) handleChannels(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		jsonErr(w, "channel required", 400)
		return
	}
	channel := parts[0]

	if len(parts) >= 2 && parts[1] == "setup" && r.Method == http.MethodPost {
		var body struct {
			Token  string `json:"token"`
			UserId string `json:"userId"`
			Name   string `json:"name"`
		}
		readBody(r, &body)
		entry := tui.FormatEntry(body.UserId, body.Name)
		if err := tui.SaveChannelSetupConfig(s.exec, channel, body.Token, entry); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		s.invalidateSnapshot()
		jsonResp(w, map[string]bool{"ok": true})
		return
	}

	if len(parts) >= 2 && parts[1] == "users" {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				UserId string `json:"userId"`
				Name   string `json:"name"`
			}
			readBody(r, &body)
			entry := tui.FormatEntry(body.UserId, body.Name)
			if err := tui.AppendAllowFrom(s.exec, channel, entry); err != nil {
				jsonErr(w, err.Error(), 500)
				return
			}
			s.invalidateSnapshot()
			jsonResp(w, map[string]bool{"ok": true})

		case http.MethodDelete:
			if len(parts) < 3 {
				jsonErr(w, "user id required", 400)
				return
			}
			userId := parts[2]
			// Find index by userId
			snap := s.getSnapshot()
			var users []tui.ApprovedUser
			if channel == "discord" {
				users = snap.Discord.ApprovedUsers
			} else {
				users = snap.Telegram.ApprovedUsers
			}
			for i, u := range users {
				if u.UserID == userId {
					if err := tui.RemoveAllowFrom(s.exec, channel, i); err != nil {
						jsonErr(w, err.Error(), 500)
						return
					}
					s.invalidateSnapshot()
					jsonResp(w, map[string]bool{"ok": true})
					return
				}
			}
			jsonErr(w, "user not found", 404)

		default:
			jsonErr(w, "method not allowed", 405)
		}
		return
	}
	jsonErr(w, "unknown channel endpoint", 400)
}

// ── Email ──

func (s *webServer) handleEmail(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := tui.ReadConfigMap(s.exec)
		if err != nil {
			cfg = map[string]interface{}{}
		}
		email := tui.GetMapNested(cfg, "channels", "email")
		resp := map[string]interface{}{
			"enabled":             tui.GetBool(email, "enabled"),
			"provider":            tui.GetString(email, "provider"),
			"address":             tui.GetString(email, "address"),
			"displayName":         tui.GetString(email, "display_name"),
			"hasApiKey":           tui.GetString(email, "api_key") != "",
			"baseUrl":             tui.GetString(email, "base_url"),
			"allowFrom":           tui.GetStringSliceValue(email, "allow_from"),
			"receiveEnabled":      tui.GetBool(email, "receive_enabled"),
			"receiveMode":         tui.GetString(email, "receive_mode"),
			"pollIntervalSeconds": getIntValue(email, "poll_interval_seconds"),
		}
		jsonResp(w, resp)

	case http.MethodPut:
		var body map[string]interface{}
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			email := tui.EnsureMapNested(cfg, "channels", "email")
			for k, raw := range body {
				switch k {
				case "enabled":
					if enabled, ok := raw.(bool); ok {
						email["enabled"] = enabled
					}
				case "address":
					email["address"] = strings.TrimSpace(toString(raw))
				case "displayName":
					email["display_name"] = strings.TrimSpace(toString(raw))
				case "apiKey":
					if key := strings.TrimSpace(toString(raw)); key != "" {
						email["api_key"] = key
					}
				case "baseUrl":
					email["base_url"] = strings.TrimSpace(toString(raw))
				case "allowFrom":
					email["allow_from"] = coerceStringSlice(raw)
				}
			}
			email["provider"] = "resend"
			if strings.TrimSpace(tui.GetString(email, "display_name")) == "" {
				email["display_name"] = "sciClaw"
			}
			if strings.TrimSpace(tui.GetString(email, "base_url")) == "" {
				email["base_url"] = "https://api.resend.com"
			}
			email["receive_enabled"] = false
			email["receive_mode"] = "poll"
			if getIntValue(email, "poll_interval_seconds") <= 0 {
				email["poll_interval_seconds"] = 30
			}
			return nil
		})
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})

	default:
		jsonErr(w, "method not allowed", 405)
	}
}

func (s *webServer) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	var body struct {
		To string `json:"to"`
	}
	readBody(r, &body)
	out, err := s.runCLI(30*time.Second, "email", "test", shellQuote(body.To))
	jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
}

// ── Auth ──

func (s *webServer) handleAuth(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/auth")
	if path == "" || path == "/" {
		// GET - list providers
		snap := s.getSnapshot()
		providers := []map[string]string{
			{"provider": "OpenAI", "status": providerStatus(snap.OpenAI), "method": ""},
			{"provider": "Anthropic", "status": providerStatus(snap.Anthropic), "method": ""},
		}
		jsonResp(w, providers)
		return
	}

	// /api/auth/{provider}/{action}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		jsonErr(w, "action required", 400)
		return
	}
	provider := parts[0]
	action := parts[1]

	switch action {
	case "login":
		out, err := s.runCLI(60*time.Second, "auth", "login", "--provider", strings.ToLower(provider))
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "logout":
		out, err := s.runCLI(10*time.Second, "auth", "logout", "--provider", strings.ToLower(provider))
		s.invalidateSnapshot()
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "key":
		var body struct {
			Key string `json:"key"`
		}
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			p := tui.EnsureMapNested(cfg, "providers", strings.ToLower(provider))
			p["api_key"] = body.Key
			p["auth_method"] = "manual"
			return nil
		})
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		s.invalidateSnapshot()
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonErr(w, "unknown action", 400)
	}
}

func providerStatus(s string) string {
	if s == "ready" {
		return "active"
	}
	return "not_set"
}

// ── Doctor ──

func (s *webServer) handleDoctor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	out, err := s.runCLI(60*time.Second, "doctor", "--json")
	if err != nil {
		// Doctor may exit non-zero but still produce output
		if out == "" {
			out = err.Error()
		}
	}
	// Try to parse as JSON
	var report interface{}
	if json.Unmarshal([]byte(out), &report) == nil {
		jsonResp(w, report)
		return
	}
	// Fallback: return raw text
	jsonResp(w, map[string]interface{}{
		"version":  s.exec.AgentVersion(),
		"os":       "",
		"arch":     "",
		"checks":   []interface{}{},
		"raw":      out,
		"passed":   0,
		"warnings": 0,
		"errors":   0,
		"skipped":  0,
	})
}

// ── Service ──

func (s *webServer) handleService(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/service/")
	switch action {
	case "logs":
		out, _ := s.runCLI(10*time.Second, "service", "logs", "--lines", "50")
		jsonResp(w, map[string]string{"logs": out})
	case "start", "stop", "restart", "install", "uninstall", "refresh":
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", 405)
			return
		}
		out, err := s.runCLI(20*time.Second, "service", action)
		s.invalidateSnapshot()
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	default:
		jsonErr(w, "unknown service action", 400)
	}
}

// ── Jobs ──

func (s *webServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	type jobsSummary struct {
		Total              int `json:"total"`
		Active             int `json:"active"`
		Running            int `json:"running"`
		Queued             int `json:"queued"`
		Done               int `json:"done"`
		Failed             int `json:"failed"`
		Interrupted        int `json:"interrupted"`
		Cancelled          int `json:"cancelled"`
		DistinctChannels   int `json:"distinctChannels"`
		DistinctChats      int `json:"distinctChats"`
		DistinctUsers      int `json:"distinctUsers"`
		DistinctWorkspaces int `json:"distinctWorkspaces"`
	}
	type jobView struct {
		ID              string `json:"id"`
		ShortID         string `json:"shortId"`
		Channel         string `json:"channel"`
		ChatID          string `json:"chatId"`
		Workspace       string `json:"workspace"`
		RouteLabel      string `json:"routeLabel"`
		RuntimeKey      string `json:"runtimeKey"`
		TargetKey       string `json:"targetKey"`
		Class           string `json:"class"`
		Lane            string `json:"lane"`
		State           string `json:"state"`
		Phase           string `json:"phase"`
		Detail          string `json:"detail"`
		Summary         string `json:"summary"`
		AskSummary      string `json:"askSummary"`
		LastError       string `json:"lastError"`
		StatusMessageID string `json:"statusMessageId"`
		UserID          string `json:"userId"`
		UserName        string `json:"userName"`
		MessageID       string `json:"messageId"`
		SessionKey      string `json:"sessionKey"`
		StartedAt       int64  `json:"startedAt"`
		UpdatedAt       int64  `json:"updatedAt"`
		DurationSec     int64  `json:"durationSec"`
		Stale           bool   `json:"stale"`
	}
	type jobsResponse struct {
		GeneratedAt int64       `json:"generatedAt"`
		Summary     jobsSummary `json:"summary"`
		Jobs        []jobView   `json:"jobs"`
	}

	buildJobsResponse := func() (jobsResponse, error) {
		records, err := loadJobRecords()
		if err != nil {
			return jobsResponse{}, err
		}

		cfg, _ := config.LoadConfig(getConfigPath())
		routeLabels := map[string]string{}
		if cfg != nil {
			for _, mapping := range cfg.Routing.Mappings {
				key := strings.TrimSpace(mapping.Channel) + "\x00" + strings.TrimSpace(mapping.ChatID)
				routeLabels[key] = strings.TrimSpace(mapping.Label)
			}
		}

		sort.Slice(records, func(i, j int) bool {
			ri := jobStateRank(records[i].State)
			rj := jobStateRank(records[j].State)
			if ri != rj {
				return ri < rj
			}
			if records[i].UpdatedAt != records[j].UpdatedAt {
				return records[i].UpdatedAt > records[j].UpdatedAt
			}
			return records[i].StartedAt > records[j].StartedAt
		})

		resp := jobsResponse{GeneratedAt: time.Now().UnixMilli(), Jobs: []jobView{}}
		channelSet := map[string]struct{}{}
		chatSet := map[string]struct{}{}
		userSet := map[string]struct{}{}
		workspaceSet := map[string]struct{}{}

		for _, record := range records {
			resp.Summary.Total++
			switch record.State {
			case routing.JobStateRunning:
				resp.Summary.Running++
				resp.Summary.Active++
			case routing.JobStateQueued:
				resp.Summary.Queued++
				resp.Summary.Active++
			case routing.JobStateDone:
				resp.Summary.Done++
			case routing.JobStateFailed:
				resp.Summary.Failed++
			case routing.JobStateInterrupted:
				resp.Summary.Interrupted++
			case routing.JobStateCancelled:
				resp.Summary.Cancelled++
			}

			if channel := strings.TrimSpace(record.Channel); channel != "" {
				channelSet[channel] = struct{}{}
			}
			if chat := strings.TrimSpace(record.ChatID); chat != "" {
				chatSet[chat] = struct{}{}
			}
			if workspace := strings.TrimSpace(record.Workspace); workspace != "" {
				workspaceSet[workspace] = struct{}{}
			}

			userID, userName := jobSenderInfo(record)
			if userID != "" {
				userSet[userID] = struct{}{}
			}

			labelKey := strings.TrimSpace(record.Channel) + "\x00" + strings.TrimSpace(record.ChatID)
			durationSec := int64(0)
			if record.UpdatedAt > record.StartedAt && record.StartedAt > 0 {
				durationSec = (record.UpdatedAt - record.StartedAt) / 1000
			}

			stale := false
			if (record.State == routing.JobStateRunning || record.State == routing.JobStateQueued) && record.UpdatedAt > 0 {
				stale = time.Since(time.UnixMilli(record.UpdatedAt)) > 15*time.Minute
			}

			resp.Jobs = append(resp.Jobs, jobView{
				ID:              record.ID,
				ShortID:         strings.TrimSpace(record.ShortID),
				Channel:         record.Channel,
				ChatID:          record.ChatID,
				Workspace:       record.Workspace,
				RouteLabel:      routeLabels[labelKey],
				RuntimeKey:      record.RuntimeKey,
				TargetKey:       record.TargetKey,
				Class:           string(record.Class),
				Lane:            jobLane(record.Class),
				State:           string(record.State),
				Phase:           record.Phase,
				Detail:          record.Detail,
				Summary:         record.Summary,
				AskSummary:      record.AskSummary,
				LastError:       record.LastError,
				StatusMessageID: record.StatusMessageID,
				UserID:          userID,
				UserName:        userName,
				MessageID:       strings.TrimSpace(record.Message.Metadata["message_id"]),
				SessionKey:      record.Message.SessionKey,
				StartedAt:       record.StartedAt,
				UpdatedAt:       record.UpdatedAt,
				DurationSec:     durationSec,
				Stale:           stale,
			})
		}

		resp.Summary.DistinctChannels = len(channelSet)
		resp.Summary.DistinctChats = len(chatSet)
		resp.Summary.DistinctUsers = len(userSet)
		resp.Summary.DistinctWorkspaces = len(workspaceSet)
		return resp, nil
	}

	switch {
	case r.URL.Path == "/api/jobs" && r.Method == http.MethodGet:
		resp, err := buildJobsResponse()
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, resp)
		return
	default:
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ── Models ──

func (s *webServer) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		snap := s.getSnapshot()
		current := snap.ActiveModel
		provider := snap.ActiveProvider
		effort := ""
		authMethod := ""

		cfg, err := loadConfig()
		if err == nil && cfg != nil {
			current = cfg.Agents.Defaults.Model
			provider = models.ResolveProvider(current, cfg)
			effort = cfg.Agents.Defaults.ReasoningEffort
			if method, ok := detectProviderAuth(strings.ToLower(strings.TrimSpace(provider)), cfg); ok {
				authMethod = method
			}
		}

		jsonResp(w, map[string]string{
			"current":    current,
			"provider":   provider,
			"effort":     effort,
			"authMethod": authMethod,
		})
	case http.MethodPut:
		var body struct {
			Model string `json:"model"`
		}
		if err := readBody(r, &body); err != nil {
			jsonErr(w, "invalid request", http.StatusBadRequest)
			return
		}
		body.Model = strings.TrimSpace(body.Model)
		if body.Model == "" {
			jsonErr(w, "model is required", http.StatusBadRequest)
			return
		}

		cfg, err := loadConfig()
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		provider := models.ResolveProvider(body.Model, cfg)
		cfg.Agents.Defaults.Model = body.Model
		if provider != "" && provider != "unknown" {
			cfg.Agents.Defaults.Provider = provider
		}
		err = config.SaveConfig(getConfigPath(), cfg)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		s.invalidateSnapshot()
		_, running, _ := s.exec.ServiceInstalled(), s.exec.ServiceActive(), s.exec.ServiceActive()
		jsonResp(w, map[string]interface{}{
			"ok":              true,
			"model":           body.Model,
			"provider":        provider,
			"restartRequired": running,
		})
	default:
		jsonErr(w, "method not allowed", 405)
	}
}

func (s *webServer) handleModelsAction(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/models/")
	switch action {
	case "catalog":
		type discoverPayload struct {
			Provider string   `json:"provider"`
			Source   string   `json:"source"`
			Models   []string `json:"models"`
			Warning  string   `json:"warning,omitempty"`
		}
		type catalogEntry struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Source   string `json:"source"`
		}
		type catalogResponse struct {
			Provider string         `json:"provider"`
			Source   string         `json:"source"`
			Warning  string         `json:"warning,omitempty"`
			Models   []catalogEntry `json:"models"`
		}

		out, err := s.runCLI(20*time.Second, "models", "discover", "--json")
		payload := discoverPayload{}
		if json.Unmarshal([]byte(strings.TrimSpace(out)), &payload) != nil {
			msg := firstNonEmptyLine(strings.TrimSpace(out))
			if msg == "" && err != nil {
				msg = err.Error()
			}
			if msg == "" {
				msg = "No model catalog returned"
			}
			jsonResp(w, catalogResponse{Warning: msg, Models: []catalogEntry{}})
			return
		}

		seen := map[string]struct{}{}
		entries := make([]catalogEntry, 0, len(payload.Models))
		for _, model := range payload.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			if _, ok := seen[model]; ok {
				continue
			}
			seen[model] = struct{}{}
			entries = append(entries, catalogEntry{
				ID:       model,
				Name:     model,
				Provider: payload.Provider,
				Source:   payload.Source,
			})
		}

		jsonResp(w, catalogResponse{
			Provider: payload.Provider,
			Source:   payload.Source,
			Warning:  payload.Warning,
			Models:   entries,
		})
	case "effort":
		if r.Method != http.MethodPut {
			jsonErr(w, "method not allowed", 405)
			return
		}
		var body struct {
			Effort string `json:"effort"`
		}
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			defaults := tui.EnsureMapNested(cfg, "agents", "defaults")
			defaults["reasoning_effort"] = body.Effort
			return nil
		})
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonErr(w, "unknown models action", 400)
	}
}

// ── PHI ──

func (s *webServer) handlePhi(w http.ResponseWriter, r *http.Request) {
	cfg, err := tui.ReadConfigMap(s.exec)
	if err != nil {
		cfg = map[string]interface{}{}
	}
	phi := tui.GetMapNested(cfg, "phi")
	snap := s.getSnapshot()
	jsonResp(w, map[string]interface{}{
		"mode":             tui.GetString(phi, "mode"),
		"cloudModel":       snap.ActiveModel,
		"cloudProvider":    snap.ActiveProvider,
		"localBackend":     tui.GetString(phi, "local_backend"),
		"localModel":       tui.GetString(phi, "local_model"),
		"localPreset":      tui.GetString(phi, "local_preset"),
		"backendRunning":   false,
		"backendInstalled": false,
		"backendVersion":   "",
		"modelReady":       false,
		"hardware":         "",
		"lastEval":         "",
		"probeStatus":      "",
	})
}

func (s *webServer) handlePhiAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonErr(w, "method not allowed", 405)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/phi/")
	switch action {
	case "setup":
		out, err := s.runCLI(120*time.Second, "phi", "setup")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "install":
		out, err := s.runCLI(300*time.Second, "phi", "install")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "start":
		out, err := s.runCLI(30*time.Second, "phi", "start")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "stop":
		out, err := s.runCLI(10*time.Second, "phi", "stop")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "eval":
		out, err := s.runCLI(120*time.Second, "phi", "eval")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case "set-model":
		var body struct {
			Model string `json:"model"`
		}
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			p := tui.EnsureMapNested(cfg, "phi")
			p["local_model"] = body.Model
			return nil
		})
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonErr(w, "unknown phi action", 400)
	}
}

// ── Skills ──

type webSkill struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Description string `json:"description"`
	CanRemove   bool   `json:"canRemove"`
}

type webSkillsCounts struct {
	Total     int `json:"total"`
	Workspace int `json:"workspace"`
	Global    int `json:"global"`
	Builtin   int `json:"builtin"`
}

type webSkillsState struct {
	Workspace          string          `json:"workspace"`
	WorkspaceSkillsDir string          `json:"workspaceSkillsDir"`
	GlobalSkillsDir    string          `json:"globalSkillsDir"`
	BuiltinSkillsDir   string          `json:"builtinSkillsDir"`
	SourcePriority     []string        `json:"sourcePriority"`
	Counts             webSkillsCounts `json:"counts"`
	Installed          []webSkill      `json:"installed"`
}

func skillSourceRank(source string) int {
	switch source {
	case "workspace":
		return 0
	case "global":
		return 1
	case "builtin":
		return 2
	default:
		return 3
	}
}

func loadWebSkillsState() (*webSkillsState, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	workspace := cfg.WorkspacePath()
	globalDir := filepath.Dir(getConfigPath())
	globalSkillsDir := filepath.Join(globalDir, "skills")
	builtinSkillsDir := resolveBuiltinSkillsDir(workspace)
	loader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)
	installed := loader.ListSkills()

	sort.Slice(installed, func(i, j int) bool {
		left := installed[i]
		right := installed[j]
		if skillSourceRank(left.Source) != skillSourceRank(right.Source) {
			return skillSourceRank(left.Source) < skillSourceRank(right.Source)
		}
		return strings.ToLower(left.Name) < strings.ToLower(right.Name)
	})

	state := &webSkillsState{
		Workspace:          workspace,
		WorkspaceSkillsDir: filepath.Join(workspace, "skills"),
		GlobalSkillsDir:    globalSkillsDir,
		BuiltinSkillsDir:   builtinSkillsDir,
		SourcePriority:     []string{"workspace", "global", "builtin"},
		Installed:          make([]webSkill, 0, len(installed)),
	}

	for _, skill := range installed {
		switch skill.Source {
		case "workspace":
			state.Counts.Workspace++
		case "global":
			state.Counts.Global++
		case "builtin":
			state.Counts.Builtin++
		}
		state.Installed = append(state.Installed, webSkill{
			Name:        skill.Name,
			Source:      skill.Source,
			Path:        skill.Path,
			Description: skill.Description,
			CanRemove:   skill.Source == "workspace",
		})
	}
	state.Counts.Total = len(state.Installed)

	return state, nil
}

func (s *webServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		state, err := loadWebSkillsState()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, state)
	case http.MethodPost:
		var body struct {
			Path string `json:"path"`
		}
		readBody(r, &body)
		path := strings.TrimSpace(body.Path)
		if path == "" {
			jsonErr(w, "skill repo path required", 400)
			return
		}
		cfg, err := loadConfig()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		installer := skills.NewSkillInstaller(cfg.WorkspacePath())
		if err := installer.InstallFromGitHub(ctx, path); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, map[string]interface{}{"ok": true, "installed": filepath.Base(path)})
	default:
		jsonErr(w, "method not allowed", 405)
	}
}

func (s *webServer) handleSkillsAction(w http.ResponseWriter, r *http.Request) {
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/skills/"), "/")
	if name == "" {
		jsonErr(w, "skill name required", 400)
		return
	}
	decodedName, err := url.PathUnescape(name)
	if err != nil {
		jsonErr(w, "invalid skill name", 400)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		state, err := loadWebSkillsState()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		var target *webSkill
		for i := range state.Installed {
			if state.Installed[i].Name == decodedName {
				target = &state.Installed[i]
				break
			}
		}
		if target == nil {
			jsonErr(w, "skill not found", 404)
			return
		}
		if !target.CanRemove {
			jsonErr(w, "only workspace-installed skills can be removed from the web UI", 400)
			return
		}
		cfg, err := loadConfig()
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		installer := skills.NewSkillInstaller(cfg.WorkspacePath())
		if err := installer.Uninstall(decodedName); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		jsonResp(w, map[string]bool{"ok": true})
	default:
		jsonErr(w, "method not allowed", 405)
	}
}

// ── Cron ──

func (s *webServer) handleCron(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.runCLI(10*time.Second, "cron", "list", "--json")
		if err != nil {
			jsonResp(w, []interface{}{})
			return
		}
		var jobs interface{}
		if json.Unmarshal([]byte(out), &jobs) == nil {
			jsonResp(w, jobs)
		} else {
			jsonResp(w, []interface{}{})
		}
	case http.MethodPost:
		var body struct {
			Description string `json:"description"`
		}
		readBody(r, &body)
		out, err := s.runCLI(30*time.Second, "cron", "add", shellQuote(body.Description))
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	default:
		jsonErr(w, "method not allowed", 405)
	}
}

func (s *webServer) handleCronAction(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/cron/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 {
		jsonErr(w, "job id required", 400)
		return
	}
	id := parts[0]
	action := ""
	if len(parts) >= 2 {
		action = parts[1]
	}

	switch {
	case action == "toggle" && r.Method == http.MethodPost:
		out, err := s.runCLI(10*time.Second, "cron", "toggle", id)
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	case action == "" && r.Method == http.MethodDelete:
		out, err := s.runCLI(10*time.Second, "cron", "remove", id)
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	default:
		jsonErr(w, "unknown cron action", 400)
	}
}

// ── Routing ──

func (s *webServer) handleRouting(w http.ResponseWriter, r *http.Request) {
	type routingStatus struct {
		Enabled          bool   `json:"enabled"`
		UnmappedBehavior string `json:"unmappedBehavior"`
		TotalMappings    int    `json:"totalMappings"`
		InvalidMappings  int    `json:"invalidMappings"`
	}
	type routingMapping struct {
		ID             string   `json:"id"`
		Channel        string   `json:"channel"`
		ChatID         string   `json:"chatId"`
		Workspace      string   `json:"workspace"`
		AllowedSenders []string `json:"allowedSenders"`
		Label          string   `json:"label"`
		Mode           string   `json:"mode"`
		LocalBackend   string   `json:"localBackend"`
		LocalModel     string   `json:"localModel"`
		LocalPreset    string   `json:"localPreset"`
	}
	loadRouting := func() (*config.Config, error) {
		return loadConfig()
	}
	buildRoutingStatus := func(cfg *config.Config) routingStatus {
		if cfg == nil {
			return routingStatus{Enabled: false, UnmappedBehavior: config.RoutingUnmappedBehaviorBlock}
		}
		invalid := 0
		for _, m := range cfg.Routing.Mappings {
			tmp := config.RoutingConfig{
				Enabled:          cfg.Routing.Enabled,
				UnmappedBehavior: cfg.Routing.UnmappedBehavior,
				Mappings:         []config.RoutingMapping{m},
			}
			if err := config.ValidateRoutingConfig(tmp); err != nil {
				invalid++
			}
		}
		return routingStatus{
			Enabled:          cfg.Routing.Enabled,
			UnmappedBehavior: cfg.Routing.UnmappedBehavior,
			TotalMappings:    len(cfg.Routing.Mappings),
			InvalidMappings:  invalid,
		}
	}
	buildRoutingMappings := func(cfg *config.Config) []routingMapping {
		if cfg == nil || len(cfg.Routing.Mappings) == 0 {
			return []routingMapping{}
		}
		mappings := append([]config.RoutingMapping(nil), cfg.Routing.Mappings...)
		sort.Slice(mappings, func(i, j int) bool {
			ki := strings.ToLower(mappings[i].Channel) + ":" + mappings[i].ChatID
			kj := strings.ToLower(mappings[j].Channel) + ":" + mappings[j].ChatID
			return ki < kj
		})
		out := make([]routingMapping, 0, len(mappings))
		for _, m := range mappings {
			out = append(out, routingMapping{
				ID:             strings.ToLower(strings.TrimSpace(m.Channel)) + ":" + strings.TrimSpace(m.ChatID),
				Channel:        m.Channel,
				ChatID:         m.ChatID,
				Workspace:      m.Workspace,
				AllowedSenders: append([]string(nil), []string(m.AllowedSenders)...),
				Label:          m.Label,
				Mode:           mappingModeDisplay(m),
				LocalBackend:   m.LocalBackend,
				LocalModel:     m.LocalModel,
				LocalPreset:    m.LocalPreset,
			})
		}
		return out
	}

	action := strings.TrimPrefix(r.URL.Path, "/api/routing/")
	switch action {
	case "status":
		cfg, err := loadRouting()
		if err != nil {
			jsonErr(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonResp(w, buildRoutingStatus(cfg))
	case "mappings":
		if r.Method == http.MethodGet {
			cfg, err := loadRouting()
			if err != nil {
				jsonErr(w, err.Error(), http.StatusInternalServerError)
				return
			}
			jsonResp(w, buildRoutingMappings(cfg))
		} else if r.Method == http.MethodPost {
			var body map[string]interface{}
			readBody(r, &body)
			// Build routing add command from body
			args := []string{"routing", "add"}
			if ch, ok := body["channel"].(string); ok {
				args = append(args, "--channel", ch)
			}
			if cid, ok := body["chatId"].(string); ok {
				args = append(args, "--chat-id", cid)
			}
			if ws, ok := body["workspace"].(string); ok {
				args = append(args, "--workspace", ws)
			}
			if label, ok := body["label"].(string); ok && label != "" {
				args = append(args, "--label", shellQuote(label))
			}
			out, err := s.runCLI(10*time.Second, args...)
			jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
		} else {
			jsonErr(w, "method not allowed", 405)
		}
	case "reload":
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", 405)
			return
		}
		out, err := s.runCLI(10*time.Second, "routing", "reload")
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
	default:
		// Handle /api/routing/mappings/{id}
		if strings.HasPrefix(action, "mappings/") {
			id := strings.TrimPrefix(action, "mappings/")
			if r.Method == http.MethodDelete {
				parts := strings.SplitN(id, ":", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
					jsonErr(w, "invalid routing id", http.StatusBadRequest)
					return
				}
				out, err := s.runCLI(10*time.Second, "routing", "remove", "--channel", parts[0], "--chat-id", parts[1])
				jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
				return
			}
		}
		jsonErr(w, "unknown routing action", 400)
	}
}

// ── Settings ──

func (s *webServer) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := tui.ReadConfigMap(s.exec)
		if err != nil {
			cfg = map[string]interface{}{}
		}
		snap := s.getSnapshot()
		channels := tui.GetMapNested(cfg, "channels")
		routingCfg := tui.GetMapNested(cfg, "routing")
		agentCfg := tui.GetMapNested(cfg, "agents", "defaults")
		integrations := tui.GetMapNested(cfg, "integrations")

		jsonResp(w, map[string]interface{}{
			"discord":  map[string]interface{}{"enabled": tui.GetBool(channels, "discord", "enabled")},
			"telegram": map[string]interface{}{"enabled": tui.GetBool(channels, "telegram", "enabled")},
			"routing": map[string]interface{}{
				"enabled":          tui.GetBool(routingCfg, "enabled"),
				"unmappedBehavior": tui.GetString(routingCfg, "unmapped_behavior"),
			},
			"agent": map[string]interface{}{
				"defaultModel":    tui.GetString(agentCfg, "model"),
				"reasoningEffort": tui.GetString(agentCfg, "reasoning_effort"),
			},
			"integrations": map[string]interface{}{
				"pubmedApiKey": tui.GetString(integrations, "pubmed_api_key"),
			},
			"service": map[string]interface{}{
				"autoStart": snap.ServiceAutoStart,
				"installed": snap.ServiceInstalled,
				"running":   snap.ServiceRunning,
			},
			"general": map[string]interface{}{
				"workspacePath": snap.WorkspacePath,
			},
		})

	case http.MethodPut:
		var body struct {
			Path  string      `json:"path"`
			Value interface{} `json:"value"`
		}
		if err := readBody(r, &body); err != nil {
			jsonErr(w, "invalid request", 400)
			return
		}

		// Map dotted paths to config structure
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			switch body.Path {
			case "discord.enabled":
				ch := tui.EnsureMapNested(cfg, "channels", "discord")
				ch["enabled"] = body.Value
			case "telegram.enabled":
				ch := tui.EnsureMapNested(cfg, "channels", "telegram")
				ch["enabled"] = body.Value
			case "routing.enabled":
				r := tui.EnsureMapNested(cfg, "routing")
				r["enabled"] = body.Value
			case "routing.unmappedBehavior":
				r := tui.EnsureMapNested(cfg, "routing")
				r["unmapped_behavior"] = body.Value
			case "agent.defaultModel":
				a := tui.EnsureMapNested(cfg, "agents", "defaults")
				a["model"] = body.Value
			case "agent.reasoningEffort":
				a := tui.EnsureMapNested(cfg, "agents", "defaults")
				a["reasoning_effort"] = body.Value
			case "integrations.pubmedApiKey":
				i := tui.EnsureMapNested(cfg, "integrations")
				i["pubmed_api_key"] = body.Value
			case "service.autoStart":
				svc := tui.EnsureMapNested(cfg, "service")
				svc["auto_start"] = body.Value
			default:
				return fmt.Errorf("unknown setting: %s", body.Path)
			}
			return nil
		})
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		s.invalidateSnapshot()
		jsonResp(w, map[string]bool{"ok": true})

	default:
		jsonErr(w, "method not allowed", 405)
	}
}

// ── System (Personality Files) ──

// knownSystemFiles is the hardcoded allowlist of workspace personality files.
var knownSystemFiles = []string{
	"AGENTS.md",
	"SOUL.md",
	"USER.md",
	"IDENTITY.md",
	"TOOLS.md",
	"HOOKS.md",
	"memory/MEMORY.md",
}

type workspaceFileInfo struct {
	Name            string `json:"name"`
	RelativePath    string `json:"relativePath"`
	AbsolutePath    string `json:"absolutePath"`
	Source          string `json:"source"`
	Size            int64  `json:"size"`
	ModTime         string `json:"modTime"`
	Content         string `json:"content"`
	Status          string `json:"status"` // current | customized | missing
	HasTemplate     bool   `json:"hasTemplate"`
	TemplateContent string `json:"templateContent"`
}

type systemFilesResponse struct {
	PrimaryWorkspace  string `json:"primaryWorkspace"`
	SharedWorkspace   string `json:"sharedWorkspace"`
	RoutingWorkspaces []struct {
		Label     string `json:"label"`
		Workspace string `json:"workspace"`
		Channel   string `json:"channel"`
		ChatID    string `json:"chatId"`
	} `json:"routingWorkspaces"`
	ActiveWorkspace string              `json:"activeWorkspace"`
	Files           []workspaceFileInfo `json:"files"`
}

func collectWorkspaceFiles(workspace, source string, templates []workspacetpl.Template) []workspaceFileInfo {
	tplMap := make(map[string]string, len(templates))
	for _, t := range templates {
		tplMap[t.RelativePath] = t.Content
	}

	files := make([]workspaceFileInfo, 0, len(knownSystemFiles))
	for _, rel := range knownSystemFiles {
		abs := filepath.Join(workspace, rel)
		info := workspaceFileInfo{
			Name:         filepath.Base(rel),
			RelativePath: rel,
			AbsolutePath: abs,
			Source:       source,
		}
		tplContent, hasTpl := tplMap[rel]
		info.HasTemplate = hasTpl
		info.TemplateContent = tplContent

		data, err := os.ReadFile(abs)
		if err != nil {
			info.Status = "missing"
		} else {
			info.Content = string(data)
			info.Size = int64(len(data))
			if fi, statErr := os.Stat(abs); statErr == nil {
				info.ModTime = fi.ModTime().UTC().Format(time.RFC3339)
			}
			if hasTpl && info.Content == tplContent {
				info.Status = "current"
			} else {
				info.Status = "customized"
			}
		}
		files = append(files, info)
	}
	return files
}

func (s *webServer) handleSystem(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", 405)
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	templates, err := workspacetpl.Load()
	if err != nil {
		jsonErr(w, "failed to load templates: "+err.Error(), 500)
		return
	}

	primary := filepath.Clean(cfg.WorkspacePath())
	shared := filepath.Clean(cfg.SharedWorkspacePath())

	// Build routing workspace list (deduplicate by path).
	type routingEntry struct {
		Label     string `json:"label"`
		Workspace string `json:"workspace"`
		Channel   string `json:"channel"`
		ChatID    string `json:"chatId"`
	}
	seen := map[string]bool{}
	var routingWorkspaces []routingEntry
	for _, m := range cfg.Routing.Mappings {
		wp := m.Workspace
		if wp == "" {
			continue
		}
		expanded := wp
		if strings.HasPrefix(expanded, "~/") {
			home, _ := os.UserHomeDir()
			expanded = filepath.Join(home, expanded[2:])
		}
		if seen[expanded] {
			continue
		}
		seen[expanded] = true
		label := m.Label
		if label == "" {
			label = m.Channel + ":" + m.ChatID
		}
		routingWorkspaces = append(routingWorkspaces, routingEntry{
			Label:     label,
			Workspace: expanded,
			Channel:   m.Channel,
			ChatID:    m.ChatID,
		})
	}

	// Determine active workspace from query param, default to primary.
	activeWorkspace := r.URL.Query().Get("workspace")
	if activeWorkspace == "" {
		activeWorkspace = primary
	}

	// Validate the requested workspace against configured ones.
	allowed := []string{primary, shared}
	for _, rw := range routingWorkspaces {
		allowed = append(allowed, rw.Workspace)
	}
	valid := false
	for _, a := range allowed {
		if a == activeWorkspace {
			valid = true
			break
		}
	}
	if !valid {
		activeWorkspace = primary
	}

	// Determine source label for active workspace.
	source := "primary"
	if activeWorkspace == shared && shared != primary {
		source = "shared"
	}
	for _, rw := range routingWorkspaces {
		if rw.Workspace == activeWorkspace {
			source = "routing"
			break
		}
	}

	files := collectWorkspaceFiles(activeWorkspace, source, templates)

	resp := systemFilesResponse{
		PrimaryWorkspace: primary,
		SharedWorkspace:  shared,
		ActiveWorkspace:  activeWorkspace,
		Files:            files,
	}
	// Marshal routing workspaces into the anonymous struct slice.
	type routingSliceEntry struct {
		Label     string `json:"label"`
		Workspace string `json:"workspace"`
		Channel   string `json:"channel"`
		ChatID    string `json:"chatId"`
	}
	routingSlice := make([]routingSliceEntry, len(routingWorkspaces))
	for i, rw := range routingWorkspaces {
		routingSlice[i] = routingSliceEntry(rw)
	}

	jsonResp(w, map[string]interface{}{
		"primaryWorkspace":  resp.PrimaryWorkspace,
		"sharedWorkspace":   resp.SharedWorkspace,
		"routingWorkspaces": routingSlice,
		"activeWorkspace":   resp.ActiveWorkspace,
		"files":             resp.Files,
	})
}

func (s *webServer) handleSystemAction(w http.ResponseWriter, r *http.Request) {
	// Extract the action segment: /api/system/file or /api/system/reset
	action := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/system/"), "/")

	cfg, err := loadConfig()
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	primary := filepath.Clean(cfg.WorkspacePath())
	shared := filepath.Clean(cfg.SharedWorkspacePath())
	sharedReadOnly := cfg.Agents.Defaults.SharedWorkspaceReadOnly

	// Build allowed workspace set (writable).
	// Reject empty or relative workspace paths.
	allowed := make(map[string]bool)
	if filepath.IsAbs(primary) {
		allowed[primary] = true
	}
	if filepath.IsAbs(shared) && !sharedReadOnly {
		allowed[shared] = true
	}
	for _, m := range cfg.Routing.Mappings {
		wp := m.Workspace
		if wp == "" {
			continue
		}
		if strings.HasPrefix(wp, "~/") {
			home, _ := os.UserHomeDir()
			wp = filepath.Join(home, wp[2:])
		}
		wp = filepath.Clean(wp)
		if filepath.IsAbs(wp) {
			allowed[wp] = true
		}
	}

	switch action {
	case "file":
		if r.Method != http.MethodPut {
			jsonErr(w, "method not allowed", 405)
			return
		}
		var body struct {
			Workspace string `json:"workspace"`
			Path      string `json:"path"`
			Content   string `json:"content"`
		}
		if err := readBody(r, &body); err != nil {
			jsonErr(w, "invalid request body", 400)
			return
		}

		// Validate path against allowlist.
		validPath := false
		for _, known := range knownSystemFiles {
			if body.Path == known {
				validPath = true
				break
			}
		}
		if !validPath {
			jsonErr(w, "file path not allowed", 400)
			return
		}

		// Validate workspace.
		if !allowed[body.Workspace] {
			jsonErr(w, "workspace not allowed", 400)
			return
		}

		abs := filepath.Join(body.Workspace, body.Path)
		// Ensure parent directory exists (for memory/MEMORY.md).
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			jsonErr(w, "failed to create directory: "+err.Error(), 500)
			return
		}
		if err := os.WriteFile(abs, []byte(body.Content), 0644); err != nil {
			jsonErr(w, "failed to write file: "+err.Error(), 500)
			return
		}
		jsonResp(w, map[string]interface{}{"ok": true})

	case "reset":
		if r.Method != http.MethodPost {
			jsonErr(w, "method not allowed", 405)
			return
		}
		var body struct {
			Workspace string `json:"workspace"`
			Path      string `json:"path"`
		}
		if err := readBody(r, &body); err != nil {
			jsonErr(w, "invalid request body", 400)
			return
		}

		// Validate path against allowlist.
		validPath := false
		for _, known := range knownSystemFiles {
			if body.Path == known {
				validPath = true
				break
			}
		}
		if !validPath {
			jsonErr(w, "file path not allowed", 400)
			return
		}

		// Validate workspace.
		if !allowed[body.Workspace] {
			jsonErr(w, "workspace not allowed", 400)
			return
		}

		templates, err := workspacetpl.Load()
		if err != nil {
			jsonErr(w, "failed to load templates: "+err.Error(), 500)
			return
		}

		var tplContent string
		found := false
		for _, t := range templates {
			if t.RelativePath == body.Path {
				tplContent = t.Content
				found = true
				break
			}
		}
		if !found {
			jsonErr(w, "no template found for this file", 404)
			return
		}

		abs := filepath.Join(body.Workspace, body.Path)
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			jsonErr(w, "failed to create directory: "+err.Error(), 500)
			return
		}
		if err := os.WriteFile(abs, []byte(tplContent), 0644); err != nil {
			jsonErr(w, "failed to write file: "+err.Error(), 500)
			return
		}
		jsonResp(w, map[string]interface{}{"ok": true})

	default:
		jsonErr(w, "unknown action", 404)
	}
}

// ── Static Files ──

func (s *webServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil || s.staticFS == nil {
		http.NotFound(w, r)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	if _, err := iofs.Stat(s.staticFS, path); err != nil {
		data, readErr := iofs.ReadFile(s.staticFS, "index.html")
		if readErr != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(data))
		return
	}

	s.static.ServeHTTP(w, r)
}

func (s *webServer) invalidateSnapshot() {
	s.snapMu.Lock()
	s.snapshot = nil
	s.snapMu.Unlock()
}
