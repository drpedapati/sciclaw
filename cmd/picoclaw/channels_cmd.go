package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/mymmrac/telego"

	channelspkg "github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
)

func channelsCmd() {
	if len(os.Args) < 3 {
		channelsHelp()
		return
	}

	switch os.Args[2] {
	case "list":
		channelsListCmd()
	case "list-rooms":
		channelsListRoomsCmd()
	case "pair-telegram":
		channelsPairTelegramCmd()
	case "setup":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s channels setup <telegram|discord>\n", invokedCLIName())
			return
		}
		channelsSetupCmd(os.Args[3])
	default:
		channelsHelp()
	}
}

func channelsHelp() {
	commandName := invokedCLIName()
	fmt.Println("\nChannels:")
	fmt.Printf("  %s channels list\n", commandName)
	fmt.Printf("  %s channels list-rooms --channel discord\n", commandName)
	fmt.Printf("  %s channels pair-telegram [--timeout 15]\n", commandName)
	fmt.Printf("  %s channels setup telegram\n", commandName)
	fmt.Printf("  %s channels setup discord\n", commandName)
	fmt.Println()
	fmt.Println("After setup, run:")
	fmt.Printf("  %s gateway\n", commandName)
}

func channelsListCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Chat channels:")
	fmt.Printf("  Telegram: enabled=%t token=%t allow_from=%d\n",
		cfg.Channels.Telegram.Enabled,
		strings.TrimSpace(cfg.Channels.Telegram.Token) != "",
		len(cfg.Channels.Telegram.AllowFrom),
	)
	fmt.Printf("  Discord:  enabled=%t token=%t allow_from=%d\n",
		cfg.Channels.Discord.Enabled,
		strings.TrimSpace(cfg.Channels.Discord.Token) != "",
		len(cfg.Channels.Discord.AllowFrom),
	)

	fmt.Println("\nSetup:")
	fmt.Printf("  %s channels setup telegram\n", invokedCLIName())
	fmt.Printf("  %s channels setup discord\n", invokedCLIName())
}

func channelsSetupCmd(which string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	configPath := getConfigPath()
	r := bufio.NewReader(os.Stdin)

	switch strings.ToLower(which) {
	case "telegram":
		if err := setupTelegram(r, cfg, configPath); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "discord":
		if err := setupDiscord(r, cfg, configPath); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown channel: %s\n", which)
		fmt.Printf("Usage: %s channels setup <telegram|discord>\n", invokedCLIName())
		os.Exit(2)
	}

	fmt.Println("\nNext:")
	fmt.Printf("  %s gateway\n", invokedCLIName())
}

func runChannelsWizard(r *bufio.Reader, cfg *config.Config, configPath string) {
	fmt.Println()
	fmt.Println("Messaging apps:")
	doTelegram := promptYesNo(r, "  Setup Telegram?", true)
	doDiscord := promptYesNo(r, "  Setup Discord?", false)

	if doTelegram {
		if err := setupTelegram(r, cfg, configPath); err != nil {
			fmt.Printf("  Telegram setup failed: %v\n", err)
		}
	}
	if doDiscord {
		if err := setupDiscord(r, cfg, configPath); err != nil {
			fmt.Printf("  Discord setup failed: %v\n", err)
		}
	}
}

type telegramPairing struct {
	UserID   int64
	Username string
	ChatID   int64
	ChatType string
}

func setupTelegram(r *bufio.Reader, cfg *config.Config, configPath string) error {
	fmt.Println()
	fmt.Println("Telegram setup:")

	token := channelspkg.NormalizeDiscordBotToken(promptLine(r, "Paste bot token:"))
	if token == "" {
		return fmt.Errorf("token is required")
	}
	proxy := strings.TrimSpace(promptLine(r, "Proxy URL (optional, leave blank for none):"))

	// Validate token early and capture bot username for better UX.
	bot, err := newTelegramBot(token, proxy)
	if err != nil {
		return err
	}
	fmt.Printf("  Bot: @%s\n", bot.Username())

	// Save token/proxy immediately.
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = token
	cfg.Channels.Telegram.Proxy = proxy
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	fmt.Println("Pairing:")
	fmt.Println("  1. Open Telegram and message your bot anything (e.g., \"hi\").")
	fmt.Println("  2. Wait here for sciClaw to detect your user ID.")

	p, err := telegramPairOnce(bot, 60*time.Second)
	if err != nil {
		fmt.Printf("  Pairing skipped: %v\n", err)
		fmt.Println("  Fallback: add your user ID manually in ~/.picoclaw/config.json -> channels.telegram.allow_from")
		return nil
	}

	label := fmt.Sprintf("%d", p.UserID)
	if p.Username != "" {
		label = fmt.Sprintf("%d|%s", p.UserID, p.Username)
	}
	fmt.Printf("  Detected: user=%s chat_id=%d chat_type=%s\n", label, p.ChatID, p.ChatType)

	if promptYesNo(r, "Add this user to allow_from (recommended)?", true) {
		cfg.Channels.Telegram.AllowFrom = appendUniqueFlexible(cfg.Channels.Telegram.AllowFrom, label)
		if err := config.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("saving allow_from: %w", err)
		}
		fmt.Println("  Saved allow_from.")
	}

	// Best-effort: send confirmation message.
	_ = sendTelegramTestMessage(bot, p.ChatID, "sciClaw connected. You can start chatting here.")
	return nil
}

func setupDiscord(r *bufio.Reader, cfg *config.Config, configPath string) error {
	fmt.Println()
	fmt.Println("Discord setup:")

	fmt.Printf("  Help: %s\n", docsLink("#discord"))
	fmt.Println("  Security: allowlist is required (prevents the bot from responding to everyone).")

	fmt.Println()
	fmt.Println("Allowlist (required):")
	fmt.Println("  Paste your Discord user ID(s) (comma-separated). Type 'help' for instructions.")

	var allow []string
	for {
		raw := strings.TrimSpace(promptLine(r, "User IDs:"))
		if strings.EqualFold(raw, "help") {
			fmt.Println("  How to find your User ID:")
			fmt.Println("    1. Discord Settings -> Advanced -> Developer Mode (ON)")
			fmt.Println("    2. Right-click your avatar -> Copy User ID")
			continue
		}
		allow = parseCSV(raw)
		if len(allow) > 0 {
			break
		}
		fmt.Println("  Missing user IDs. This is a security requirement.")
		if !promptYesNo(r, "Try again?", true) {
			return fmt.Errorf("aborted: allowlist is required")
		}
	}

	fmt.Println()
	fmt.Println("Bot token:")
	fmt.Println("  Paste your bot token (it will be saved; never printed back).")

	token := strings.TrimSpace(promptLine(r, "Paste bot token:"))
	if token == "" {
		return fmt.Errorf("token is required")
	}

	fmt.Println()
	fmt.Println("Review:")
	fmt.Printf("  Enabled:   true\n")
	fmt.Printf("  Allowlist: %s\n", strings.Join(allow, ", "))
	if !promptYesNo(r, "Save these Discord settings now?", true) {
		return fmt.Errorf("aborted")
	}

	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.Token = token
	cfg.Channels.Discord.AllowFrom = config.FlexibleStringSlice(allow)
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println()
	fmt.Printf("  Saved Discord settings to %s\n", configPath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. In the Discord Developer Portal, enable MESSAGE CONTENT INTENT for the bot.")
	fmt.Println("  2. Invite the bot to your server with proper scopes/permissions.")
	fmt.Printf("  3. Start sciClaw: %s gateway\n", invokedCLIName())
	fmt.Printf("  Help: %s\n", docsLink("#discord"))
	return nil
}

func parseCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func appendUniqueFlexible(list config.FlexibleStringSlice, v string) config.FlexibleStringSlice {
	for _, x := range list {
		if strings.TrimSpace(x) == strings.TrimSpace(v) {
			return list
		}
	}
	return append(list, v)
}

func newTelegramBot(token, proxy string) (*telego.Bot, error) {
	var opts []telego.BotOption
	// Suppress telego's internal logger so it doesn't pollute the interactive wizard.
	opts = append(opts, telego.WithDiscardLogger())
	if strings.TrimSpace(proxy) != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL: %w", err)
		}
		opts = append(opts, telego.WithHTTPClient(&http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(u)},
		}))
	}
	bot, err := telego.NewBot(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create telegram bot: %w", err)
	}
	return bot, nil
}

func telegramPairOnce(bot *telego.Bot, timeout time.Duration) (*telegramPairing, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Disable retries so the long-polling goroutine exits immediately on
	// context deadline instead of sleeping 8s and retrying (which spews
	// background errors into the interactive terminal).
	updates, err := bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{Timeout: 30},
		telego.WithLongPollingRetryTimeout(0),
	)
	if err != nil {
		return nil, fmt.Errorf("long polling failed: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("no message received within %s", timeout)
		case u, ok := <-updates:
			if !ok {
				return nil, fmt.Errorf("updates channel closed")
			}
			if u.Message == nil || u.Message.From == nil {
				continue
			}
			return &telegramPairing{
				UserID:   u.Message.From.ID,
				Username: u.Message.From.Username,
				ChatID:   u.Message.Chat.ID,
				ChatType: string(u.Message.Chat.Type),
			}, nil
		}
	}
}

func sendTelegramTestMessage(bot *telego.Bot, chatID int64, text string) error {
	_, err := bot.SendMessage(context.Background(), &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text:   text,
	})
	return err
}

// channelsListRoomsCmd lists servers and text channels for a configured bot.
// Usage: sciclaw channels list-rooms --channel discord
// Output: one line per channel: channel_id|guild_name|#channel_name
func channelsListRoomsCmd() {
	channel := ""
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--channel" && i+1 < len(args) {
			channel = args[i+1]
			i++
		}
	}

	switch strings.ToLower(channel) {
	case "discord":
		listDiscordRooms()
	default:
		fmt.Fprintf(os.Stderr, "Usage: %s channels list-rooms --channel discord\n", invokedCLIName())
		os.Exit(2)
	}
}

func listDiscordRooms() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	token := channelspkg.NormalizeDiscordBotToken(cfg.Channels.Discord.Token)
	if token == "" {
		fmt.Fprintf(os.Stderr, "No Discord bot token configured\n")
		os.Exit(1)
	}

	session, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Discord session: %v\n", err)
		os.Exit(1)
	}

	guilds, err := session.UserGuilds(200, "", "", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list guilds: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: verify channels.discord.token is a valid bot token (no leading \"Bot \").\n")
		os.Exit(1)
	}

	for _, g := range guilds {
		channels, err := session.GuildChannels(g.ID)
		if err != nil {
			continue
		}
		for _, ch := range channels {
			if ch.Type != discordgo.ChannelTypeGuildText {
				continue
			}
			fmt.Printf("%s|%s|#%s\n", ch.ID, g.Name, ch.Name)
		}
	}
}

// channelsPairTelegramCmd listens for a Telegram message to detect chat ID.
// Usage: sciclaw channels pair-telegram [--timeout 15]
// Output on success: chat_id|chat_type|username
func channelsPairTelegramCmd() {
	timeout := 15
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--timeout" && i+1 < len(args) {
			if v, err := strconv.Atoi(args[i+1]); err == nil && v > 0 {
				timeout = v
			}
			i++
		}
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	token := strings.TrimSpace(cfg.Channels.Telegram.Token)
	if token == "" {
		fmt.Fprintf(os.Stderr, "No Telegram bot token configured\n")
		os.Exit(1)
	}

	bot, err := newTelegramBot(token, cfg.Channels.Telegram.Proxy)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create Telegram bot: %v\n", err)
		os.Exit(1)
	}

	p, err := telegramPairOnce(bot, time.Duration(timeout)*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%d|%s|%s\n", p.ChatID, p.ChatType, p.Username)
}
