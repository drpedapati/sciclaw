package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/cmd/picoclaw/tui"
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
	exec     tui.Executor
	mux      *http.ServeMux
	distDir  string

	// Cached snapshot
	snapMu   sync.RWMutex
	snapshot  *tui.VMSnapshot
	snapTime time.Time
}

func newWebServer(exec tui.Executor, distDir string) *webServer {
	s := &webServer{
		exec:    exec,
		mux:     http.NewServeMux(),
		distDir: distDir,
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
	s.mux.HandleFunc("/api/models", s.handleModels)
	s.mux.HandleFunc("/api/models/", s.handleModelsAction)
	s.mux.HandleFunc("/api/phi", s.handlePhi)
	s.mux.HandleFunc("/api/phi/", s.handlePhiAction)
	s.mux.HandleFunc("/api/skills", s.handleSkills)
	s.mux.HandleFunc("/api/cron", s.handleCron)
	s.mux.HandleFunc("/api/cron/", s.handleCronAction)
	s.mux.HandleFunc("/api/routing/", s.handleRouting)
	s.mux.HandleFunc("/api/settings", s.handleSettings)
	s.mux.HandleFunc("/api/home/onboard", s.handleOnboard)

	// Static files
	if s.distDir != "" {
		s.mux.HandleFunc("/", s.handleStatic)
	}
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

func (s *webServer) runCLI(timeout time.Duration, args ...string) (string, error) {
	cmd := "HOME=" + s.exec.HomePath() + " " + s.exec.BinaryPath() + " " + strings.Join(args, " ") + " 2>&1"
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
	var body struct{ Model string `json:"model"` }
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
	var body struct{ Message string `json:"message"` }
	if err := readBody(r, &body); err != nil || body.Message == "" {
		jsonErr(w, "message required", 400)
		return
	}

	out, err := s.runCLI(120*time.Second, "agent", "-m", shellQuote(body.Message), "-s", "web:chat")
	if err != nil {
		jsonResp(w, map[string]interface{}{"response": "Error: " + err.Error() + "\n" + out})
		return
	}
	jsonResp(w, map[string]interface{}{"response": out})
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
			"enabled":     tui.GetBool(email, "enabled"),
			"provider":    tui.GetString(email, "provider"),
			"address":     tui.GetString(email, "address"),
			"displayName": tui.GetString(email, "display_name"),
			"hasApiKey":   tui.GetString(email, "api_key") != "",
			"baseUrl":     tui.GetString(email, "base_url"),
			"allowFrom":   tui.GetStringSliceValue(email, "allow_from"),
		}
		jsonResp(w, resp)

	case http.MethodPut:
		var body map[string]interface{}
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			email := tui.EnsureMapNested(cfg, "channels", "email")
			for k, v := range body {
				switch k {
				case "enabled":
					email["enabled"] = v
				case "address":
					email["address"] = v
				case "displayName":
					email["display_name"] = v
				case "apiKey":
					email["api_key"] = v
				case "provider":
					email["provider"] = v
				case "baseUrl":
					email["base_url"] = v
				}
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
	var body struct{ To string `json:"to"` }
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
		var body struct{ Key string `json:"key"` }
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
		"version": s.exec.AgentVersion(),
		"os":      "",
		"arch":    "",
		"checks":  []interface{}{},
		"raw":     out,
		"passed":  0,
		"warnings": 0,
		"errors":  0,
		"skipped": 0,
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

// ── Models ──

func (s *webServer) handleModels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		snap := s.getSnapshot()
		jsonResp(w, map[string]string{
			"current":    snap.ActiveModel,
			"provider":   snap.ActiveProvider,
			"effort":     "",
			"authMethod": "",
		})
	case http.MethodPut:
		var body struct{ Model string `json:"model"` }
		readBody(r, &body)
		err := tui.UpdateConfigMap(s.exec, func(cfg map[string]interface{}) error {
			defaults := tui.EnsureMapNested(cfg, "agents", "defaults")
			defaults["model"] = body.Model
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

func (s *webServer) handleModelsAction(w http.ResponseWriter, r *http.Request) {
	action := strings.TrimPrefix(r.URL.Path, "/api/models/")
	switch action {
	case "catalog":
		out, err := s.runCLI(15*time.Second, "models", "list", "--json")
		if err != nil {
			jsonResp(w, []interface{}{})
			return
		}
		var catalog interface{}
		if json.Unmarshal([]byte(out), &catalog) == nil {
			jsonResp(w, catalog)
		} else {
			jsonResp(w, []interface{}{})
		}
	case "effort":
		if r.Method != http.MethodPut {
			jsonErr(w, "method not allowed", 405)
			return
		}
		var body struct{ Effort string `json:"effort"` }
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
		var body struct{ Model string `json:"model"` }
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

func (s *webServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		out, err := s.runCLI(10*time.Second, "skills", "list", "--json")
		if err != nil {
			jsonResp(w, []interface{}{})
			return
		}
		var skills interface{}
		if json.Unmarshal([]byte(out), &skills) == nil {
			jsonResp(w, skills)
		} else {
			jsonResp(w, []interface{}{})
		}
	case http.MethodPost:
		var body struct{ Path string `json:"path"` }
		readBody(r, &body)
		out, err := s.runCLI(30*time.Second, "skills", "install", shellQuote(body.Path))
		jsonResp(w, map[string]interface{}{"ok": err == nil, "output": out})
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
		var body struct{ Description string `json:"description"` }
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
	action := strings.TrimPrefix(r.URL.Path, "/api/routing/")
	switch action {
	case "status":
		out, _ := s.runCLI(10*time.Second, "routing", "status", "--json")
		var status interface{}
		if json.Unmarshal([]byte(out), &status) == nil {
			jsonResp(w, status)
		} else {
			jsonResp(w, map[string]interface{}{
				"enabled":         false,
				"unmappedBehavior": "block",
				"totalMappings":   0,
				"invalidMappings": 0,
			})
		}
	case "mappings":
		if r.Method == http.MethodGet {
			out, _ := s.runCLI(10*time.Second, "routing", "list", "--json")
			var mappings interface{}
			if json.Unmarshal([]byte(out), &mappings) == nil {
				jsonResp(w, mappings)
			} else {
				jsonResp(w, []interface{}{})
			}
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
				out, err := s.runCLI(10*time.Second, "routing", "remove", id)
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

// ── Static Files ──

func (s *webServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" {
		path = "/index.html"
	}
	filePath := filepath.Join(s.distDir, path)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// SPA fallback - serve index.html for client-side routing
		http.ServeFile(w, r, filepath.Join(s.distDir, "index.html"))
		return
	}

	http.ServeFile(w, r, filePath)
}

func (s *webServer) invalidateSnapshot() {
	s.snapMu.Lock()
	s.snapshot = nil
	s.snapMu.Unlock()
}
