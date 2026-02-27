package discordarchive

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
)

const (
	maxArchiveMessageChars = 2000
)

type Manager struct {
	workspace string
	cfg       config.DiscordArchiveConfig
	sessions  *session.SessionManager
}

type SessionStat struct {
	SessionKey string
	Messages   int
	Tokens     int
	OverLimit  bool
}

type ArchiveResult struct {
	SessionKey       string
	ArchivedMessages int
	KeptMessages     int
	ArchivePath      string
	TokensBefore     int
	TokensAfter      int
	OverLimit        bool
	DryRun           bool
}

type RecallHit struct {
	SessionKey string `json:"session_key"`
	SourcePath string `json:"source_path"`
	Score      int    `json:"score"`
	Text       string `json:"text"`
}

type archiveState struct {
	Sessions map[string]archiveSessionState `json:"sessions"`
}

type archiveSessionState struct {
	LastArchivedAt     string `json:"last_archived_at"`
	LastArchivePath    string `json:"last_archive_path"`
	ArchivedMessages   int    `json:"archived_messages"`
	KeptMessages       int    `json:"kept_messages"`
	TokensBefore       int    `json:"tokens_before"`
	TokensAfter        int    `json:"tokens_after"`
	LastOverLimitState bool   `json:"last_over_limit_state"`
}

func NewManager(workspace string, sm *session.SessionManager, cfg config.DiscordArchiveConfig) *Manager {
	return &Manager{
		workspace: workspace,
		cfg:       cfg,
		sessions:  sm,
	}
}

func (m *Manager) ListDiscordSessions(overLimitOnly bool) []SessionStat {
	if m == nil || m.sessions == nil {
		return nil
	}
	stats := make([]SessionStat, 0)
	for _, key := range m.sessions.ListKeys() {
		if !strings.HasPrefix(key, "discord:") {
			continue
		}
		snap, ok := m.sessions.Snapshot(key)
		if !ok {
			continue
		}
		msgCount := len(snap.Messages)
		tokenCount := estimateTokens(snap.Messages)
		over := tokenCount >= m.cfg.MaxSessionTokens || msgCount >= m.cfg.MaxSessionMessages
		if overLimitOnly && !over {
			continue
		}
		stats = append(stats, SessionStat{
			SessionKey: key,
			Messages:   msgCount,
			Tokens:     tokenCount,
			OverLimit:  over,
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Tokens == stats[j].Tokens {
			return stats[i].SessionKey < stats[j].SessionKey
		}
		return stats[i].Tokens > stats[j].Tokens
	})
	return stats
}

func (m *Manager) MaybeArchiveSession(sessionKey string) (*ArchiveResult, error) {
	if m == nil || m.sessions == nil {
		return nil, nil
	}
	if !strings.HasPrefix(sessionKey, "discord:") {
		return nil, nil
	}
	snap, ok := m.sessions.Snapshot(sessionKey)
	if !ok {
		return nil, nil
	}
	over := len(snap.Messages) >= m.cfg.MaxSessionMessages || estimateTokens(snap.Messages) >= m.cfg.MaxSessionTokens
	if !over {
		_ = m.writeState(sessionKey, archiveSessionState{
			LastOverLimitState: false,
		})
		return nil, nil
	}
	result, err := m.archiveSnapshot(sessionKey, snap.Messages, false)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) ArchiveAll(overLimitOnly bool, dryRun bool) ([]ArchiveResult, error) {
	if m == nil || m.sessions == nil {
		return nil, nil
	}
	results := make([]ArchiveResult, 0)
	for _, stat := range m.ListDiscordSessions(false) {
		if overLimitOnly && !stat.OverLimit {
			continue
		}
		snap, ok := m.sessions.Snapshot(stat.SessionKey)
		if !ok {
			continue
		}
		result, err := m.archiveSnapshot(stat.SessionKey, snap.Messages, dryRun)
		if err != nil {
			return results, err
		}
		if result != nil {
			results = append(results, *result)
		}
	}
	return results, nil
}

func (m *Manager) ArchiveSession(sessionKey string, dryRun bool) (*ArchiveResult, error) {
	if m == nil || m.sessions == nil {
		return nil, nil
	}
	snap, ok := m.sessions.Snapshot(sessionKey)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionKey)
	}
	return m.archiveSnapshot(sessionKey, snap.Messages, dryRun)
}

func (m *Manager) Recall(query, sessionKey string, topK, maxChars int) []RecallHit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	if topK <= 0 {
		topK = m.cfg.RecallTopK
		if topK <= 0 {
			topK = 6
		}
	}
	if maxChars <= 0 {
		maxChars = m.cfg.RecallMaxChars
		if maxChars <= 0 {
			maxChars = 3000
		}
	}

	files, err := os.ReadDir(m.archiveSessionsDir())
	if err != nil {
		return nil
	}
	safeKey := sanitizeSessionKey(sessionKey)

	terms := tokenize(query)
	type scored struct {
		hit  RecallHit
		size int
	}
	scoredHits := make([]scored, 0)
	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if safeKey != "" && !strings.Contains(name, safeKey) {
			continue
		}
		path := filepath.Join(m.archiveSessionsDir(), name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(data)
		score := lexicalScore(text, terms)
		if score <= 0 {
			continue
		}
		snippet := summarizeText(text, 420)
		scoredHits = append(scoredHits, scored{
			hit: RecallHit{
				SessionKey: extractSessionKeyFromArchive(text),
				SourcePath: path,
				Score:      score,
				Text:       snippet,
			},
			size: len(snippet),
		})
	}
	sort.Slice(scoredHits, func(i, j int) bool {
		if scoredHits[i].hit.Score == scoredHits[j].hit.Score {
			return scoredHits[i].hit.SourcePath < scoredHits[j].hit.SourcePath
		}
		return scoredHits[i].hit.Score > scoredHits[j].hit.Score
	})

	out := make([]RecallHit, 0, topK)
	remaining := maxChars
	for _, candidate := range scoredHits {
		if len(out) >= topK {
			break
		}
		if candidate.size > remaining {
			continue
		}
		out = append(out, candidate.hit)
		remaining -= candidate.size
	}
	return out
}

func (m *Manager) archiveSnapshot(sessionKey string, allMessages []providers.Message, dryRun bool) (*ArchiveResult, error) {
	if !strings.HasPrefix(sessionKey, "discord:") {
		return nil, nil
	}
	if len(allMessages) == 0 {
		return nil, nil
	}

	tokensBefore := estimateTokens(allMessages)
	keepStart := calculateKeepStart(allMessages, m.cfg.KeepUserPairs, m.cfg.MinTailMessages)
	if keepStart <= 0 {
		return nil, nil
	}
	if keepStart > len(allMessages) {
		keepStart = len(allMessages)
	}

	archiveSlice := allMessages[:keepStart]
	keptSlice := allMessages[keepStart:]
	archiveForMarkdown := selectArchiveMessages(archiveSlice)
	if len(archiveForMarkdown) == 0 {
		return nil, nil
	}

	result := &ArchiveResult{
		SessionKey:       sessionKey,
		ArchivedMessages: len(archiveForMarkdown),
		KeptMessages:     len(keptSlice),
		TokensBefore:     tokensBefore,
		TokensAfter:      estimateTokens(keptSlice),
		OverLimit:        tokensBefore >= m.cfg.MaxSessionTokens || len(allMessages) >= m.cfg.MaxSessionMessages,
		DryRun:           dryRun,
	}

	archivePath := m.archivePathFor(sessionKey)
	result.ArchivePath = archivePath
	if dryRun {
		return result, nil
	}

	if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
		return nil, err
	}
	md := buildMarkdown(sessionKey, archiveForMarkdown)
	if err := os.WriteFile(archivePath, []byte(md), 0644); err != nil {
		return nil, err
	}

	m.sessions.ReplaceHistory(sessionKey, keptSlice)
	if err := m.sessions.Save(sessionKey); err != nil {
		return nil, err
	}

	if err := m.writeState(sessionKey, archiveSessionState{
		LastArchivedAt:     time.Now().UTC().Format(time.RFC3339),
		LastArchivePath:    archivePath,
		ArchivedMessages:   result.ArchivedMessages,
		KeptMessages:       result.KeptMessages,
		TokensBefore:       result.TokensBefore,
		TokensAfter:        result.TokensAfter,
		LastOverLimitState: result.OverLimit,
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *Manager) writeState(sessionKey string, stateEntry archiveSessionState) error {
	path := filepath.Join(m.archiveBaseDir(), ".archive-state.json")
	current := archiveState{Sessions: map[string]archiveSessionState{}}

	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &current)
		if current.Sessions == nil {
			current.Sessions = map[string]archiveSessionState{}
		}
	}
	current.Sessions[sessionKey] = stateEntry

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (m *Manager) archiveBaseDir() string {
	return filepath.Join(m.workspace, "memory", "archive", "discord")
}

func (m *Manager) archiveSessionsDir() string {
	return filepath.Join(m.archiveBaseDir(), "sessions")
}

func (m *Manager) archivePathFor(sessionKey string) string {
	now := time.Now().UTC()
	return filepath.Join(
		m.archiveSessionsDir(),
		fmt.Sprintf("%s-discord-session-%s-%d.md", now.Format("2006-01-02"), sanitizeSessionKey(sessionKey), now.UnixNano()),
	)
}

func calculateKeepStart(messages []providers.Message, keepUserPairs, minTailMessages int) int {
	if len(messages) == 0 {
		return 0
	}
	if keepUserPairs <= 0 {
		keepUserPairs = 12
	}
	if minTailMessages <= 0 {
		minTailMessages = 4
	}

	keepStart := 0
	userCount := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			userCount++
			if userCount >= keepUserPairs {
				keepStart = i
				break
			}
		}
	}
	if userCount < keepUserPairs {
		keepStart = 0
	}

	minKeepStart := len(messages) - minTailMessages
	if minKeepStart < 0 {
		minKeepStart = 0
	}
	if keepStart > minKeepStart {
		keepStart = minKeepStart
	}
	return keepStart
}

func selectArchiveMessages(messages []providers.Message) []providers.Message {
	out := make([]providers.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		out = append(out, providers.Message{
			Role:    msg.Role,
			Content: truncate(content, maxArchiveMessageChars),
		})
	}
	return out
}

func buildMarkdown(sessionKey string, messages []providers.Message) string {
	var b strings.Builder
	now := time.Now().UTC().Format(time.RFC3339)
	b.WriteString("# Discord Session Archive\n\n")
	b.WriteString("- **Session Key**: " + sessionKey + "\n")
	b.WriteString("- **Archived At**: " + now + "\n")
	b.WriteString("- **Messages**: " + fmt.Sprintf("%d", len(messages)) + "\n\n")
	b.WriteString("---\n\n")
	for i, msg := range messages {
		label := "Assistant"
		if msg.Role == "user" {
			label = "User"
		}
		b.WriteString(fmt.Sprintf("### %d. %s\n\n", i+1, label))
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

func estimateTokens(messages []providers.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4
	}
	return total
}

var nonSafeChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeSessionKey(sessionKey string) string {
	key := strings.TrimSpace(sessionKey)
	if key == "" {
		return "unknown"
	}
	key = strings.ReplaceAll(key, ":", "_")
	key = strings.ReplaceAll(key, "@", "_")
	key = nonSafeChars.ReplaceAllString(key, "_")
	key = strings.Trim(key, "_")
	if key == "" {
		return "unknown"
	}
	return key
}

func tokenize(text string) []string {
	raw := strings.Fields(strings.ToLower(text))
	out := make([]string, 0, len(raw))
	seen := map[string]struct{}{}
	for _, token := range raw {
		token = strings.Trim(token, ".,!?;:\"'()[]{}<>")
		if len(token) < 2 {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func lexicalScore(text string, terms []string) int {
	lower := strings.ToLower(text)
	score := 0
	for _, term := range terms {
		score += strings.Count(lower, term)
	}
	return score
}

func summarizeText(text string, max int) string {
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	return truncate(text, max)
}

func extractSessionKeyFromArchive(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- **Session Key**:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- **Session Key**:"))
		}
	}
	return ""
}

func truncate(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "... [truncated]"
}
