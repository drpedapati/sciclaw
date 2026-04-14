package main

// addon_users.go exposes a read-only list of "registered users" — the union
// of identities sciclaw knows about — for addon UIs to render dropdowns
// instead of freeform name inputs. This makes it possible for the same
// "alice" in chat, in /theme, and in webtop/jupyter to be the same person.
//
// Endpoint: GET /api/core/users
//
// Response body shape:
//
//	[
//	  {
//	    "sender_id":    "discord:214611...",
//	    "display_name": "Alice",
//	    "slug":         "alice",
//	    "answer_theme": "clear",
//	    "sources":      ["profile", "routing:#als-rct"]
//	  },
//	  ...
//	]
//
// Identity sources unioned:
//   1. pkg/profile/ — every senderID with a persisted profile (set by /theme).
//   2. cfg.Routing.Mappings[*].AllowedSenders — every sender the operator has
//      explicitly allowed in any routing rule, except the literal "*" wildcard.
//
// Slug derivation: sluggify display_name if present, otherwise the part of
// sender_id after the platform prefix. Slugs are validated against
// [a-z0-9_-]{1,64}, the same charset addons use for container names.
// Collisions are resolved by appending "-2", "-3", etc.

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/profile"
)

// coreUserEntry is the JSON shape one user takes in the response.
type coreUserEntry struct {
	SenderID    string   `json:"sender_id"`
	DisplayName string   `json:"display_name,omitempty"`
	Slug        string   `json:"slug"`
	AnswerTheme string   `json:"answer_theme,omitempty"`
	Sources     []string `json:"sources"`
}

// handleCoreUsers serves GET /api/core/users. Read-only, no auth (same
// posture as the rest of the sciclaw web admin — 127.0.0.1-only).
func (s *webServer) handleCoreUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonErr(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cfg, err := loadConfig()
	if err != nil {
		jsonErr(w, "loading config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	home := sciclawHomeDir()
	store := profile.NewStore(home + "/profiles")
	entries := buildCoreUsers(store, cfg)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

// buildCoreUsers is split out so unit tests can drive it without the full
// HTTP/config plumbing. It unions profile-based and routing-based identity
// sources, derives slugs, and resolves collisions.
func buildCoreUsers(store *profile.Store, cfg *config.Config) []coreUserEntry {
	type accum struct {
		entry   coreUserEntry
		sources map[string]bool
	}
	users := map[string]*accum{}

	getOrCreate := func(senderID string) *accum {
		if a, ok := users[senderID]; ok {
			return a
		}
		a := &accum{
			entry:   coreUserEntry{SenderID: senderID},
			sources: map[string]bool{},
		}
		users[senderID] = a
		return a
	}

	// 1. Profile-store entries.
	if store != nil {
		ids, _ := store.List()
		for _, id := range ids {
			a := getOrCreate(id)
			a.sources["profile"] = true
			if p, err := store.Load(id); err == nil && p != nil {
				if p.DisplayName != "" {
					a.entry.DisplayName = p.DisplayName
				}
				if p.AnswerTheme != "" {
					a.entry.AnswerTheme = p.AnswerTheme
				}
			}
		}
	}

	// 2. Routing rule allowed_senders. Skip the "*" wildcard since it
	//    doesn't name a specific identity.
	if cfg != nil {
		for _, m := range cfg.Routing.Mappings {
			for _, sender := range m.AllowedSenders {
				sender = strings.TrimSpace(sender)
				if sender == "" || sender == "*" {
					continue
				}
				a := getOrCreate(sender)
				a.sources["routing:"+m.Channel] = true
			}
		}
	}

	// Materialize, sluggify, dedupe.
	out := make([]coreUserEntry, 0, len(users))
	for _, a := range users {
		a.entry.Slug = deriveSlug(a.entry.DisplayName, a.entry.SenderID)
		for src := range a.sources {
			a.entry.Sources = append(a.entry.Sources, src)
		}
		sort.Strings(a.entry.Sources)
		out = append(out, a.entry)
	}
	// Stable sort: by display_name, falling back to sender_id.
	sort.Slice(out, func(i, j int) bool {
		if out[i].DisplayName != out[j].DisplayName {
			return out[i].DisplayName < out[j].DisplayName
		}
		return out[i].SenderID < out[j].SenderID
	})
	// Resolve slug collisions by appending "-2", "-3", etc.
	seen := map[string]int{}
	for i := range out {
		base := out[i].Slug
		if seen[base] == 0 {
			seen[base] = 1
			continue
		}
		seen[base]++
		// New unique slug: base + "-" + count.
		var unique string
		for n := seen[base]; ; n++ {
			candidate := base + "-" + itoa(n)
			if seen[candidate] == 0 {
				unique = candidate
				seen[candidate] = 1
				seen[base] = n
				break
			}
		}
		out[i].Slug = unique
	}
	return out
}

// deriveSlug produces an addon-name-safe slug from a display name and
// sender_id, in that order of preference. The output charset matches the
// addon container name validator: [a-z0-9_-], 1..64 chars, no leading dash.
//
// "Alice Doe"           -> "alice-doe"
// "discord:214611..."   -> "214611"     (strips platform prefix)
// "@al!ce"              -> "al-ce"      (collapses runs of bad chars to one -)
// "!!!"                 -> falls through to sender_id ("y" for "x:y")
// ""                    -> "user"       (sentinel)
func deriveSlug(displayName, senderID string) string {
	if slug := slugify(displayName); slug != "" {
		return slug
	}
	// Strip platform prefix like "discord:" so the slug is the bare ID.
	src := senderID
	if i := strings.IndexByte(src, ':'); i >= 0 && i < len(src)-1 {
		src = src[i+1:]
	}
	if slug := slugify(src); slug != "" {
		return slug
	}
	return "user"
}

// slugify lowercases, replaces every run of non-[a-z0-9_] with a single
// "-", trims leading/trailing dashes, and caps at 64 chars. Returns ""
// if the input has no convertible characters.
func slugify(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	var b strings.Builder
	lastDash := false
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

// itoa is a small inline replacement for strconv.Itoa to avoid the import
// in this single use site.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
