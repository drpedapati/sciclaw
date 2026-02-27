package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/constants"
)

const (
	EventRouteMatch       = "route_match"
	EventRouteUnmapped    = "route_unmapped"
	EventRouteDeny        = "route_deny"
	EventRouteInvalid     = "route_invalid"
	EventRouteMentionSkip = "route_mention_skip"
)

type Decision struct {
	Event        string
	Allowed      bool
	Channel      string
	ChatID       string
	SenderID     string
	Workspace    string
	SessionKey   string
	Reason       string
	MappingLabel string
}

type Resolver struct {
	enabled          bool
	unmappedBehavior string
	defaultWorkspace string
	mappings         map[string]config.RoutingMapping
}

func NewResolver(cfg *config.Config) (*Resolver, error) {
	if cfg == nil {
		return nil, fmt.Errorf("routing config is nil")
	}
	if err := config.ValidateRoutingConfig(cfg.Routing); err != nil {
		return nil, err
	}

	r := &Resolver{
		enabled:          cfg.Routing.Enabled,
		unmappedBehavior: strings.TrimSpace(cfg.Routing.UnmappedBehavior),
		defaultWorkspace: cfg.WorkspacePath(),
		mappings:         make(map[string]config.RoutingMapping, len(cfg.Routing.Mappings)),
	}
	if r.unmappedBehavior == "" {
		r.unmappedBehavior = config.RoutingUnmappedBehaviorDefault
	}
	for _, m := range cfg.Routing.Mappings {
		r.mappings[mappingKey(m.Channel, m.ChatID)] = m
	}
	return r, nil
}

func (r *Resolver) Resolve(msg bus.InboundMessage) Decision {
	channel := msg.Channel
	chatID := msg.ChatID
	enforceSender := true

	// Internal/system messages should never be blocked by user mapping ACLs.
	if constants.IsInternalChannel(msg.Channel) {
		enforceSender = false
		originChannel, originChatID := parseOrigin(msg)
		if originChannel != "" && originChatID != "" {
			channel = originChannel
			chatID = originChatID
		} else {
			return r.allowDefault(msg, channel, chatID, "internal channel")
		}
	}

	if !r.enabled {
		return r.allowDefault(msg, channel, chatID, "routing disabled")
	}

	mapping, ok := r.mappings[mappingKey(channel, chatID)]
	if !ok {
		if r.unmappedBehavior == config.RoutingUnmappedBehaviorDefault {
			d := r.allowDefault(msg, channel, chatID, "unmapped default fallback")
			d.Event = EventRouteUnmapped
			return d
		}
		return Decision{
			Event:    EventRouteUnmapped,
			Allowed:  false,
			Channel:  channel,
			ChatID:   chatID,
			SenderID: msg.SenderID,
			Reason:   "no routing mapping for channel/chat",
		}
	}

	if enforceSender && !isSenderAllowed(msg.SenderID, mapping.AllowedSenders) {
		return Decision{
			Event:        EventRouteDeny,
			Allowed:      false,
			Channel:      channel,
			ChatID:       chatID,
			SenderID:     msg.SenderID,
			Reason:       "sender is not allowlisted for this mapping",
			MappingLabel: mapping.Label,
		}
	}

	if enforceSender && mapping.MentionRequired() && !isMentionOrDM(msg.Metadata) {
		return Decision{
			Event:        EventRouteMentionSkip,
			Allowed:      false,
			Channel:      channel,
			ChatID:       chatID,
			SenderID:     msg.SenderID,
			Reason:       "mention required",
			MappingLabel: mapping.Label,
		}
	}

	if err := ensureReadableWorkspace(mapping.Workspace); err != nil {
		return Decision{
			Event:        EventRouteInvalid,
			Allowed:      false,
			Channel:      channel,
			ChatID:       chatID,
			SenderID:     msg.SenderID,
			Workspace:    mapping.Workspace,
			Reason:       fmt.Sprintf("workspace invalid: %v", err),
			MappingLabel: mapping.Label,
		}
	}

	return Decision{
		Event:        EventRouteMatch,
		Allowed:      true,
		Channel:      channel,
		ChatID:       chatID,
		SenderID:     msg.SenderID,
		Workspace:    mapping.Workspace,
		SessionKey:   namespacedSessionKey(channel, chatID, mapping.Workspace),
		Reason:       "exact mapping match",
		MappingLabel: mapping.Label,
	}
}

func (r *Resolver) allowDefault(msg bus.InboundMessage, channel, chatID, reason string) Decision {
	return Decision{
		Event:      EventRouteMatch,
		Allowed:    true,
		Channel:    channel,
		ChatID:     chatID,
		SenderID:   msg.SenderID,
		Workspace:  r.defaultWorkspace,
		SessionKey: namespacedSessionKey(channel, chatID, r.defaultWorkspace),
		Reason:     reason,
	}
}

func mappingKey(channel, chatID string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + "\x00" + strings.TrimSpace(chatID)
}

func parseOrigin(msg bus.InboundMessage) (string, string) {
	if msg.Channel != "system" {
		return "", ""
	}
	parts := strings.SplitN(msg.ChatID, ":", 2)
	if len(parts) != 2 {
		return "", ""
	}
	channel := strings.TrimSpace(parts[0])
	chatID := strings.TrimSpace(parts[1])
	if channel == "" || chatID == "" {
		return "", ""
	}
	return channel, chatID
}

func namespacedSessionKey(channel, chatID, workspace string) string {
	base := fmt.Sprintf("%s:%s", strings.TrimSpace(channel), strings.TrimSpace(chatID))
	if workspace == "" {
		return base
	}
	hash := sha256.Sum256([]byte(filepath.Clean(workspace)))
	// 12 hex chars keeps it compact while avoiding collisions in practice.
	return fmt.Sprintf("%s@%s", base, hex.EncodeToString(hash[:6]))
}

func ensureReadableWorkspace(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	// Do not call os.ReadDir() on every inbound message.
	// On macOS cloud-backed folders (e.g. Dropbox/iCloud), ReadDir can stall for
	// minutes and block the single routing dispatcher goroutine, which prevents
	// otherwise valid Discord mentions from becoming active tasks.
	return nil
}

func isMentionOrDM(metadata map[string]string) bool {
	if metadata["is_dm"] == "true" {
		return true
	}
	return metadata["is_mention"] == "true"
}

func isSenderAllowed(senderID string, allowlist []string) bool {
	if len(allowlist) == 0 {
		return false
	}

	idPart := senderID
	userPart := ""
	if idx := strings.Index(senderID, "|"); idx > 0 {
		idPart = senderID[:idx]
		userPart = senderID[idx+1:]
	}

	for _, allowed := range allowlist {
		trimmed := strings.TrimSpace(strings.TrimPrefix(allowed, "@"))
		if trimmed == "" {
			continue
		}
		allowedID := trimmed
		allowedUser := ""
		if idx := strings.Index(trimmed, "|"); idx > 0 {
			allowedID = trimmed[:idx]
			allowedUser = trimmed[idx+1:]
		}

		if senderID == allowed ||
			senderID == trimmed ||
			idPart == allowed ||
			idPart == trimmed ||
			idPart == allowedID ||
			(userPart != "" && (userPart == allowed || userPart == trimmed || userPart == allowedUser)) {
			return true
		}
	}
	return false
}
