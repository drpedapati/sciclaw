package channels

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newTestDiscordChannel() *DiscordChannel {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error { return nil }
	ch.setRunning(true)
	return ch
}

func TestNormalizeDiscordBotToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "raw", in: "abc123", want: "abc123"},
		{name: "bot prefix", in: "Bot abc123", want: "abc123"},
		{name: "bot prefix lowercase", in: "bot abc123", want: "abc123"},
		{name: "quoted", in: "\"abc123\"", want: "abc123"},
		{name: "quoted with bot prefix", in: "'Bot abc123'", want: "abc123"},
		{name: "spaces", in: "   Bot   abc123   ", want: "abc123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeDiscordBotToken(tc.in); got != tc.want {
				t.Fatalf("NormalizeDiscordBotToken(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDiscordTypingStopsOnFirstReply(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	calls := 0
	ch.sendTypingFn = func(channelID string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}

	// Simulate multiple mentions arriving before a single reply.
	ch.startTyping("chan-1")
	ch.startTyping("chan-1")
	time.Sleep(25 * time.Millisecond)

	mu.Lock()
	beforeStop := calls
	mu.Unlock()
	if beforeStop == 0 {
		t.Fatalf("expected typing calls before stop, got 0")
	}

	// A single stopTyping (from Send) should cancel the loop entirely,
	// even though two startTyping calls were made.
	ch.stopTyping("chan-1")
	time.Sleep(25 * time.Millisecond)

	mu.Lock()
	afterStop := calls
	mu.Unlock()
	time.Sleep(25 * time.Millisecond)
	mu.Lock()
	afterWait := calls
	mu.Unlock()

	if afterWait != afterStop {
		t.Fatalf("typing should stop after first stop, got afterStop=%d afterWait=%d", afterStop, afterWait)
	}
}

func TestDiscordSendClearsTypingFully(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.startTyping("chan-2")
	ch.startTyping("chan-2")

	// First Send should clear typing entirely (not just decrement).
	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-2",
		Content: "done",
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	ch.typingMu.Lock()
	_, ok := ch.typing["chan-2"]
	ch.typingMu.Unlock()
	if ok {
		t.Fatalf("expected typing state fully cleared after first send")
	}
}

func TestDiscordStopCancelsAllTypingLoops(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.startTyping("chan-a")
	ch.startTyping("chan-b")

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	ch.typingMu.Lock()
	n := len(ch.typing)
	ch.typingMu.Unlock()
	if n != 0 {
		t.Fatalf("expected no typing loops after stop, got %d", n)
	}
}

func TestDiscordTypingAutoExpiresOnTimeout(t *testing.T) {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 5 * time.Millisecond,
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error { return nil }
	ch.setRunning(true)

	// Override the typing context to use a very short timeout for the test.
	// We can't change maxTypingDuration, so we'll start typing with a short-lived parent context.
	shortCtx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ch.ctx = shortCtx

	ch.startTyping("chan-timeout")

	// Typing should be active initially.
	ch.typingMu.Lock()
	_, active := ch.typing["chan-timeout"]
	ch.typingMu.Unlock()
	if !active {
		t.Fatalf("expected typing to be active initially")
	}

	// Wait for parent context to expire + loop cleanup.
	time.Sleep(80 * time.Millisecond)

	ch.typingMu.Lock()
	_, still := ch.typing["chan-timeout"]
	ch.typingMu.Unlock()
	if still {
		t.Fatalf("expected typing to auto-expire after context timeout")
	}
}

func TestDiscordSendStopsTypingEvenOnError(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.startTyping("chan-err")

	// Simulate bot stopped — Send returns error, but typing should still be cleared.
	ch.setRunning(false)
	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-err",
		Content: "test",
	})
	if err == nil {
		t.Fatalf("expected error when bot not running")
	}

	ch.typingMu.Lock()
	_, ok := ch.typing["chan-err"]
	ch.typingMu.Unlock()
	if ok {
		t.Fatalf("expected typing cleared even when Send fails")
	}
}

func TestSplitDiscordMessage_RespectsLimit(t *testing.T) {
	msg := strings.Repeat("a", 4205)
	chunks := splitDiscordMessage(msg, discordMaxRunes)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if got := len([]rune(chunks[0])); got != 2000 {
		t.Fatalf("expected first chunk length 2000, got %d", got)
	}
	if got := len([]rune(chunks[1])); got != 2000 {
		t.Fatalf("expected second chunk length 2000, got %d", got)
	}
	if got := len([]rune(chunks[2])); got != 205 {
		t.Fatalf("expected third chunk length 205, got %d", got)
	}
}

func TestDiscordSend_SendsChunkedMessages(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	var sent []bus.OutboundMessage
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return nil
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-3",
		Content: strings.Repeat("b", 4100),
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	mu.Lock()
	count := len(sent)
	copySent := append([]bus.OutboundMessage(nil), sent...)
	mu.Unlock()

	if count != 3 {
		t.Fatalf("expected 3 sent chunks, got %d", count)
	}
	for i, chunk := range copySent {
		if len(chunk.Embeds) != 0 {
			t.Fatalf("unexpected embeds on chunk %d: %#v", i, chunk.Embeds)
		}
		if chunk.ChatID != "chan-3" {
			t.Fatalf("chunk %d chat id = %q, want chan-3", i, chunk.ChatID)
		}
		if chunk.Channel != "discord" {
			t.Fatalf("chunk %d channel = %q, want discord", i, chunk.Channel)
		}
		content := chunk.Content
		if n := len([]rune(content)); n > discordMaxRunes {
			t.Fatalf("chunk %d exceeds limit: %d", i, n)
		}
	}
}

func TestDiscordSendOrEditProgressEditsExistingMessage(t *testing.T) {
	ch := newTestDiscordChannel()
	var edited []bus.OutboundMessage
	ch.editMessageFn = func(channelID, messageID string, msg bus.OutboundMessage) error {
		edited = append(edited, msg)
		return nil
	}
	progress := bus.OutboundMessage{Content: "Thinking", Embeds: []bus.OutboundEmbed{{Title: "sciClaw · J1"}}}

	id, err := ch.SendOrEditProgress(context.Background(), "chan-1", "progress-123", progress)
	if err != nil {
		t.Fatalf("SendOrEditProgress: %v", err)
	}
	if id != "progress-123" {
		t.Fatalf("message id = %q, want progress-123", id)
	}
	if len(edited) != 1 || edited[0].Content != "Thinking" || len(edited[0].Embeds) != 1 || edited[0].Embeds[0].Title != "sciClaw · J1" {
		t.Fatalf("expected progress edit call, got %#v", edited)
	}
}

func TestDiscordProcessIncomingMessage_PublishesReplyMetadata(t *testing.T) {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-1",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	session := &discordgo.Session{State: &discordgo.State{Ready: discordgo.Ready{User: &discordgo.User{ID: "bot-1"}}}}
	msg := &discordgo.Message{
		ID:        "m-1",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "status",
		Author:    &discordgo.User{ID: "user-1", Username: "alice"},
		MessageReference: &discordgo.MessageReference{
			MessageID: "progress-123",
		},
		ReferencedMessage: &discordgo.Message{
			ID:     "progress-123",
			Author: &discordgo.User{ID: "bot-1", Username: "sciclaw"},
		},
	}

	ch.processIncomingMessage(session, msg, false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if got := in.Metadata["reply_message_id"]; got != "progress-123" {
		t.Fatalf("reply_message_id = %q, want progress-123", got)
	}
	if got := in.Metadata["reply_to_bot"]; got != "true" {
		t.Fatalf("reply_to_bot = %q, want true", got)
	}
	if got := in.Metadata["is_mention"]; got != "true" {
		t.Fatalf("is_mention = %q, want true", got)
	}
}

func TestDiscordProcessIncomingMessage_RoleMentionDoesNotCountAsBotMention(t *testing.T) {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-1",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	msg := &discordgo.Message{
		ID:           "m-role",
		ChannelID:    "chan-1",
		GuildID:      "guild-1",
		Content:      "<@&role-1> status",
		Author:       &discordgo.User{ID: "user-1", Username: "alice"},
		MentionRoles: []string{"role-1"},
	}

	ch.processIncomingMessage(newTestSession("bot-1"), msg, false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if got := in.Metadata["has_direct_mention"]; got != "false" {
		t.Fatalf("has_direct_mention = %q, want false", got)
	}
	if got := in.Metadata["is_mention"]; got != "false" {
		t.Fatalf("is_mention = %q, want false", got)
	}
}

func TestDiscordProcessIncomingMessage_BotRoleMentionCountsAsMention(t *testing.T) {
	sess := newTestSession("bot-1")
	sess.State.User.Username = "sciclaw-app"
	_ = sess.State.GuildAdd(&discordgo.Guild{
		ID: "guild-1",
		Roles: []*discordgo.Role{
			{ID: "role-bot", Name: "sciclaw-app", Managed: true},
			{ID: "role-other", Name: "moderator", Managed: false},
		},
	})
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		session:     sess,
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-1",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	msg := &discordgo.Message{
		ID:           "m-bot-role",
		ChannelID:    "chan-1",
		GuildID:      "guild-1",
		Content:      "<@&role-bot> check status",
		Author:       &discordgo.User{ID: "user-1", Username: "alice"},
		MentionRoles: []string{"role-bot"},
	}

	ch.processIncomingMessage(sess, msg, false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if got := in.Metadata["has_direct_mention"]; got != "true" {
		t.Fatalf("has_direct_mention = %q, want true", got)
	}
	if got := in.Metadata["is_mention"]; got != "true" {
		t.Fatalf("is_mention = %q, want true", got)
	}
	if strings.Contains(in.Content, "<@&role-bot>") {
		t.Fatal("role mention should be stripped from content")
	}
	if !strings.Contains(in.Content, "check status") {
		t.Fatalf("content should contain 'check status', got %q", in.Content)
	}
}

func TestDiscordProcessIncomingMessage_AttachmentsUseFilenamesInContent(t *testing.T) {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-1",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	msg := &discordgo.Message{
		ID:        "m-attach",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "please review",
		Author:    &discordgo.User{ID: "user-1", Username: "alice"},
		Attachments: []*discordgo.MessageAttachment{
			{
				ID:          "att-1",
				Filename:    "draft.docx",
				ContentType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
				URL:         "https://cdn.discordapp.com/attachments/x/draft.docx",
			},
		},
	}

	ch.processIncomingMessage(newTestSession("bot-1"), msg, false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message")
	}
	if !strings.Contains(in.Content, "[attachment: draft.docx]") {
		t.Fatalf("expected attachment filename in content, got %q", in.Content)
	}
	if strings.Contains(in.Content, "cdn.discordapp.com") {
		t.Fatalf("expected attachment content to avoid raw CDN URL, got %q", in.Content)
	}
	if len(in.Media) != 1 || in.Media[0] != "https://cdn.discordapp.com/attachments/x/draft.docx" {
		t.Fatalf("unexpected media payload: %#v", in.Media)
	}
}

func TestDiscordEnsureSlashCommandsCreatesSlashCommands(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	ch.listCommandsFn = func(appID, guildID string) ([]*discordgo.ApplicationCommand, error) {
		if appID != "bot-1" || guildID != "" {
			t.Fatalf("unexpected list args: %q %q", appID, guildID)
		}
		return nil, nil
	}
	var created []string
	ch.createCommandFn = func(appID, guildID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
		created = append(created, cmd.Name)
		switch cmd.Name {
		case "btw":
			if len(cmd.Options) != 1 || cmd.Options[0].Name != "prompt" || !cmd.Options[0].Required {
				t.Fatalf("unexpected btw command options: %#v", cmd.Options)
			}
		case "skill":
			if len(cmd.Options) != 2 {
				t.Fatalf("unexpected skill command options: %#v", cmd.Options)
			}
			if cmd.Options[0].Name != "name" || !cmd.Options[0].Required || !cmd.Options[0].Autocomplete {
				t.Fatalf("unexpected skill name option: %#v", cmd.Options[0])
			}
			if cmd.Options[1].Name != "prompt" || !cmd.Options[1].Required {
				t.Fatalf("unexpected skill prompt option: %#v", cmd.Options[1])
			}
		default:
			t.Fatalf("unexpected command name: %q", cmd.Name)
		}
		return &discordgo.ApplicationCommand{ID: "cmd-1", Name: cmd.Name}, nil
	}
	ch.editCommandFn = func(appID, guildID, cmdID string, cmd *discordgo.ApplicationCommand) (*discordgo.ApplicationCommand, error) {
		t.Fatalf("did not expect edit")
		return nil, nil
	}

	if err := ch.ensureSlashCommands(); err != nil {
		t.Fatalf("ensureSlashCommands: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("expected two create calls, got %d (%v)", len(created), created)
	}
}

func TestDiscordHandleInteractionCreateBTWPublishesInboundAndDefers(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	var response *discordgo.InteractionResponse
	var editedContent string
	ch.interactionRespondFn = func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
		response = resp
		return nil
	}
	ch.interactionEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
		if edit != nil && edit.Content != nil {
			editedContent = *edit.Content
		}
		return nil
	}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "ix-1",
			AppID:     "app-1",
			Type:      discordgo.InteractionApplicationCommand,
			Token:     "token-1",
			GuildID:   "guild-1",
			ChannelID: "chan-1",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "alice"}},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "btw",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "what is this channel about"},
				},
			},
		},
	}

	ch.handleInteractionCreate(nil, interaction)

	if response == nil {
		t.Fatal("expected interaction response")
	}
	if response.Type != discordgo.InteractionResponseDeferredChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral deferred response, got %#v", response.Data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message from /btw interaction")
	}
	if in.Content != "/btw what is this channel about" {
		t.Fatalf("unexpected inbound content: %q", in.Content)
	}
	if in.Metadata["is_slash_command"] != "true" || in.Metadata["command_name"] != "btw" {
		t.Fatalf("unexpected slash metadata: %#v", in.Metadata)
	}
	if in.Metadata["has_direct_mention"] != "true" || in.Metadata["is_mention"] != "true" {
		t.Fatalf("expected slash command to route as direct invocation, got %#v", in.Metadata)
	}
	if !strings.Contains(editedContent, "Started.") {
		t.Fatalf("expected deferred interaction to be replaced with started note, got %q", editedContent)
	}
}

func TestDiscordHandleInteractionCreateBTWRejectsBlankPrompt(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	var response *discordgo.InteractionResponse
	ch.interactionRespondFn = func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
		response = resp
		return nil
	}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "chan-1",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "alice"}},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "btw",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "   "},
				},
			},
		},
	}

	ch.handleInteractionCreate(nil, interaction)

	if response == nil || response.Type != discordgo.InteractionResponseChannelMessageWithSource {
		t.Fatalf("expected immediate validation response, got %#v", response)
	}
	if response.Data == nil || !strings.Contains(response.Data.Content, "prompt is required") {
		t.Fatalf("unexpected validation response: %#v", response.Data)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := ch.BaseChannel.bus.ConsumeInbound(ctx); ok {
		t.Fatal("blank /btw prompt should not reach inbound bus")
	}
}

func TestDiscordHandleInteractionCreateSkillPublishesInboundAndDefers(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	deferred := false
	var editedContent string
	ch.skillCatalogFn = func(channelID, guildID, userID string) ([]SlashSkillChoice, error) {
		if !deferred {
			t.Fatal("expected /skill interaction to defer before loading catalog")
		}
		if channelID != "chan-1" || guildID != "guild-1" || userID != "user-1" {
			t.Fatalf("unexpected skill catalog args: %q %q %q", channelID, guildID, userID)
		}
		return []SlashSkillChoice{
			{Name: "pubmed-cli", Description: "PubMed workflows"},
			{Name: "humanize-text", Description: "Rewrite text naturally"},
		}, nil
	}
	var response *discordgo.InteractionResponse
	ch.interactionRespondFn = func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
		response = resp
		if resp != nil && resp.Type == discordgo.InteractionResponseDeferredChannelMessageWithSource {
			deferred = true
		}
		return nil
	}
	ch.interactionEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
		if edit != nil && edit.Content != nil {
			editedContent = *edit.Content
		}
		return nil
	}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "ix-2",
			AppID:     "app-1",
			Type:      discordgo.InteractionApplicationCommand,
			Token:     "token-2",
			GuildID:   "guild-1",
			ChannelID: "chan-1",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "alice"}},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "skill",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "pubmed-cli"},
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "find the Jeon paper"},
				},
			},
		},
	}

	ch.handleInteractionCreate(nil, interaction)

	if response == nil {
		t.Fatal("expected interaction response")
	}
	if response.Type != discordgo.InteractionResponseDeferredChannelMessageWithSource {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral deferred response, got %#v", response.Data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	in, ok := ch.BaseChannel.bus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message from /skill interaction")
	}
	if !strings.Contains(in.Content, `Use the skill "pubmed-cli" for this task.`) {
		t.Fatalf("unexpected inbound content: %q", in.Content)
	}
	if !strings.Contains(in.Content, "find the Jeon paper") {
		t.Fatalf("unexpected inbound content: %q", in.Content)
	}
	if in.Metadata["is_slash_command"] != "true" || in.Metadata["command_name"] != "skill" {
		t.Fatalf("unexpected slash metadata: %#v", in.Metadata)
	}
	if in.Metadata["requested_skill_name"] != "pubmed-cli" {
		t.Fatalf("requested skill metadata = %q, want pubmed-cli", in.Metadata["requested_skill_name"])
	}
	if !strings.Contains(editedContent, `using the skill "pubmed-cli"`) {
		t.Fatalf("expected deferred interaction to be replaced with started note, got %q", editedContent)
	}
}

func TestDiscordHandleInteractionCreateSkillRejectsUnavailableSkill(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	ch.skillCatalogFn = func(channelID, guildID, userID string) ([]SlashSkillChoice, error) {
		return []SlashSkillChoice{{Name: "pubmed-cli"}}, nil
	}
	var response *discordgo.InteractionResponse
	var editedContent string
	ch.interactionRespondFn = func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
		response = resp
		return nil
	}
	ch.interactionEditFn = func(interaction *discordgo.Interaction, edit *discordgo.WebhookEdit) error {
		if edit != nil && edit.Content != nil {
			editedContent = *edit.Content
		}
		return nil
	}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommand,
			ChannelID: "chan-1",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "alice"}},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "skill",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "missing-skill"},
					{Name: "prompt", Type: discordgo.ApplicationCommandOptionString, Value: "do the thing"},
				},
			},
		},
	}

	ch.handleInteractionCreate(nil, interaction)

	if response == nil || response.Type != discordgo.InteractionResponseDeferredChannelMessageWithSource {
		t.Fatalf("expected deferred validation response, got %#v", response)
	}
	if response.Data == nil || response.Data.Flags != discordgo.MessageFlagsEphemeral {
		t.Fatalf("expected ephemeral deferred response, got %#v", response.Data)
	}
	if !strings.Contains(editedContent, "not available") {
		t.Fatalf("unexpected deferred validation edit: %q", editedContent)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, ok := ch.BaseChannel.bus.ConsumeInbound(ctx); ok {
		t.Fatal("unavailable /skill should not reach inbound bus")
	}
}

func TestDiscordHandleInteractionCreateSkillAutocompleteReturnsMatches(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.botUserID = "bot-1"
	ch.skillCatalogFn = func(channelID, guildID, userID string) ([]SlashSkillChoice, error) {
		return []SlashSkillChoice{
			{Name: "pubmed-cli", Description: "PubMed workflows"},
			{Name: "humanize-text", Description: "Rewrite naturally"},
			{Name: "send-email", Description: "Send email"},
		}, nil
	}
	var response *discordgo.InteractionResponse
	ch.interactionRespondFn = func(interaction *discordgo.Interaction, resp *discordgo.InteractionResponse) error {
		response = resp
		return nil
	}
	interaction := &discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			Type:      discordgo.InteractionApplicationCommandAutocomplete,
			ChannelID: "chan-1",
			GuildID:   "guild-1",
			Member:    &discordgo.Member{User: &discordgo.User{ID: "user-1", Username: "alice"}},
			Data: discordgo.ApplicationCommandInteractionData{
				Name: "skill",
				Options: []*discordgo.ApplicationCommandInteractionDataOption{
					{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "pub", Focused: true},
				},
			},
		},
	}

	ch.handleInteractionCreate(nil, interaction)

	if response == nil {
		t.Fatal("expected autocomplete response")
	}
	if response.Type != discordgo.InteractionApplicationCommandAutocompleteResult {
		t.Fatalf("unexpected response type: %v", response.Type)
	}
	if response.Data == nil || len(response.Data.Choices) != 1 {
		t.Fatalf("unexpected choices: %#v", response.Data)
	}
	if response.Data.Choices[0].Name != "pubmed-cli" {
		t.Fatalf("choice name = %q, want pubmed-cli", response.Data.Choices[0].Name)
	}
}

func TestDiscordSendOrEditProgressReturnsMessageIDForNewProgressMessage(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.sendProgressMessageFn = func(channelID string, msg bus.OutboundMessage) (string, error) {
		if msg.Content != "Thinking" {
			t.Fatalf("content = %q, want Thinking", msg.Content)
		}
		if len(msg.Embeds) != 1 || msg.Embeds[0].Title != "sciClaw · J1" {
			t.Fatalf("unexpected embeds: %#v", msg.Embeds)
		}
		return "progress-123", nil
	}

	id, err := ch.SendOrEditProgress(context.Background(), "chan-1", "", bus.OutboundMessage{Content: "Thinking", Embeds: []bus.OutboundEmbed{{Title: "sciClaw · J1"}}})
	if err != nil {
		t.Fatalf("SendOrEditProgress: %v", err)
	}
	if id != "progress-123" {
		t.Fatalf("message id = %q, want progress-123", id)
	}
}

func TestDiscordSendOrEditProgressFallsBackToNewMessageOnEditFailure(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.editMessageFn = func(channelID, messageID string, msg bus.OutboundMessage) error {
		return os.ErrNotExist
	}
	ch.session = &discordgo.Session{}
	ch.sendMessageFn = nil
	called := 0
	ch.session = &discordgo.Session{}
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error {
		if msg.Content != "Thinking" {
			t.Fatalf("content = %q, want Thinking", msg.Content)
		}
		if len(msg.Embeds) != 1 || msg.Embeds[0].Title != "sciClaw · J1" {
			t.Fatalf("unexpected embeds: %#v", msg.Embeds)
		}
		called++
		return nil
	}

	id, err := ch.SendOrEditProgress(context.Background(), "chan-1", "progress-123", bus.OutboundMessage{Content: "Thinking", Embeds: []bus.OutboundEmbed{{Title: "sciClaw · J1"}}})
	if err != nil {
		t.Fatalf("SendOrEditProgress: %v", err)
	}
	if id != "" {
		t.Fatalf("expected empty fallback id with stub sender, got %q", id)
	}
	if called != 1 {
		t.Fatalf("expected fallback send call, got %d", called)
	}
}

func TestDiscordSendOrEditProgressDoesNotReplaceOnTransientEditFailure(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.editMessageFn = func(channelID, messageID string, msg bus.OutboundMessage) error {
		return fmt.Errorf("temporary discord edit failure")
	}
	called := 0
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error {
		called++
		return nil
	}

	id, err := ch.SendOrEditProgress(context.Background(), "chan-1", "progress-123", bus.OutboundMessage{Content: "Thinking", Embeds: []bus.OutboundEmbed{{Title: "sciClaw · J1"}}})
	if err == nil {
		t.Fatal("expected edit error")
	}
	if id != "" {
		t.Fatalf("expected empty id on edit error, got %q", id)
	}
	if called != 0 {
		t.Fatalf("expected no fallback send call, got %d", called)
	}
}

func TestDiscordSend_WithAttachments_RoutesCaptionsAndRemainingText(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	var sentFiles []struct {
		Content    string
		Attachment bus.OutboundAttachment
	}
	var sentMessages []string

	ch.sendFileFn = func(channelID, content string, attachment bus.OutboundAttachment) error {
		mu.Lock()
		sentFiles = append(sentFiles, struct {
			Content    string
			Attachment bus.OutboundAttachment
		}{
			Content:    content,
			Attachment: attachment,
		})
		mu.Unlock()
		return nil
	}
	ch.sendMessageFn = func(channelID string, msg bus.OutboundMessage) error {
		mu.Lock()
		sentMessages = append(sentMessages, msg.Content)
		mu.Unlock()
		return nil
	}

	content := strings.Repeat("x", discordMaxRunes+35)
	chunks := splitDiscordMessage(content, discordMaxRunes)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for setup, got %d", len(chunks))
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-attach",
		Content: content,
		Attachments: []bus.OutboundAttachment{
			{Path: "/tmp/a.docx", Filename: "a.docx"},
			{Path: "/tmp/b.pdf", Filename: "b.pdf"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	mu.Lock()
	gotFiles := append([]struct {
		Content    string
		Attachment bus.OutboundAttachment
	}(nil), sentFiles...)
	gotMessages := append([]string(nil), sentMessages...)
	mu.Unlock()

	if len(gotFiles) != 2 {
		t.Fatalf("expected 2 file sends, got %d", len(gotFiles))
	}
	if gotFiles[0].Content != chunks[0] {
		t.Fatalf("expected first attachment caption to equal first chunk")
	}
	if gotFiles[1].Content != "" {
		t.Fatalf("expected second attachment without caption, got %q", gotFiles[1].Content)
	}
	if len(gotMessages) != 1 {
		t.Fatalf("expected 1 trailing text message, got %d", len(gotMessages))
	}
	if gotMessages[0] != chunks[1] {
		t.Fatalf("expected trailing message to equal second chunk")
	}
}

func TestSendDiscordAttachment_ValidatesPathsAndSize(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: dirPath}); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory error, got %v", err)
	}

	largePath := filepath.Join(tmp, "large.bin")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatalf("create large file: %v", err)
	}
	if err := f.Truncate(discordMaxFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate large file: %v", err)
	}
	_ = f.Close()

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: largePath}); err == nil || !strings.Contains(err.Error(), "exceeds Discord limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	okPath := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(okPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: okPath}); err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected session nil error for valid file, got %v", err)
	}
}

// newTestSession returns a minimal discordgo.Session whose State.User is the bot.
func newTestSession(botID string) *discordgo.Session {
	s := &discordgo.Session{
		State: discordgo.NewState(),
	}
	s.State.User = &discordgo.User{ID: botID}
	return s
}

func TestProcessIncomingMessage_EditWithMention(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-123",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")

	msg := &discordgo.Message{
		ID:        "msg-1",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "hello <@bot-123> please help",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
		Mentions:  []*discordgo.User{{ID: "bot-123"}},
	}

	ch.processIncomingMessage(s, msg, true)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected message on bus for edit-with-mention, got nothing")
	}
	if got.Metadata["is_edit"] != "true" {
		t.Fatalf("expected is_edit=true, got %q", got.Metadata["is_edit"])
	}
	if strings.Contains(got.Content, "<@bot-123>") {
		t.Fatal("bot mention should be stripped from content")
	}
}

func TestProcessIncomingMessage_EditWithoutMention(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-123",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")

	msg := &discordgo.Message{
		ID:        "msg-2",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "just fixing a typo",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
	}

	ch.processIncomingMessage(s, msg, true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, ok := mb.ConsumeInbound(ctx)
	if ok {
		t.Fatal("edit without mention should not produce a bus message")
	}
}

func TestShouldDropDuplicateInbound_SamePayload(t *testing.T) {
	ch := &DiscordChannel{}
	if got := ch.shouldDropDuplicateInbound("msg-1", "hello  please help", nil, true, false); got {
		t.Fatal("first sighting should not be dropped")
	}
	if got := ch.shouldDropDuplicateInbound("msg-1", "hello  please help", nil, true, false); !got {
		t.Fatal("second identical sighting should be dropped")
	}
}

func TestProcessIncomingMessage_DedupesCreateThenIdenticalEdit(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel:   NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:           context.Background(),
		typing:        make(map[string]*typingState),
		typingEvery:   10 * time.Millisecond,
		botUserID:     "bot-123",
		recentInbound: make(map[string]inboundMessageFingerprint),
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")
	msg := &discordgo.Message{
		ID:        "msg-dup",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "hello <@bot-123> please help",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
		Mentions:  []*discordgo.User{{ID: "bot-123"}},
	}

	ch.processIncomingMessage(s, msg, false)
	ch.processIncomingMessage(s, msg, true)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("expected initial message on bus")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	if _, ok := mb.ConsumeInbound(ctx2); ok {
		t.Fatal("identical edit should have been deduped")
	}
}

func TestProcessIncomingMessage_EditAddingMentionStillProcesses(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel:   NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:           context.Background(),
		typing:        make(map[string]*typingState),
		typingEvery:   10 * time.Millisecond,
		botUserID:     "bot-123",
		recentInbound: make(map[string]inboundMessageFingerprint),
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")
	original := &discordgo.Message{
		ID:        "msg-edit-mention",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "please help",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
	}
	edited := &discordgo.Message{
		ID:        "msg-edit-mention",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "please help <@bot-123>",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
		Mentions:  []*discordgo.User{{ID: "bot-123"}},
	}

	ch.processIncomingMessage(s, original, false)
	ch.processIncomingMessage(s, edited, true)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("expected original message on bus")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	got, ok := mb.ConsumeInbound(ctx2)
	if !ok {
		t.Fatal("expected mention-adding edit on bus")
	}
	if got.Metadata["is_edit"] != "true" {
		t.Fatalf("expected is_edit=true, got %q", got.Metadata["is_edit"])
	}
}

func TestProcessIncomingMessage_EditWithChangedContentStillProcesses(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel:   NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:           context.Background(),
		typing:        make(map[string]*typingState),
		typingEvery:   10 * time.Millisecond,
		botUserID:     "bot-123",
		recentInbound: make(map[string]inboundMessageFingerprint),
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")
	original := &discordgo.Message{
		ID:        "msg-edit-change",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "hello <@bot-123> please help",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
		Mentions:  []*discordgo.User{{ID: "bot-123"}},
	}
	edited := &discordgo.Message{
		ID:        "msg-edit-change",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "hello <@bot-123> please help now",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
		Mentions:  []*discordgo.User{{ID: "bot-123"}},
	}

	ch.processIncomingMessage(s, original, false)
	ch.processIncomingMessage(s, edited, true)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, ok := mb.ConsumeInbound(ctx); !ok {
		t.Fatal("expected original message on bus")
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	got, ok := mb.ConsumeInbound(ctx2)
	if !ok {
		t.Fatal("expected changed edit on bus")
	}
	if !strings.Contains(got.Content, "please help now") {
		t.Fatalf("expected updated content in edited message, got %q", got.Content)
	}
}

func TestProcessIncomingMessage_EditNilAuthor(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-123",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")

	// Embed unfurl: Author is nil
	msg := &discordgo.Message{
		ID:        "msg-3",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "",
		Author:    nil,
	}

	ch.processIncomingMessage(s, msg, true)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, ok := mb.ConsumeInbound(ctx)
	if ok {
		t.Fatal("edit with nil author should not produce a bus message")
	}
}

func TestProcessIncomingMessage_NewMessageWithoutMention(t *testing.T) {
	mb := bus.NewMessageBus()
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, mb, nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
		botUserID:   "bot-123",
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.setRunning(true)

	s := newTestSession("bot-123")

	// New messages (not edits) should still be published even without a mention.
	msg := &discordgo.Message{
		ID:        "msg-4",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Content:   "hello everyone",
		Author:    &discordgo.User{ID: "user-1", Username: "tester"},
	}

	ch.processIncomingMessage(s, msg, false)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("new message without mention should still reach bus")
	}
	if got.Metadata["is_edit"] != "false" {
		t.Fatalf("expected is_edit=false, got %q", got.Metadata["is_edit"])
	}
}
