package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Valid answer themes.
const (
	ThemeClear  = "clear"
	ThemeFormal = "formal"
	ThemeBrief  = "brief"
)

// OperatorID is the profile key used by the TUI and web UI operator.
const OperatorID = "_operator"

// UserProfile stores per-user preferences.
type UserProfile struct {
	AnswerTheme string `json:"answer_theme"`
	DisplayName string `json:"display_name,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// Store manages per-user profile files on disk.
type Store struct {
	dir string

	// OnProfileUpdated is invoked after SetAnswerTheme successfully persists a
	// profile to disk. It is wired by main() at startup to fan the change out
	// to the addon hook dispatcher as a "profile_updated" event. Nil is a
	// no-op, so tests and cold-start code paths that do not care about the
	// addon system can leave it unset.
	//
	// The callback runs synchronously on the caller's goroutine, so
	// implementations must be cheap or self-dispatch. The main wiring uses the
	// addon dispatcher's bounded-context helper, which is non-blocking in
	// practice.
	OnProfileUpdated func(senderID string, p *UserProfile)
}

// NewStore creates a profile store at the given directory.
// The directory is created lazily on first write.
func NewStore(profileDir string) *Store {
	return &Store{dir: profileDir}
}

// Load reads a user profile from disk. Returns a zero-value profile (not an
// error) if the file does not exist.
func (s *Store) Load(senderID string) (*UserProfile, error) {
	path := s.path(senderID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &UserProfile{AnswerTheme: ThemeClear}, nil
		}
		return nil, fmt.Errorf("reading profile %s: %w", senderID, err)
	}
	var p UserProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile %s: %w", senderID, err)
	}
	if !IsValidTheme(p.AnswerTheme) {
		p.AnswerTheme = ThemeClear
	}
	return &p, nil
}

// Save writes a user profile to disk atomically (temp file + rename).
func (s *Store) Save(senderID string, p *UserProfile) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating profile directory: %w", err)
	}
	p.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling profile: %w", err)
	}
	data = append(data, '\n')

	target := s.path(senderID)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing temp profile: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming profile: %w", err)
	}
	return nil
}

// AnswerTheme returns the user's answer theme, defaulting to "clear".
func (s *Store) AnswerTheme(senderID string) string {
	p, err := s.Load(senderID)
	if err != nil || p == nil {
		return ThemeClear
	}
	return p.AnswerTheme
}

// SetAnswerTheme validates and persists the user's answer theme.
//
// On a successful save, Store.OnProfileUpdated is invoked if set. The callback
// runs synchronously and its errors are swallowed — the contract is that
// hooks are fire-and-forget and must not block profile writes.
func (s *Store) SetAnswerTheme(senderID, displayName, theme string) error {
	theme = strings.TrimSpace(strings.ToLower(theme))
	if !IsValidTheme(theme) {
		return fmt.Errorf("invalid theme %q: must be clear, formal, or brief", theme)
	}
	p, err := s.Load(senderID)
	if err != nil {
		return fmt.Errorf("loading profile for %s: %w", senderID, err)
	}
	p.AnswerTheme = theme
	if displayName != "" {
		p.DisplayName = displayName
	}
	if err := s.Save(senderID, p); err != nil {
		return err
	}
	if s.OnProfileUpdated != nil {
		s.OnProfileUpdated(senderID, p)
	}
	return nil
}

// List returns every senderID that has a persisted profile, sorted.
// Returns an empty slice (not an error) if the directory is missing —
// fresh installs have no profiles until /theme is invoked at least once.
//
// The directory layout maps each senderID to a single .json file via
// path(), which sluggifies path separators to "_". This function reverses
// that mapping by stripping the .json extension; sluggified IDs (those
// that originally contained "/" or "\") still come back with "_"
// substituted. That's safe because every code path writes via Save()
// which uses the same sluggifier.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing profile directory %s: %w", s.dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		// Skip atomic-write temp files that survived a crash.
		if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".tmp.json") {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(out)
	return out, nil
}

// IsValidTheme returns true if the theme name is recognized.
func IsValidTheme(theme string) bool {
	switch theme {
	case ThemeClear, ThemeFormal, ThemeBrief:
		return true
	}
	return false
}

// ThemeLabel returns a capitalized display label for the theme.
func ThemeLabel(theme string) string {
	switch theme {
	case ThemeFormal:
		return "Formal"
	case ThemeBrief:
		return "Brief"
	default:
		return "Clear"
	}
}

func (s *Store) path(senderID string) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == '\x00' {
			return '_'
		}
		return r
	}, senderID)
	return filepath.Join(s.dir, safe+".json")
}
