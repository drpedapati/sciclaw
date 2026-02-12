package channels

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
	"github.com/sipeed/picoclaw/pkg/voice"
)

type DiscordChannel struct {
	*BaseChannel
	session     *discordgo.Session
	config      config.DiscordConfig
	transcriber *voice.GroqTranscriber
}

func NewDiscordChannel(cfg config.DiscordConfig, bus *bus.MessageBus) (*DiscordChannel, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	base := NewBaseChannel("discord", cfg, bus, cfg.AllowFrom)

	return &DiscordChannel{
		BaseChannel: base,
		session:     session,
		config:      cfg,
		transcriber: nil,
	}, nil
}

func (c *DiscordChannel) SetTranscriber(transcriber *voice.GroqTranscriber) {
	c.transcriber = transcriber
}

func (c *DiscordChannel) Start(ctx context.Context) error {
	logger.InfoC("discord", "Starting Discord bot")

	c.session.AddHandler(c.handleMessage)

	if err := c.session.Open(); err != nil {
		return fmt.Errorf("failed to open discord session: %w", err)
	}

	c.setRunning(true)

	botUser, err := c.session.User("@me")
	if err != nil {
		return fmt.Errorf("failed to get bot user: %w", err)
	}
	logger.InfoCF("discord", "Discord bot connected", map[string]interface{}{
		"username": botUser.Username,
		"user_id":  botUser.ID,
	})

	return nil
}

func (c *DiscordChannel) Stop(ctx context.Context) error {
	logger.InfoC("discord", "Stopping Discord bot")
	c.setRunning(false)

	if err := c.session.Close(); err != nil {
		return fmt.Errorf("failed to close discord session: %w", err)
	}

	return nil
}

func (c *DiscordChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("discord bot not running")
	}

	channelID := msg.ChatID
	if channelID == "" {
		return fmt.Errorf("channel ID is empty")
	}

	message := msg.Content

	if _, err := c.session.ChannelMessageSend(channelID, message); err != nil {
		return fmt.Errorf("failed to send discord message: %w", err)
	}

	return nil
}

func (c *DiscordChannel) handleMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m == nil || m.Author == nil {
		return
	}

	if m.Author.ID == s.State.User.ID {
		return
	}

	senderID := m.Author.ID
	senderName := m.Author.Username
	if m.Author.Discriminator != "" && m.Author.Discriminator != "0" {
		senderName += "#" + m.Author.Discriminator
	}

	content := m.Content
	mediaPaths := []string{}

	for _, attachment := range m.Attachments {
		isAudio := isAudioFile(attachment.Filename, attachment.ContentType)

		if isAudio {
			localPath := c.downloadAttachment(attachment.URL, attachment.Filename)
			if localPath != "" {
				mediaPaths = append(mediaPaths, localPath)

				transcribedText := ""
				if c.transcriber != nil && c.transcriber.IsAvailable() {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()

					result, err := c.transcriber.Transcribe(ctx, localPath)
					if err != nil {
						log.Printf("Voice transcription failed: %v", err)
						transcribedText = fmt.Sprintf("[audio: %s (transcription failed)]", localPath)
					} else {
						transcribedText = fmt.Sprintf("[audio transcription: %s]", result.Text)
						log.Printf("Audio transcribed successfully: %s", result.Text)
					}
				} else {
					transcribedText = fmt.Sprintf("[audio: %s]", localPath)
				}

				if content != "" {
					content += "\n"
				}
				content += transcribedText
			} else {
				mediaPaths = append(mediaPaths, attachment.URL)
				if content != "" {
					content += "\n"
				}
				content += fmt.Sprintf("[attachment: %s]", attachment.URL)
			}
		} else {
			mediaPaths = append(mediaPaths, attachment.URL)
			if content != "" {
				content += "\n"
			}
			content += fmt.Sprintf("[attachment: %s]", attachment.URL)
		}
	}

	if content == "" && len(mediaPaths) == 0 {
		return
	}

	if content == "" {
		content = "[media only]"
	}

	logger.DebugCF("discord", "Received message", map[string]interface{}{
		"sender_name": senderName,
		"sender_id":   senderID,
		"preview":     utils.Truncate(content, 50),
	})

	metadata := map[string]string{
		"message_id":   m.ID,
		"user_id":      senderID,
		"username":     m.Author.Username,
		"display_name": senderName,
		"guild_id":     m.GuildID,
		"channel_id":   m.ChannelID,
		"is_dm":        fmt.Sprintf("%t", m.GuildID == ""),
	}

	c.HandleMessage(senderID, m.ChannelID, content, mediaPaths, metadata)
}

func isAudioFile(filename, contentType string) bool {
	audioExtensions := []string{".mp3", ".wav", ".ogg", ".m4a", ".flac", ".aac", ".wma"}
	audioTypes := []string{"audio/", "application/ogg", "application/x-ogg"}

	for _, ext := range audioExtensions {
		if strings.HasSuffix(strings.ToLower(filename), ext) {
			return true
		}
	}

	for _, audioType := range audioTypes {
		if strings.HasPrefix(strings.ToLower(contentType), audioType) {
			return true
		}
	}

	return false
}

func (c *DiscordChannel) downloadAttachment(url, filename string) string {
	mediaDir := filepath.Join(os.TempDir(), "picoclaw_media")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		log.Printf("Failed to create media directory: %v", err)
		return ""
	}

	localPath := filepath.Join(mediaDir, filename)

	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Failed to download attachment: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("Failed to download attachment, status: %d", resp.StatusCode)
		return ""
	}

	out, err := os.Create(localPath)
	if err != nil {
		log.Printf("Failed to create file: %v", err)
		return ""
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		log.Printf("Failed to write file: %v", err)
		return ""
	}

	log.Printf("Attachment downloaded successfully to: %s", localPath)
	return localPath
}
