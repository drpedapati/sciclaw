package vmtui

import (
	"fmt"
	"strconv"
	"strings"
)

// ApprovedUser represents a parsed allowlist entry.
type ApprovedUser struct {
	Raw      string // original config entry
	UserID   string // numeric part (may be empty)
	Username string // display name (may be empty)
}

// ParseApprovedUser parses a FlexibleStringSlice entry like "123", "123|username", or "username".
func ParseApprovedUser(raw string) ApprovedUser {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ApprovedUser{Raw: raw}
	}
	if idx := strings.Index(raw, "|"); idx > 0 {
		return ApprovedUser{
			Raw:      raw,
			UserID:   raw[:idx],
			Username: raw[idx+1:],
		}
	}
	if _, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return ApprovedUser{Raw: raw, UserID: raw}
	}
	return ApprovedUser{Raw: raw, Username: raw}
}

// DisplayName returns a human-friendly label.
func (u ApprovedUser) DisplayName() string {
	if u.Username != "" {
		return u.Username
	}
	if u.UserID != "" {
		return "(no name)"
	}
	return "(empty)"
}

// DisplayID returns the user ID or a placeholder.
func (u ApprovedUser) DisplayID() string {
	if u.UserID != "" {
		return u.UserID
	}
	return "(no ID)"
}

// FormatEntry creates the compound "ID|username" string for saving.
func FormatEntry(id, username string) string {
	id = strings.TrimSpace(id)
	username = strings.TrimSpace(username)
	if id != "" && username != "" {
		return fmt.Sprintf("%s|%s", id, username)
	}
	if id != "" {
		return id
	}
	return username
}
