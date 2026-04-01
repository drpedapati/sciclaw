package channels

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
	"github.com/sipeed/picoclaw/pkg/voice"
)

const (
	transcriptionTimeout = 30 * time.Second
	sendTimeout          = 10 * time.Second
	typingInterval       = 8 * time.Second
	maxTypingDuration    = 3 * time.Minute // safety net: auto-cancel typing if stopTyping is never called
	discordMaxRunes      = 2000
	discordMaxFileBytes  = 25 * 1024 * 1024
	discordInboundDedupTTL = 2 * time.Minute
)

type typingState struct {
	pending int
	cancel  context.CancelFunc
}

type SlashSkillChoice struct {
	Name        string
	Description string
}

type inboundMessageFingerprint struct {
	fingerprint string
	seenAt      time.Time
}

type DiscordChannel struct {
	*BaseChannel
	session     *discordgo.Session
	config      config.DiscordConfig
	transcriber *voice.GroqTranscriber
	ctx         context.Context
	botUserID  string
	botRoleMu  sync.Mutex
	botRoleIDs map[string]bool // managed role IDs discovered lazily per guild

	typingMu              sync.Mutex
	typing                map[string]*typingState
	typingEvery           time.Duration
	sendMessageFn         func(channelID string, msg bus.OutboundMessage) error
	sendProgressMessageFn func(channelID string, msg bus.OutboundMessage) (string, error)
	sendFileFn            func(channelID, content string, attachment bus.OutboundAttachment) error
	sendTypingFn          func(channelID string) error
	editMessageFn         func(channelID, messageID string, msg bus.OutboundMessage) error
	interactionRespondFn  func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error
	interactionEditFn     func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error
	listCommandsFn        func(appID, guildID string) ([]*discordgo.ApplicationCommand, error)
	createCommandFn       func(appID, guildID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error)
	editCommandFn         func(appID, guildID, cmdID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error)
	skillCatalogFn        func(channelID, guildID, userID string) ([]SlashSkillChoice, error)
	themeSetFn            func(senderID, displayName, theme string) error
	inboundMu             sync.Mutex
	recentInbound         map[string]inboundMessageFingerprint
}

// SetThemeHandler sets the callback for the /theme slash command.
func (c *DiscordChannel) SetThemeHandler(fn func(senderID, displayName, theme string) error) {
	c.themeSetFn = fn
}

func NewDiscordChannel(cfg config.DiscordConfig, messageBus *bus.MessageBus) (*DiscordChannel, error) {
	token := NormalizeDiscordBotToken(cfg.Token)
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}
	// Explicitly request message-content events for gateway delivery in guild channels.
	// DiscordGo defaults to IntentsAllWithoutPrivileged, which excludes MessageContent.
	session.Identify.Intents = discordgo.IntentsAllWithoutPrivileged | discordgo.IntentsMessageContent

	base := NewBaseChannel("discord", cfg, messageBus, cfg.AllowFrom)

	return &DiscordChannel{
		BaseChannel: base,
		session:     session,
		config:      cfg,
		transcriber: nil,
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: typingInterval,
		sendMessageFn: func(channelID string, msg bus.OutboundMessage) error {
			_, err := session.ChannelMessageSendComplex(channelID, discordMessageSend(msg))
			return err
		},
		sendProgressMessageFn: func(channelID string, msg bus.OutboundMessage) (string, error) {
			sent, err := session.ChannelMessageSendComplex(channelID, discordMessageSend(msg))
			if err != nil {
				return "", err
			}
			return sent.ID, nil
		},
		sendFileFn: func(channelID, content string, attachment bus.OutboundAttachment) error {
			return sendDiscordAttachment(session, channelID, content, attachment)
		},
		sendTypingFn: func(channelID string) error {
			return session.ChannelTyping(channelID)
		},
		editMessageFn: func(channelID, messageID string, msg bus.OutboundMessage) error {
			_, err := session.ChannelMessageEditComplex(discordMessageEdit(channelID, messageID, msg))
			return err
		},
		interactionRespondFn: func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
			return session.InteractionRespond(interaction, resp)
		},
		interactionEditFn: func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
			_, err := session.InteractionResponseEdit(interaction, edit)
			return err
		},
		listCommandsFn: func(appID, guildID string) ([]*discordgo.ApplicationCommand, error) {
			return session.ApplicationCommands(appID, guildID)
		},
		createCommandFn: func(appID, guildID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
			return session.ApplicationCommandCreate(appID, guildID, cmd)
		},
		editCommandFn: func(appID, guildID, cmdID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
			return session.ApplicationCommandEdit(appID, guildID, cmdID, cmd)
		},
		recentInbound: make(map[string]inboundMessageFingerprint),
	}, nil
}

// isBotRoleMention checks whether roleID is the bot's managed role for the
// given guild.  Results are cached so the guild roles are only inspected once.
func (c *DiscordChannel) isBotRoleMention(guildID, roleID string) bool {
	c.botRoleMu.Lock()
	defer c.botRoleMu.Unlock()

	// Fast path: already resolved this role.
	if c.botRoleIDs[roleID] {
		return true
	}

	// Have we already scanned this guild?
	sentinelKey := "guild:" + guildID
	if c.botRoleIDs[sentinelKey] {
		return false // scanned before, roleID wasn't a match
	}

	// Lazy discovery: scan guild roles now.
	if c.botRoleIDs == nil {
		c.botRoleIDs = make(map[string]bool)
	}
	c.botRoleIDs[sentinelKey] = true // mark guild as scanned

	if c.session == nil || c.session.State == nil || c.session.State.User == nil {
		return false
	}
	botName := strings.ToLower(strings.TrimSpace(c.session.State.User.Username))
	if botName == "" {
		return false
	}
	guild, err := c.session.State.Guild(guildID)
	if err != nil {
		return false
	}
	for _, r := range guild.Roles {
		if r.Managed && strings.ToLower(r.Name) == botName {
			c.botRoleIDs[r.ID] = true
		}
	}
	return c.botRoleIDs[roleID]
}

func (c *DiscordChannel) shouldDropDuplicateInbound(messageID, content string, media []string, hasDirectMention, replyToBot bool) bool {
	if strings.TrimSpace(messageID) == "" {
		return false
	}

	parts := []string{
		content,
		strings.Join(media, "\n"),
		fmt.Sprintf("direct=%t", hasDirectMention),
		fmt.Sprintf("reply=%t", replyToBot),
	}
	fingerprint := strings.Join(parts, "\x1f")
	now := time.Now()

	c.inboundMu.Lock()
	defer c.inboundMu.Unlock()

	if c.recentInbound == nil {
		c.recentInbound = make(map[string]inboundMessageFingerprint)
	}

	for id, seen := range c.recentInbound {
		if now.Sub(seen.seenAt) > discordInboundDedupTTL {
			delete(c.recentInbound, id)
		}
	}

	if seen, ok := c.recentInbound[messageID]; ok {
		if seen.fingerprint == fingerprint && now.Sub(seen.seenAt) <= discordInboundDedupTTL {
			return true
		}
	}

	c.recentInbound[messageID] = inboundMessageFingerprint{
		fingerprint: fingerprint,
		seenAt:      now,
	}
	return false
}

func (c *DiscordChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
}

func (c *DiscordChannel) SetSlashSkillCatalogCallback(cb func(channelID, guildID, userID string) ([]SlashSkillChoice, error)) {
	c.skillCatalogFn = cb
}

// GetChannelMessages fetches recent messages from a Discord channel via the REST API.
// Messages are returned in reverse chronological order (newest first) by the API;
// the caller is responsible for reversing if chronological order is needed.
func (c *DiscordChannel) GetChannelMessages(channelID string, limit int, beforeID string) ([]*discordgo.Message, error) {
	if c.session == nil {
		return nil, fmt.Errorf("discord session not available")
	}
	return c.session.ChannelMessages(channelID, limit, beforeID, "", "")
}

func (c *DiscordChannel) getContext() context.Context {
	if c.ctx == nil {
		return context.Background()
	}
	return c.ctx
}

func (c *DiscordChannel) Start(ctx context.Context) error {
	logger.InfoC("discord", "Starting Discord bot")

	c.ctx = ctx
	c.session.AddHandler(c.handleMessage)
	c.session.AddHandler(c.handleMessageUpdate)
	c.session.AddHandler(c.handleInteractionCreate)

	if err := c.session.Open(); err != nil {
		// Graceful fallback for deployments where Message Content intent is not granted.
		// Keep bot online instead of hard-failing startup.
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "4014") || strings.Contains(errText, "disallowed intent") {
			fallback := discordgo.IntentsAllWithoutPrivileged
			if c.session.Identify.Intents != fallback {
				logger.WarnCF("discord", "Message content intent not granted; retrying without it", map[string]any{
					"error": err.Error(),
				})
				c.session.Identify.Intents = fallback
				if retryErr := c.session.Open(); retryErr != nil {
					return fmt.Errorf("failed to open discord session (fallback without message content intent): %w", retryErr)
				}
			} else {
				return fmt.Errorf("failed to open discord session: %w", err)
			}
		} else {
			return fmt.Errorf("failed to open discord session: %w", err)
		}
	}

	c.setRunning(true)

	botUser, err := c.session.User("@me")
	if err != nil {
		return fmt.Errorf("failed to get bot user: %w", err)
	}
	c.botUserID = botUser.ID
	logger.InfoCF("discord", "Discord bot connected", map[string]any{
		"username": botUser.Username,
		"user_id":  botUser.ID,
	})
	if err := c.ensureSlashCommands(); err != nil {
		logger.WarnCF("discord", "Failed to sync Discord slash commands", map[string]any{
			"error": err.Error(),
		})
	}

	return nil
}

func (c *DiscordChannel) Stop(ctx context.Context) error {
	logger.InfoC("discord", "Stopping Discord bot")
	c.setRunning(false)
	c.stopAllTyping()

	if c.session == nil {
		return nil
	}

	if err := c.session.Close(); err != nil {
		return fmt.Errorf("failed to close discord session: %w", err)
	}

	return nil
}

func (c *DiscordChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	channelID := msg.ChatID

	// Always stop typing first, even if we bail out below.
	if channelID != "" {
		c.stopTyping(channelID)
	}

	if !c.IsRunning() {
		return fmt.Errorf("discord bot not running")
	}
	if channelID == "" {
		return fmt.Errorf("channel ID is empty")
	}

	message := msg.Content
	if strings.TrimSpace(message) == "" {
		message = "[empty message]"
	}
	chunks := splitDiscordMessage(message, discordMaxRunes)

	// 使用传入的 ctx 进行超时控制
	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		if len(msg.Attachments) > 0 {
			done <- c.sendMessageWithAttachments(channelID, chunks, msg.Attachments)
			return
		}
		for _, chunk := range chunks {
			if err := c.sendMessage(channelID, bus.OutboundMessage{Channel: msg.Channel, ChatID: msg.ChatID, Content: chunk}); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("failed to send discord message: %w", err)
		}
		return nil
	case <-sendCtx.Done():
		return fmt.Errorf("send message timeout: %w", sendCtx.Err())
	}
}

func (c *DiscordChannel) SendOrEditProgress(ctx context.Context, chatID, messageID string, msg bus.OutboundMessage) (string, error) {
	channelID := strings.TrimSpace(chatID)
	if channelID == "" {
		return "", fmt.Errorf("channel ID is empty")
	}
	if !c.IsRunning() {
		return "", fmt.Errorf("discord bot not running")
	}

	c.stopTyping(channelID)

	if strings.TrimSpace(messageID) == "" {
		id, err := c.sendProgressMessage(channelID, msg)
		if err != nil {
			return "", err
		}
		return id, nil
	}

	if err := c.editProgressMessage(channelID, messageID, msg); err != nil {
		if !shouldReplaceProgressMessage(err) {
			return "", fmt.Errorf("edit progress message: %w", err)
		}
		id, sendErr := c.sendProgressMessage(channelID, msg)
		if sendErr != nil {
			return "", fmt.Errorf("edit progress message: %w (fallback send failed: %v)", err, sendErr)
		}
		return id, nil
	}
	return messageID, nil
}

func shouldReplaceProgressMessage(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var restErr *discordgo.RESTError
	if errors.As(err, &restErr) {
		if restErr.Message != nil && restErr.Message.Code == discordgo.ErrCodeUnknownMessage {
			return true
		}
		if restErr.Response != nil && restErr.Response.StatusCode == 404 {
			return true
		}
	}
	return false
}

func (c *DiscordChannel) sendMessageWithAttachments(channelID string, chunks []string, attachments []bus.OutboundAttachment) error {
	remainingChunks := append([]string(nil), chunks...)

	for i, attachment := range attachments {
		caption := ""
		if i == 0 && len(remainingChunks) > 0 {
			caption = remainingChunks[0]
			remainingChunks = remainingChunks[1:]
		}

		if err := c.sendFile(channelID, caption, attachment); err != nil {
			return err
		}
	}

	for _, chunk := range remainingChunks {
		if err := c.sendMessage(channelID, bus.OutboundMessage{Channel: "discord", ChatID: channelID, Content: chunk}); err != nil {
			return err
		}
	}

	return nil
}

// appendContent 安全地追加内容到现有文本
func appendContent(content, suffix string) string {
	if content == "" {
		return suffix
	}
	return content + "\n" + suffix
}

func buildBTWSlashCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "btw",
		Description: "Ask a quick read-only side question in the current workspace",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "prompt",
				Description: "The side question to ask sciClaw",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
		},
	}
}

func buildSkillSlashCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "skill",
		Description: "Run a task with an explicit skill in the current workspace",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:         "name",
				Description:  "The skill to use",
				Type:         discordgo.ApplicationCommandOptionString,
				Required:     true,
				Autocomplete: true,
			},
			{
				Name:        "prompt",
				Description: "The task to perform with that skill",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
			},
		},
	}
}

func buildThemeSlashCommand() *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:        "theme",
		Description: "Set your answer theme (clear, formal, brief)",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Name:        "style",
				Description: "Answer theme to use",
				Type:        discordgo.ApplicationCommandOptionString,
				Required:    true,
				Choices: []*discordgo.ApplicationCommandOptionChoice{
					{Name: "Clear (default — plain language, dense prose)", Value: "clear"},
					{Name: "Formal (academic, publication-ready)", Value: "formal"},
					{Name: "Brief (3-5 sentences max)", Value: "brief"},
				},
			},
		},
	}
}

func interactionAuthor(i *discordgo.Interaction) *discordgo.User {
	if i == nil {
		return nil
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User
	}
	return i.User
}

func slashOptionString(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, opt := range options {
		if opt == nil || opt.Name != name {
			continue
		}
		return strings.TrimSpace(opt.StringValue())
	}
	return ""
}

func focusedSlashOptionString(options []*discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, opt := range options {
		if opt == nil || opt.Name != name || !opt.Focused {
			continue
		}
		return strings.TrimSpace(opt.StringValue())
	}
	return ""
}

func buildSlashMetadata(i *discordgo.Interaction, commandName string, author *discordgo.User) map[string]string {
	senderName := author.Username
	if author.Discriminator != "" && author.Discriminator != "0" {
		senderName += "#" + author.Discriminator
	}
	return map[string]string{
		"interaction_id":     i.ID,
		"interaction_token":  i.Token,
		"command_name":       commandName,
		"is_slash_command":   "true",
		"user_id":            author.ID,
		"username":           author.Username,
		"display_name":       senderName,
		"guild_id":           strings.TrimSpace(i.GuildID),
		"channel_id":         strings.TrimSpace(i.ChannelID),
		"is_dm":              fmt.Sprintf("%t", strings.TrimSpace(i.GuildID) == ""),
		"is_mention":         "true",
		"has_direct_mention": "true",
		"reply_to_bot":       "false",
	}
}

func rankSkillChoice(choice SlashSkillChoice, query string) int {
	if query == "" {
		return 1
	}
	name := strings.ToLower(strings.TrimSpace(choice.Name))
	desc := strings.ToLower(strings.TrimSpace(choice.Description))
	switch {
	case name == query:
		return 4
	case strings.HasPrefix(name, query):
		return 3
	case strings.Contains(name, query):
		return 2
	case strings.Contains(desc, query):
		return 1
	default:
		return 0
	}
}

func autocompleteSkillChoices(all []SlashSkillChoice, query string) []*discordgo.ApplicationCommandOptionChoice {
	query = strings.ToLower(strings.TrimSpace(query))
	type scored struct {
		choice SlashSkillChoice
		score  int
	}
	scoredChoices := make([]scored, 0, len(all))
	for _, choice := range all {
		score := rankSkillChoice(choice, query)
		if score == 0 {
			continue
		}
		scoredChoices = append(scoredChoices, scored{choice: choice, score: score})
	}
	sort.SliceStable(scoredChoices, func(i, j int) bool {
		if scoredChoices[i].score != scoredChoices[j].score {
			return scoredChoices[i].score > scoredChoices[j].score
		}
		return strings.ToLower(scoredChoices[i].choice.Name) < strings.ToLower(scoredChoices[j].choice.Name)
	})
	if len(scoredChoices) > 25 {
		scoredChoices = scoredChoices[:25]
	}
	choices := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(scoredChoices))
	for _, item := range scoredChoices {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{
			Name:  item.choice.Name,
			Value: item.choice.Name,
		})
	}
	return choices
}

func normalizeSkillName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validateSlashSkill(available []SlashSkillChoice, requested string) (SlashSkillChoice, bool) {
	target := normalizeSkillName(requested)
	for _, choice := range available {
		if normalizeSkillName(choice.Name) == target {
			return choice, true
		}
	}
	return SlashSkillChoice{}, false
}

func skillSlashContent(name, prompt string) string {
	return fmt.Sprintf("Use the skill %q for this task. Read its SKILL.md first and follow it.\n\nTask:\n%s", name, prompt)
}

func (c *DiscordChannel) ensureSlashCommands() error {
	if strings.TrimSpace(c.botUserID) == "" {
		return fmt.Errorf("bot user id is empty")
	}
	if c.listCommandsFn == nil || c.createCommandFn == nil || c.editCommandFn == nil {
		return fmt.Errorf("discord slash command functions are not configured")
	}
	commands, err := c.listCommandsFn(c.botUserID, "")
	if err != nil {
		return err
	}
	existingByName := make(map[string]*discordgo.ApplicationCommand, len(commands))
	for _, existing := range commands {
		if existing == nil {
			continue
		}
		existingByName[existing.Name] = existing
	}
	for _, cmd := range []*discordgo.ApplicationCommand{buildBTWSlashCommand(), buildSkillSlashCommand(), buildThemeSlashCommand()} {
		existing := existingByName[cmd.Name]
		if existing == nil {
			if _, err := c.createCommandFn(c.botUserID, "", cmd); err != nil {
				return err
			}
			continue
		}
		if existing.Description == cmd.Description && reflect.DeepEqual(existing.Options, cmd.Options) {
			continue
		}
		if _, err := c.editCommandFn(c.botUserID, "", existing.ID, cmd); err != nil {
			return err
		}
	}
	return nil
}

func (c *DiscordChannel) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil {
		return
	}
	c.processIncomingMessage(s, m.Message, false)
}

func (c *DiscordChannel) handleInteractionCreate(_ *discordgo.Session, i *discordgo.InteractionCreate) {
	if i == nil || i.Interaction == nil {
		return
	}
	data := i.ApplicationCommandData()
	switch i.Type {
	case discordgo.InteractionApplicationCommandAutocomplete:
		if data.Name != "skill" || c.interactionRespondFn == nil {
			return
		}
		c.handleSkillAutocomplete(i, data)
		return
	case discordgo.InteractionApplicationCommand:
	default:
		return
	}
	if c.interactionRespondFn == nil {
		logger.WarnC("discord", "Interaction responder not configured")
		return
	}
	switch data.Name {
	case "btw":
		c.handleBTWInteraction(i, data)
	case "skill":
		c.handleSkillInteraction(i, data)
	case "theme":
		c.handleThemeInteraction(i, data)
	}
}

func (c *DiscordChannel) respondInteractionMessage(i *discordgo.Interaction, content string) {
	if c.interactionRespondFn == nil {
		return
	}
	_ = c.interactionRespondFn(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (c *DiscordChannel) deferInteraction(i *discordgo.Interaction) error {
	if c.interactionRespondFn == nil {
		return fmt.Errorf("interaction responder not configured")
	}
	return c.interactionRespondFn(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Flags: discordgo.MessageFlagsEphemeral,
		},
	})
}

func (c *DiscordChannel) editDeferredInteractionMessage(i *discordgo.Interaction, content string) {
	if c.interactionEditFn == nil {
		logger.WarnC("discord", "Interaction edit responder not configured")
		return
	}
	content = strings.TrimSpace(content)
	if content == "" {
		content = "Request could not be completed."
	}
	if err := c.interactionEditFn(i, &discordgo.WebhookEdit{
		Content: &content,
	}); err != nil {
		logger.WarnCF("discord", "Failed to edit deferred interaction response", map[string]any{
			"error": err.Error(),
		})
	}
}

func (c *DiscordChannel) publishSlashTask(i *discordgo.InteractionCreate, author *discordgo.User, content string, metadata map[string]string, startedMessage string) {
	c.HandleMessage(author.ID, i.ChannelID, content, nil, metadata)
	c.editDeferredInteractionMessage(i.Interaction, startedMessage)
}

func (c *DiscordChannel) handleBTWInteraction(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	prompt := slashOptionString(data.Options, "prompt")
	if prompt == "" {
		c.respondInteractionMessage(i.Interaction, "The `/btw` prompt is required.")
		return
	}
	author := interactionAuthor(i.Interaction)
	if author == nil {
		return
	}
	if !c.IsAllowed(author.ID) {
		c.respondInteractionMessage(i.Interaction, "You are not allowed to use this sciClaw bot.")
		return
	}
	if err := c.deferInteraction(i.Interaction); err != nil {
		logger.WarnCF("discord", "Failed to defer /btw interaction", map[string]any{
			"error": err.Error(),
		})
		return
	}
	c.publishSlashTask(i, author, "/btw "+prompt, buildSlashMetadata(i.Interaction, data.Name, author), "Started. I’ll reply in the channel below.")
}

func (c *DiscordChannel) handleSkillAutocomplete(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	author := interactionAuthor(i.Interaction)
	if author == nil || c.interactionRespondFn == nil {
		return
	}
	query := focusedSlashOptionString(data.Options, "name")
	var choices []*discordgo.ApplicationCommandOptionChoice
	if c.IsAllowed(author.ID) && c.skillCatalogFn != nil {
		available, err := c.skillCatalogFn(i.ChannelID, i.GuildID, author.ID)
		if err != nil {
			logger.WarnCF("discord", "Failed to load /skill autocomplete catalog", map[string]any{
				"error":      err.Error(),
				"channel_id": i.ChannelID,
				"guild_id":   i.GuildID,
				"user_id":    author.ID,
			})
		} else {
			choices = autocompleteSkillChoices(available, query)
		}
	}
	_ = c.interactionRespondFn(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionApplicationCommandAutocompleteResult,
		Data: &discordgo.InteractionResponseData{
			Choices: choices,
		},
	})
}

func (c *DiscordChannel) handleSkillInteraction(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	name := slashOptionString(data.Options, "name")
	prompt := slashOptionString(data.Options, "prompt")
	if name == "" {
		c.respondInteractionMessage(i.Interaction, "The `/skill` name is required.")
		return
	}
	if prompt == "" {
		c.respondInteractionMessage(i.Interaction, "The `/skill` prompt is required.")
		return
	}
	author := interactionAuthor(i.Interaction)
	if author == nil {
		return
	}
	if !c.IsAllowed(author.ID) {
		c.respondInteractionMessage(i.Interaction, "You are not allowed to use this sciClaw bot.")
		return
	}
	if c.skillCatalogFn == nil {
		c.respondInteractionMessage(i.Interaction, "The `/skill` catalog is not configured on this sciClaw instance.")
		return
	}
	if err := c.deferInteraction(i.Interaction); err != nil {
		logger.WarnCF("discord", "Failed to defer /skill interaction", map[string]any{
			"error": err.Error(),
		})
		return
	}
	available, err := c.skillCatalogFn(i.ChannelID, i.GuildID, author.ID)
	if err != nil {
		logger.WarnCF("discord", "Failed to load /skill catalog", map[string]any{
			"error":      err.Error(),
			"channel_id": i.ChannelID,
			"guild_id":   i.GuildID,
			"user_id":    author.ID,
		})
		c.editDeferredInteractionMessage(i.Interaction, "Unable to load skills for this channel right now.")
		return
	}
	selected, ok := validateSlashSkill(available, name)
	if !ok {
		c.editDeferredInteractionMessage(i.Interaction, fmt.Sprintf("The skill %q is not available in this channel.", name))
		return
	}
	metadata := buildSlashMetadata(i.Interaction, data.Name, author)
	metadata["requested_skill_name"] = selected.Name
	c.publishSlashTask(i, author, skillSlashContent(selected.Name, prompt), metadata, fmt.Sprintf("Started. I’m using the skill %q and will reply in the channel below.", selected.Name))
}

func (c *DiscordChannel) handleThemeInteraction(i *discordgo.InteractionCreate, data discordgo.ApplicationCommandInteractionData) {
	style := slashOptionString(data.Options, "style")
	if style == "" {
		c.respondInteractionEphemeral(i.Interaction, "Please select a theme.")
		return
	}
	author := interactionAuthor(i.Interaction)
	if author == nil {
		return
	}
	if !c.IsAllowed(author.ID) {
		c.respondInteractionEphemeral(i.Interaction, "You are not allowed to use this sciClaw bot.")
		return
	}
	if c.themeSetFn == nil {
		c.respondInteractionEphemeral(i.Interaction, "Theme preferences are not configured on this sciClaw instance.")
		return
	}
	if err := c.themeSetFn("discord:"+author.ID, author.Username, style); err != nil {
		c.respondInteractionEphemeral(i.Interaction, fmt.Sprintf("Failed to set theme: %v", err))
		return
	}
	label := strings.ToUpper(style[:1]) + style[1:]
	c.respondInteractionEphemeral(i.Interaction, fmt.Sprintf("Answer theme set to **%s**. I'll use this style for all your messages.", label))
}

func (c *DiscordChannel) respondInteractionEphemeral(interaction *discordgo.Interaction, content string) {
	if c.interactionRespondFn == nil {
		return
	}
	c.interactionRespondFn(interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
}

func (c *DiscordChannel) handleMessageUpdate(s *discordgo.Session, m *discordgo.MessageUpdate) {
	// Discord fires MessageUpdate for embed unfurls, link previews, and pin
	// updates where Author/Content may be nil. Skip those.
	if m == nil || m.Message == nil || m.Author == nil {
		return
	}
	// Only process edits that mention the bot — this lets users add a
	// forgotten @mention without re-triggering on every typo fix.
	c.processIncomingMessage(s, m.Message, true)
}

// processIncomingMessage is the shared handler for both new messages and edits.
// When editOnly is true the message is silently dropped unless it mentions the bot.
func (c *DiscordChannel) processIncomingMessage(_ *discordgo.Session, m *discordgo.Message, editOnly bool) {
	if m.Author == nil {
		return
	}

	if strings.TrimSpace(c.botUserID) != "" && m.Author.ID == c.botUserID {
		return
	}

	// 检查白名单，避免为被拒绝的用户下载附件和转录
	if !c.IsAllowed(m.Author.ID) {
		logger.DebugCF("discord", "Message rejected by allowlist", map[string]any{
			"user_id": m.Author.ID,
		})
		return
	}

	senderID := m.Author.ID
	senderName := m.Author.Username
	if m.Author.Discriminator != "" && m.Author.Discriminator != "0" {
		senderName += "#" + m.Author.Discriminator
	}

	// Detect any bot activation signal and keep direct mentions distinct from
	// broader role-based pings so mention_only routing can stay strict.
	isMention := m.GuildID == "" // DMs are always "mentions"
	hasDirectMention := isMention
	replyToBot := false

	// DEBUG: Log all incoming messages to trace mention detection
	logger.InfoCF("discord", "Mention detection start", map[string]any{
		"content_preview": utils.Truncate(m.Content, 100),
		"mentions_count":  len(m.Mentions),
		"mention_roles":   len(m.MentionRoles),
		"bot_user_id":     c.botUserID,
		"guild_id":        m.GuildID,
		"channel_id":      m.ChannelID,
		"message_id":      m.ID,
		"sender_id":       senderID,
		"has_msg_ref":     m.MessageReference != nil,
		"has_ref_msg":     m.ReferencedMessage != nil,
		"is_edit":         editOnly,
	})

	if !isMention {
		// Check direct user mentions
		for _, u := range m.Mentions {
			logger.InfoCF("discord", "Checking mention", map[string]any{
				"mention_id": u.ID,
				"bot_id":     c.botUserID,
				"match":      u.ID == c.botUserID,
			})
			if u.ID == c.botUserID {
				isMention = true
				hasDirectMention = true
				break
			}
		}
		// Check if the bot's managed role was mentioned (Discord creates a
		// role with the same name as the bot; users frequently pick the role
		// from autocomplete instead of the bot user).
		if !isMention && m.GuildID != "" {
			for _, roleID := range m.MentionRoles {
				if c.isBotRoleMention(m.GuildID, roleID) {
					isMention = true
					hasDirectMention = true
					break
				}
			}
		}
		// Check if replying to a bot message
		if !isMention && m.MessageReference != nil && m.ReferencedMessage != nil {
			if m.ReferencedMessage.Author != nil && m.ReferencedMessage.Author.ID == c.botUserID {
				isMention = true
				replyToBot = true
			}
		}
	}

	// For edits, only process if the edited message mentions the bot.
	if editOnly && !isMention {
		return
	}

	content := m.Content
	if isMention && c.botUserID != "" {
		content = regexp.MustCompile(`<@!?`+regexp.QuoteMeta(c.botUserID)+`>`).ReplaceAllString(content, "")
		// Also strip the bot's managed role mention (<@&ROLE_ID>)
		for _, roleID := range m.MentionRoles {
			if m.GuildID != "" && c.isBotRoleMention(m.GuildID, roleID) {
				content = strings.ReplaceAll(content, "<@&"+roleID+">", "")
			}
		}
		content = strings.TrimSpace(content)
	}
	mediaPaths := make([]string, 0, len(m.Attachments))
	localFiles := make([]string, 0, len(m.Attachments))

	// 确保临时文件在函数返回时被清理
	defer func() {
		for _, file := range localFiles {
			if err := os.Remove(file); err != nil {
				logger.DebugCF("discord", "Failed to cleanup temp file", map[string]any{
					"file":  file,
					"error": err.Error(),
				})
			}
		}
	}()

	for _, attachment := range m.Attachments {
		isAudio := utils.IsAudioFile(attachment.Filename, attachment.ContentType)

		if isAudio {
			localPath := c.downloadAttachment(attachment.URL, attachment.Filename)
			if localPath != "" {
				localFiles = append(localFiles, localPath)

				transcribedText := ""
				if c.transcriber != nil && c.transcriber.IsAvailable() {
					ctx, cancel := context.WithTimeout(c.getContext(), transcriptionTimeout)
					result, err := c.transcriber.Transcribe(ctx, localPath)
					cancel() // 立即释放context资源，避免在for循环中泄漏

					if err != nil {
						logger.ErrorCF("discord", "Voice transcription failed", map[string]any{
							"error": err.Error(),
						})
						transcribedText = fmt.Sprintf("[audio: %s (transcription failed)]", attachment.Filename)
					} else {
						transcribedText = fmt.Sprintf("[audio transcription: %s]", result.Text)
						logger.DebugCF("discord", "Audio transcribed successfully", map[string]any{
							"text": result.Text,
						})
					}
				} else {
					transcribedText = fmt.Sprintf("[audio: %s]", attachment.Filename)
				}

				content = appendContent(content, transcribedText)
			} else {
				logger.WarnCF("discord", "Failed to download audio attachment", map[string]any{
					"url":      attachment.URL,
					"filename": attachment.Filename,
				})
				mediaPaths = append(mediaPaths, attachment.URL)
				content = appendContent(content, fmt.Sprintf("[attachment: %s]", attachment.Filename))
			}
		} else {
			mediaPaths = append(mediaPaths, attachment.URL)
			content = appendContent(content, fmt.Sprintf("[attachment: %s]", attachment.Filename))
		}
	}

	if content == "" && len(mediaPaths) == 0 {
		logger.InfoCF("discord", "Dropping empty inbound message payload", map[string]any{
			"channel_id":  m.ChannelID,
			"guild_id":    m.GuildID,
			"sender_id":   senderID,
			"is_mention":  isMention,
			"attachments": len(m.Attachments),
		})
		return
	}

	if content == "" {
		content = "[media only]"
	}

	if c.shouldDropDuplicateInbound(m.ID, content, mediaPaths, hasDirectMention, replyToBot) {
		logger.InfoCF("discord", "Dropping duplicate inbound message event", map[string]any{
			"message_id": m.ID,
			"sender_id":  senderID,
			"channel_id": m.ChannelID,
			"is_edit":    editOnly,
		})
		return
	}

	logger.DebugCF("discord", "Received message", map[string]any{
		"sender_name": senderName,
		"sender_id":   senderID,
		"preview":     utils.Truncate(content, 50),
		"is_edit":     editOnly,
	})
	if isMention {
		c.startTyping(m.ChannelID)
	}

	metadata := map[string]string{
		"message_id":         m.ID,
		"user_id":            senderID,
		"username":           m.Author.Username,
		"display_name":       senderName,
		"guild_id":           m.GuildID,
		"channel_id":         m.ChannelID,
		"is_dm":              fmt.Sprintf("%t", m.GuildID == ""),
		"is_mention":         fmt.Sprintf("%t", isMention),
		"has_direct_mention": fmt.Sprintf("%t", hasDirectMention),
		"reply_to_bot":       fmt.Sprintf("%t", replyToBot),
		"is_edit":            fmt.Sprintf("%t", editOnly),
	}
	if m.MessageReference != nil {
		metadata["reply_message_id"] = strings.TrimSpace(m.MessageReference.MessageID)
	}
	if m.ReferencedMessage != nil && m.ReferencedMessage.Author != nil {
		metadata["reply_author_id"] = strings.TrimSpace(m.ReferencedMessage.Author.ID)
	}

	c.HandleMessage(senderID, m.ChannelID, content, mediaPaths, metadata)
}

func (c *DiscordChannel) downloadAttachment(url, filename string) string {
	return utils.DownloadFile(url, filename, utils.DownloadOptions{
		LoggerPrefix: "discord",
	})
}

func (c *DiscordChannel) sendMessage(channelID string, msg bus.OutboundMessage) error {
	if c.sendMessageFn != nil {
		return c.sendMessageFn(channelID, msg)
	}
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	_, err := c.session.ChannelMessageSendComplex(channelID, discordMessageSend(msg))
	return err
}

func (c *DiscordChannel) sendProgressMessage(channelID string, msg bus.OutboundMessage) (string, error) {
	if c.sendProgressMessageFn != nil {
		return c.sendProgressMessageFn(channelID, msg)
	}
	if c.session == nil {
		return "", fmt.Errorf("discord session is nil")
	}
	if c.sendMessageFn != nil {
		if err := c.sendMessageFn(channelID, msg); err != nil {
			return "", err
		}
		return "", nil
	}
	sent, err := c.session.ChannelMessageSendComplex(channelID, discordMessageSend(msg))
	if err != nil {
		return "", err
	}
	return sent.ID, nil
}

func (c *DiscordChannel) editProgressMessage(channelID, messageID string, msg bus.OutboundMessage) error {
	if c.editMessageFn != nil {
		return c.editMessageFn(channelID, messageID, msg)
	}
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	_, err := c.session.ChannelMessageEditComplex(discordMessageEdit(channelID, messageID, msg))
	return err
}

func (c *DiscordChannel) sendFile(channelID, content string, attachment bus.OutboundAttachment) error {
	if c.sendFileFn != nil {
		return c.sendFileFn(channelID, content, attachment)
	}
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	return sendDiscordAttachment(c.session, channelID, content, attachment)
}

func discordMessageSend(msg bus.OutboundMessage) *discordgo.MessageSend {
	send := &discordgo.MessageSend{}
	if len(msg.Embeds) == 0 {
		send.Content = msg.Content
	} else {
		send.Content = ""
	}
	send.Embeds = discordEmbeds(msg.Embeds)
	return send
}

func discordMessageEdit(channelID, messageID string, msg bus.OutboundMessage) *discordgo.MessageEdit {
	content := msg.Content
	if len(msg.Embeds) > 0 {
		content = ""
	}
	embeds := discordEmbeds(msg.Embeds)
	return &discordgo.MessageEdit{
		ID:      messageID,
		Channel: channelID,
		Content: &content,
		Embeds:  &embeds,
	}
}

func discordEmbeds(embeds []bus.OutboundEmbed) []*discordgo.MessageEmbed {
	if len(embeds) == 0 {
		return nil
	}
	out := make([]*discordgo.MessageEmbed, 0, len(embeds))
	for _, embed := range embeds {
		fields := make([]*discordgo.MessageEmbedField, 0, len(embed.Fields))
		for _, field := range embed.Fields {
			fields = append(fields, &discordgo.MessageEmbedField{
				Name:   field.Name,
				Value:  field.Value,
				Inline: field.Inline,
			})
		}
		msgEmbed := &discordgo.MessageEmbed{
			Title:       embed.Title,
			Description: embed.Description,
			Color:       embed.Color,
			Fields:      fields,
		}
		if strings.TrimSpace(embed.Footer) != "" {
			msgEmbed.Footer = &discordgo.MessageEmbedFooter{Text: embed.Footer}
		}
		if embed.TimestampUnix > 0 {
			msgEmbed.Timestamp = time.Unix(embed.TimestampUnix, 0).UTC().Format(time.RFC3339)
		}
		out = append(out, msgEmbed)
	}
	return out
}

func (c *DiscordChannel) sendTyping(channelID string) error {
	if c.sendTypingFn != nil {
		return c.sendTypingFn(channelID)
	}
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	return c.session.ChannelTyping(channelID)
}

func (c *DiscordChannel) startTyping(channelID string) {
	if strings.TrimSpace(channelID) == "" {
		return
	}

	c.typingMu.Lock()
	if state, ok := c.typing[channelID]; ok {
		state.pending++
		c.typingMu.Unlock()
		return
	}

	loopCtx, cancel := context.WithTimeout(c.getContext(), maxTypingDuration)
	c.typing[channelID] = &typingState{
		pending: 1,
		cancel:  cancel,
	}
	every := c.typingEvery
	c.typingMu.Unlock()

	go c.runTypingLoop(loopCtx, channelID, every)
}

func (c *DiscordChannel) stopTyping(channelID string) {
	if strings.TrimSpace(channelID) == "" {
		return
	}

	var cancel context.CancelFunc
	c.typingMu.Lock()
	state, ok := c.typing[channelID]
	if ok {
		// Always cancel on send — pending count can drift when multiple
		// mentions arrive but only one response is sent.
		delete(c.typing, channelID)
		cancel = state.cancel
	}
	c.typingMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func (c *DiscordChannel) stopAllTyping() {
	c.typingMu.Lock()
	cancels := make([]context.CancelFunc, 0, len(c.typing))
	for key, state := range c.typing {
		delete(c.typing, key)
		if state != nil && state.cancel != nil {
			cancels = append(cancels, state.cancel)
		}
	}
	c.typingMu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

func (c *DiscordChannel) runTypingLoop(ctx context.Context, channelID string, every time.Duration) {
	if every <= 0 {
		every = typingInterval
	}

	// Clean up map entry when the loop exits (covers both stopTyping and timeout).
	defer func() {
		c.typingMu.Lock()
		delete(c.typing, channelID)
		c.typingMu.Unlock()
	}()

	if err := c.sendTyping(channelID); err != nil {
		logger.DebugCF("discord", "Typing indicator send failed", map[string]any{
			"channel_id": channelID,
			"error":      err.Error(),
		})
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := c.sendTyping(channelID); err != nil {
				logger.DebugCF("discord", "Typing indicator heartbeat failed", map[string]any{
					"channel_id": channelID,
					"error":      err.Error(),
				})
			}
		}
	}
}

func splitDiscordMessage(content string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = discordMaxRunes
	}

	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{"[empty message]"}
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return []string{trimmed}
	}

	chunks := make([]string, 0, (len(runes)/maxRunes)+1)
	remaining := runes
	for len(remaining) > maxRunes {
		split := maxRunes
		windowStart := maxRunes - 200
		if windowStart < 0 {
			windowStart = 0
		}
		for i := maxRunes - 1; i >= windowStart; i-- {
			if remaining[i] == '\n' || remaining[i] == ' ' || remaining[i] == '\t' {
				split = i
				break
			}
		}
		if split <= 0 {
			split = maxRunes
		}

		chunk := strings.TrimSpace(string(remaining[:split]))
		if chunk == "" {
			chunk = string(remaining[:maxRunes])
			split = maxRunes
		}
		chunks = append(chunks, chunk)

		remaining = remaining[split:]
		for len(remaining) > 0 && (remaining[0] == ' ' || remaining[0] == '\n' || remaining[0] == '\t' || remaining[0] == '\r') {
			remaining = remaining[1:]
		}
	}

	if len(remaining) > 0 {
		last := strings.TrimSpace(string(remaining))
		if last != "" {
			chunks = append(chunks, last)
		}
	}

	if len(chunks) == 0 {
		return []string{"[empty message]"}
	}
	return chunks
}

func sendDiscordAttachment(session *discordgo.Session, channelID, content string, attachment bus.OutboundAttachment) error {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return fmt.Errorf("attachment path is required")
	}

	stat, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat attachment %q: %w", path, err)
	}
	if stat.IsDir() {
		return fmt.Errorf("attachment %q is a directory", path)
	}
	if stat.Size() > discordMaxFileBytes {
		return fmt.Errorf("attachment %q exceeds Discord limit (%d bytes)", path, discordMaxFileBytes)
	}
	if session == nil {
		return fmt.Errorf("discord session is nil")
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open attachment %q: %w", path, err)
	}
	defer file.Close()

	name := strings.TrimSpace(attachment.Filename)
	if name == "" {
		name = filepath.Base(path)
	}

	_, err = session.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content: content,
		Files: []*discordgo.File{
			{
				Name:   name,
				Reader: file,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("send attachment %q: %w", name, err)
	}
	return nil
}
