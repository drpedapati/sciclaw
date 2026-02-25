package channels

import "strings"

// NormalizeDiscordBotToken trims common copy/paste wrappers and removes an
// optional leading "Bot " prefix so callers can safely pass the raw token.
func NormalizeDiscordBotToken(token string) string {
	t := strings.TrimSpace(token)
	t = strings.Trim(t, "\"'")
	t = strings.TrimSpace(t)

	parts := strings.Fields(t)
	if len(parts) >= 2 && strings.EqualFold(parts[0], "bot") {
		return strings.Join(parts[1:], "")
	}
	return t
}
