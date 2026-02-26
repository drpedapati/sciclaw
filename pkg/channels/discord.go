package channels

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
)

type typingState struct {
	pending int
	cancel  context.CancelFunc
}

type DiscordChannel struct {
	*BaseChannel
	session     *discordgo.Session
	config      config.DiscordConfig
	transcriber *voice.GroqTranscriber
	ctx         context.Context
	botUserID   string

	typingMu      sync.Mutex
	typing        map[string]*typingState
	typingEvery   time.Duration
	sendMessageFn func(channelID, content string) error
	sendFileFn    func(channelID, content string, attachment bus.OutboundAttachment) error
	sendTypingFn  func(channelID string) error
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
		sendMessageFn: func(channelID, content string) error {
			_, err := session.ChannelMessageSend(channelID, content)
			return err
		},
		sendFileFn: func(channelID, content string, attachment bus.OutboundAttachment) error {
			return sendDiscordAttachment(session, channelID, content, attachment)
		},
		sendTypingFn: func(channelID string) error {
			return session.ChannelTyping(channelID)
		},
	}, nil
}

func (c *DiscordChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
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
			if err := c.sendMessage(channelID, chunk); err != nil {
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
		if err := c.sendMessage(channelID, chunk); err != nil {
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

func (c *DiscordChannel) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil {
		return
	}

	if m.Author.ID == s.State.User.ID {
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

	// Detect @bot mention (direct user mention OR role mention)
	isMention := m.GuildID == "" // DMs are always "mentions"
	if !isMention {
		// Check direct user mentions
		for _, u := range m.Mentions {
			if u.ID == c.botUserID {
				isMention = true
				break
			}
		}
		// Check role mentions - if user mentioned any role, check if bot has that role
		if !isMention && len(m.MentionRoles) > 0 && m.GuildID != "" {
			if member, err := s.GuildMember(m.GuildID, c.botUserID); err == nil {
				for _, mentionedRole := range m.MentionRoles {
					for _, botRole := range member.Roles {
						if mentionedRole == botRole {
							isMention = true
							break
						}
					}
					if isMention {
						break
					}
				}
			}
		}
	}

	content := m.Content
	if isMention && c.botUserID != "" {
		content = regexp.MustCompile(`<@!?`+regexp.QuoteMeta(c.botUserID)+`>`).ReplaceAllString(content, "")
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
				content = appendContent(content, fmt.Sprintf("[attachment: %s]", attachment.URL))
			}
		} else {
			mediaPaths = append(mediaPaths, attachment.URL)
			content = appendContent(content, fmt.Sprintf("[attachment: %s]", attachment.URL))
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

	logger.DebugCF("discord", "Received message", map[string]any{
		"sender_name": senderName,
		"sender_id":   senderID,
		"preview":     utils.Truncate(content, 50),
	})
	if isMention {
		c.startTyping(m.ChannelID)
	}

	metadata := map[string]string{
		"message_id":   m.ID,
		"user_id":      senderID,
		"username":     m.Author.Username,
		"display_name": senderName,
		"guild_id":     m.GuildID,
		"channel_id":   m.ChannelID,
		"is_dm":        fmt.Sprintf("%t", m.GuildID == ""),
		"is_mention":   fmt.Sprintf("%t", isMention),
	}

	c.HandleMessage(senderID, m.ChannelID, content, mediaPaths, metadata)
}

func (c *DiscordChannel) downloadAttachment(url, filename string) string {
	return utils.DownloadFile(url, filename, utils.DownloadOptions{
		LoggerPrefix: "discord",
	})
}

func (c *DiscordChannel) sendMessage(channelID, content string) error {
	if c.sendMessageFn != nil {
		return c.sendMessageFn(channelID, content)
	}
	if c.session == nil {
		return fmt.Errorf("discord session is nil")
	}
	_, err := c.session.ChannelMessageSend(channelID, content)
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
