package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type Session struct {
	Key       string              `json:"key"`
	Messages  []providers.Message `json:"messages"`
	Artifacts []Artifact          `json:"artifacts,omitempty"`
	Summary   string              `json:"summary,omitempty"`
	Created   time.Time           `json:"created"`
	Updated   time.Time           `json:"updated"`
}

type Artifact struct {
	Role    string    `json:"role"`
	Path    string    `json:"path"`
	Label   string    `json:"label,omitempty"`
	Source  string    `json:"source,omitempty"`
	Updated time.Time `json:"updated"`
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	storage  string
}

var (
	// Keep gateway startup/routing responsive even if cloud-backed folders stall.
	sessionLoadTimeout  = 750 * time.Millisecond
	sessionSaveWarnTime = 750 * time.Millisecond
	errSessionLoadTimed = errors.New("session load timed out")
	readDir             = os.ReadDir
	readFile            = os.ReadFile
)

func NewSessionManager(storage string) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		storage:  storage,
	}

	if storage != "" {
		os.MkdirAll(storage, 0755)
		if err := sm.loadSessionsWithTimeout(sessionLoadTimeout); err != nil {
			logger.WarnCF("session", "Session preload skipped", map[string]interface{}{
				"storage": storage,
				"error":   err.Error(),
			})
		}
	}

	return sm
}

func (sm *SessionManager) GetOrCreate(key string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		return session
	}

	session = &Session{
		Key:      key,
		Messages: []providers.Message{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	sm.sessions[key] = session

	return session
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddFullMessage(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

// AddFullMessage adds a complete message with tool calls and tool call ID to the session.
// This is used to save the full conversation flow including tool calls and tool results.
func (sm *SessionManager) AddFullMessage(sessionKey string, msg providers.Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionKey]
	if !ok {
		session = &Session{
			Key:      sessionKey,
			Messages: []providers.Message{},
			Created:  time.Now(),
		}
		sm.sessions[sessionKey] = session
	}

	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now()
}

func (sm *SessionManager) RegisterArtifacts(sessionKey string, artifacts ...Artifact) {
	if len(artifacts) == 0 {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionKey]
	if !ok {
		session = &Session{
			Key:      sessionKey,
			Messages: []providers.Message{},
			Created:  time.Now(),
		}
		sm.sessions[sessionKey] = session
	}

	now := time.Now()
	for _, artifact := range artifacts {
		artifact.Path = filepath.Clean(strings.TrimSpace(artifact.Path))
		if artifact.Path == "" {
			continue
		}
		artifact.Role = strings.TrimSpace(artifact.Role)
		if artifact.Role == "" {
			artifact.Role = "input"
		}
		artifact.Label = strings.TrimSpace(artifact.Label)
		if artifact.Label == "" {
			artifact.Label = filepath.Base(artifact.Path)
		}
		artifact.Source = strings.TrimSpace(artifact.Source)
		if artifact.Updated.IsZero() {
			artifact.Updated = now
		}

		replaced := false
		for i := range session.Artifacts {
			if session.Artifacts[i].Path != artifact.Path {
				continue
			}
			session.Artifacts[i].Role = artifact.Role
			session.Artifacts[i].Updated = artifact.Updated
			if artifact.Label != "" {
				session.Artifacts[i].Label = artifact.Label
			}
			if artifact.Source != "" {
				session.Artifacts[i].Source = artifact.Source
			}
			replaced = true
			break
		}
		if !replaced {
			session.Artifacts = append(session.Artifacts, artifact)
		}
	}

	session.Updated = now
}

func (sm *SessionManager) GetArtifacts(key string) []Artifact {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok || len(session.Artifacts) == 0 {
		return []Artifact{}
	}

	artifacts := make([]Artifact, len(session.Artifacts))
	copy(artifacts, session.Artifacts)
	return artifacts
}

func (sm *SessionManager) BuildArtifactContext(key, workspace string) string {
	artifacts := sm.GetArtifacts(key)
	if len(artifacts) == 0 {
		return ""
	}

	filtered := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		if _, err := os.Stat(artifact.Path); err != nil {
			continue
		}
		filtered = append(filtered, artifact)
	}
	if len(filtered) == 0 {
		return ""
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Role != filtered[j].Role {
			return filtered[i].Role < filtered[j].Role
		}
		if !filtered[i].Updated.Equal(filtered[j].Updated) {
			return filtered[i].Updated.After(filtered[j].Updated)
		}
		return filtered[i].Path < filtered[j].Path
	})

	inputs := make([]Artifact, 0, len(filtered))
	outputs := make([]Artifact, 0, len(filtered))
	for _, artifact := range filtered {
		switch artifact.Role {
		case "output":
			outputs = append(outputs, artifact)
		default:
			inputs = append(inputs, artifact)
		}
	}

	var b strings.Builder
	b.WriteString("## Working Artifacts\n\n")
	b.WriteString("Use these canonical local paths instead of any older remote attachment URLs or stale filenames from prior turns.\n")
	if len(inputs) > 0 {
		b.WriteString("\nInputs:\n")
		for _, line := range formatArtifactLines(inputs, workspace, 6) {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	if len(outputs) > 0 {
		b.WriteString("\nOutputs:\n")
		for _, line := range formatArtifactLines(outputs, workspace, 6) {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	return strings.TrimSpace(b.String())
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return []providers.Message{}
	}

	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

// ListKeys returns all known session keys in stable order.
func (sm *SessionManager) ListKeys() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	keys := make([]string, 0, len(sm.sessions))
	for key := range sm.sessions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Snapshot returns a deep copy of one session if it exists.
func (sm *SessionManager) Snapshot(key string) (Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stored, ok := sm.sessions[key]
	if !ok || stored == nil {
		return Session{}, false
	}

	out := Session{
		Key:     stored.Key,
		Summary: stored.Summary,
		Created: stored.Created,
		Updated: stored.Updated,
	}
	if len(stored.Messages) > 0 {
		out.Messages = make([]providers.Message, len(stored.Messages))
		copy(out.Messages, stored.Messages)
	} else {
		out.Messages = []providers.Message{}
	}
	if len(stored.Artifacts) > 0 {
		out.Artifacts = make([]Artifact, len(stored.Artifacts))
		copy(out.Artifacts, stored.Artifacts)
	} else {
		out.Artifacts = []Artifact{}
	}
	return out, true
}

// ReplaceHistory replaces the message history for a session.
func (sm *SessionManager) ReplaceHistory(sessionKey string, history []providers.Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stored, ok := sm.sessions[sessionKey]
	if !ok || stored == nil {
		stored = &Session{
			Key:     sessionKey,
			Created: time.Now(),
		}
		sm.sessions[sessionKey] = stored
	}

	if len(history) == 0 {
		stored.Messages = []providers.Message{}
	} else {
		stored.Messages = make([]providers.Message, len(history))
		copy(stored.Messages, history)
	}
	stored.Updated = time.Now()
}

func (sm *SessionManager) GetSummary(key string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return ""
	}
	return session.Summary
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		session.Summary = summary
		session.Updated = time.Now()
	}
}

func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if !ok {
		return
	}

	if keepLast <= 0 {
		session.Messages = []providers.Message{}
		session.Updated = time.Now()
		return
	}

	if len(session.Messages) <= keepLast {
		return
	}

	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.Updated = time.Now()
}

func (sm *SessionManager) Save(key string) error {
	if sm.storage == "" {
		return nil
	}
	saveStartedAt := time.Now()

	// Validate key to avoid invalid filenames and path traversal.
	if key == "" || key == "." || key == ".." || key != filepath.Base(key) || strings.Contains(key, "/") || strings.Contains(key, "\\") {
		return os.ErrInvalid
	}

	if strings.HasPrefix(key, "discord:") {
		logger.InfoCF("session", "Session save start", map[string]interface{}{
			"session_key": key,
			"storage":     sm.storage,
		})
	}

	// Snapshot under read lock, then perform slow file I/O after unlock.
	sm.mu.RLock()
	stored, ok := sm.sessions[key]
	if !ok {
		sm.mu.RUnlock()
		return nil
	}

	snapshot := Session{
		Key:     stored.Key,
		Summary: stored.Summary,
		Created: stored.Created,
		Updated: stored.Updated,
	}
	if len(stored.Messages) > 0 {
		snapshot.Messages = make([]providers.Message, len(stored.Messages))
		copy(snapshot.Messages, stored.Messages)
	} else {
		snapshot.Messages = []providers.Message{}
	}
	if len(stored.Artifacts) > 0 {
		snapshot.Artifacts = make([]Artifact, len(stored.Artifacts))
		copy(snapshot.Artifacts, stored.Artifacts)
	} else {
		snapshot.Artifacts = []Artifact{}
	}
	sm.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save marshal failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}

	sessionPath := filepath.Join(sm.storage, key+".json")
	tmpFile, err := os.CreateTemp(sm.storage, "session-*.tmp")
	if err != nil {
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save temp file create failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}

	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save write failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}
	if err := tmpFile.Chmod(0644); err != nil {
		_ = tmpFile.Close()
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save chmod failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save fsync failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}
	if err := tmpFile.Close(); err != nil {
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save close failed", map[string]interface{}{
				"session_key": key,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}

	if err := os.Rename(tmpPath, sessionPath); err != nil {
		if strings.HasPrefix(key, "discord:") {
			logger.WarnCF("session", "Session save rename failed", map[string]interface{}{
				"session_key": key,
				"path":        sessionPath,
				"error":       err.Error(),
				"duration_ms": time.Since(saveStartedAt).Milliseconds(),
			})
		}
		return err
	}
	cleanup = false
	if strings.HasPrefix(key, "discord:") {
		elapsed := time.Since(saveStartedAt)
		fields := map[string]interface{}{
			"session_key": key,
			"path":        sessionPath,
			"duration_ms": elapsed.Milliseconds(),
		}
		if elapsed >= sessionSaveWarnTime {
			logger.WarnCF("session", "Session save completed slowly", fields)
		} else {
			logger.InfoCF("session", "Session save complete", fields)
		}
	}
	return nil
}

func (sm *SessionManager) loadSessions() error {
	files, err := readDir(sm.storage)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		sessionPath := filepath.Join(sm.storage, file.Name())
		data, err := readFile(sessionPath)
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		sm.mu.Lock()
		sm.sessions[session.Key] = &session
		sm.mu.Unlock()
	}

	return nil
}

func (sm *SessionManager) loadSessionsWithTimeout(timeout time.Duration) error {
	if timeout <= 0 {
		return sm.loadSessions()
	}

	done := make(chan error, 1)
	go func() {
		done <- sm.loadSessions()
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return errSessionLoadTimed
	}
}

func formatArtifactLines(artifacts []Artifact, workspace string, limit int) []string {
	if len(artifacts) == 0 {
		return nil
	}
	if limit <= 0 || limit > len(artifacts) {
		limit = len(artifacts)
	}

	lines := make([]string, 0, limit+1)
	for _, artifact := range artifacts[:limit] {
		displayPath := displayArtifactPath(workspace, artifact.Path)
		label := strings.TrimSpace(artifact.Label)
		if label != "" && label != displayPath {
			lines = append(lines, "- "+label+" -> "+displayPath)
			continue
		}
		lines = append(lines, "- "+displayPath)
	}
	if len(artifacts) > limit {
		lines = append(lines, fmt.Sprintf("- ... +%d more", len(artifacts)-limit))
	}
	return lines
}

func displayArtifactPath(workspace, artifactPath string) string {
	cleanPath := filepath.Clean(strings.TrimSpace(artifactPath))
	if cleanPath == "" {
		return ""
	}
	if strings.TrimSpace(workspace) != "" {
		if rel, err := filepath.Rel(workspace, cleanPath); err == nil {
			rel = filepath.Clean(rel)
			if rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				return filepath.ToSlash(rel)
			}
		}
	}
	return filepath.ToSlash(cleanPath)
}
