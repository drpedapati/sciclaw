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

	"github.com/sipeed/picoclaw/pkg/bus"
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
	case "test":
		channelsTestCmd()
	case "list-rooms":
		channelsListRoomsCmd()
	case "pair-telegram":
		channelsPairTelegramCmd()
	case "setup":
		if len(os.Args) < 4 {
			fmt.Printf("Usage: %s channels setup <telegram|discord|email>\n", invokedCLIName())
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
	fmt.Printf("  %s channels setup email\n", commandName)
	fmt.Printf("  %s channels test email --to recipient@example.com\n", commandName)
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
	fmt.Printf("  Email:    enabled=%t address=%t provider=%s receive=%t\n",
		cfg.Channels.Email.Enabled,
		strings.TrimSpace(cfg.Channels.Email.Address) != "",
		strings.TrimSpace(cfg.Channels.Email.Provider),
		cfg.Channels.Email.ReceiveEnabled,
	)

	fmt.Println("\nSetup:")
	fmt.Printf("  %s channels setup telegram\n", invokedCLIName())
	fmt.Printf("  %s channels setup discord\n", invokedCLIName())
	fmt.Printf("  %s channels setup email\n", invokedCLIName())
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
	case "email":
		if err := setupEmail(r, cfg, configPath); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown channel: %s\n", which)
		fmt.Printf("Usage: %s channels setup <telegram|discord|email>\n", invokedCLIName())
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
	doEmail := promptYesNo(r, "  Setup Email (send-only)?", false)

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
	if doEmail {
		if err := setupEmail(r, cfg, configPath); err != nil {
			fmt.Printf("  Email setup failed: %v\n", err)
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

	var bot *telego.Bot
	var token, proxy string
	for {
		token = cleanToken(promptLine(r, "Paste bot token:"))
		if token == "" {
			return fmt.Errorf("token is required")
		}
		proxy = strings.TrimSpace(promptLine(r, "Proxy URL (optional, leave blank for none):"))

		// Validate token early and capture bot username for better UX.
		var err error
		bot, err = newTelegramBot(token, proxy)
		if err == nil {
			break
		}
		fmt.Printf("  Error: %v\n", err)
		if !promptYesNo(r, "Try again?", true) {
			return fmt.Errorf("aborted")
		}
	}
	fmt.Printf("  Bot: @%s\n", bot.Username())

	// Save token/proxy immediately.
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.Token = token
	cfg.Channels.Telegram.Proxy = proxy
	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("  Saved. Telegram is ready.")

	// Group privacy — Telegram enables this by default, which prevents
	// the bot from seeing messages in group chats.
	fmt.Println()
	fmt.Println("Group chats:")
	fmt.Println("  By default Telegram blocks bots from reading group messages.")
	fmt.Println("  To use the bot in group chats, disable privacy mode:")
	fmt.Println("    1. Open Telegram and search for @BotFather")
	fmt.Println("    2. Send /mybots → select your bot → Bot Settings → Group Privacy")
	fmt.Println("    3. Tap \"Turn off\"")
	fmt.Println("  (Skip this if you only use the bot in DMs.)")

	// Allowlist — same approach as Discord: paste user IDs directly.
	// Empty allowlist means everyone can message the bot.
	fmt.Println()
	fmt.Println("Allowlist (optional):")
	fmt.Println("  Restrict who can message the bot. Leave blank to allow everyone.")
	fmt.Println("  To find your Telegram user ID:")
	fmt.Println("    1. Open Telegram and search for @userinfobot")
	fmt.Println("    2. Send it any message — it replies with your user ID")
	raw := strings.TrimSpace(promptLine(r, "Telegram user ID(s) (comma-separated, or blank to skip):"))
	if raw != "" {
		ids := parseCSV(raw)
		for _, id := range ids {
			cfg.Channels.Telegram.AllowFrom = appendUniqueFlexible(cfg.Channels.Telegram.AllowFrom, id)
		}
		if err := config.SaveConfig(configPath, cfg); err != nil {
			return fmt.Errorf("saving allow_from: %w", err)
		}
		fmt.Printf("  Saved %d user(s) to allowlist.\n", len(ids))
	}
	return nil
}

func setupDiscord(r *bufio.Reader, cfg *config.Config, configPath string) error {
	fmt.Println()
	fmt.Println("Discord setup:")

	fmt.Printf("  Help: %s\n", docsLink("#discord"))
	fmt.Println("  Security: allowlist is required (prevents the bot from responding to everyone).")

	fmt.Println()
	fmt.Println("Allowlist (required):")
	fmt.Println("  To find your Discord user ID:")
	fmt.Println("    1. Discord Settings -> Advanced -> Developer Mode (ON)")
	fmt.Println("    2. Right-click your avatar -> Copy User ID")
	fmt.Println("  Paste user ID(s) comma-separated. Type 'help' to see these instructions again.")

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

	token := cleanToken(promptLine(r, "Paste bot token:"))
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
	fmt.Println("  1. In the Discord Developer Portal → Bot → enable MESSAGE CONTENT INTENT.")
	fmt.Println("  2. Invite the bot to your server (OAuth2 → URL Generator → scopes: bot).")
	fmt.Println()
	fmt.Println("  Bot Permissions (keep minimal):")
	fmt.Println("    Enable:  View Channels, Send Messages, Send Messages in Threads,")
	fmt.Println("             Embed Links, Attach Files, Read Message History,")
	fmt.Println("             Add Reactions, Use Slash Commands")
	fmt.Println("    Do NOT enable: Administrator, Manage Roles/Channels,")
	fmt.Println("             Kick/Ban/Moderate, Manage Webhooks, Mention Everyone")
	fmt.Println()
	fmt.Printf("  3. Start sciClaw: %s gateway\n", invokedCLIName())
	fmt.Printf("  Help: %s\n", docsLink("#discord"))
	return nil
}

func channelsTestCmd() {
	if len(os.Args) < 4 {
		fmt.Printf("Usage: %s channels test email --to recipient@example.com [--subject \"...\"]\n", invokedCLIName())
		os.Exit(2)
	}

	switch strings.ToLower(os.Args[3]) {
	case "email":
		channelsTestEmailCmd(os.Args[4:])
	default:
		fmt.Printf("Unknown channel: %s\n", os.Args[3])
		os.Exit(2)
	}
}

func channelsTestEmailCmd(args []string) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}
	if !cfg.Channels.Email.Enabled {
		fmt.Printf("Email is disabled. Run: %s channels setup email\n", invokedCLIName())
		os.Exit(1)
	}
	if strings.TrimSpace(cfg.Channels.Email.APIKey) == "" || strings.TrimSpace(cfg.Channels.Email.Address) == "" {
		fmt.Printf("Email is not fully configured. Run: %s channels setup email\n", invokedCLIName())
		os.Exit(1)
	}

	to := ""
	subject := "sciClaw test email"
	body := "This is a test email sent from sciClaw."
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--to":
			i++
			if i < len(args) {
				to = strings.TrimSpace(args[i])
			}
		case "--subject":
			i++
			if i < len(args) {
				subject = strings.TrimSpace(args[i])
			}
		case "--body":
			i++
			if i < len(args) {
				body = args[i]
			}
		default:
			fmt.Printf("Unknown option: %s\n", args[i])
			os.Exit(2)
		}
	}
	if to == "" {
		to = strings.TrimSpace(cfg.Channels.Email.Address)
	}

	msg := bus.OutboundMessage{
		Channel: "email",
		ChatID:  to,
		Subject: subject,
		Content: body,
	}
	if err := channelspkg.SendEmailMessage(context.Background(), nil, cfg.Channels.Email, msg); err != nil {
		fmt.Printf("Error sending test email: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Test email sent to %s\n", to)
}

func setupEmail(r *bufio.Reader, cfg *config.Config, configPath string) error {
	fmt.Println()
	fmt.Println("Email setup:")
	fmt.Println("  This build supports outbound email through Resend.")
	fmt.Println("  Inbound receive is not active yet.")

	apiKey := strings.TrimSpace(promptLine(r, "Resend API key:"))
	if apiKey == "" {
		return fmt.Errorf("api key is required")
	}
	address := strings.TrimSpace(promptLine(r, "From address (for example support@example.com):"))
	if address == "" {
		return fmt.Errorf("address is required")
	}
	displayName := strings.TrimSpace(promptLine(r, "Display name (optional, default sciClaw):"))
	if displayName == "" {
		displayName = "sciClaw"
	}
	baseURL := strings.TrimSpace(promptLine(r, "Custom Resend API URL (optional, leave blank for default):"))
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}

	cfg.Channels.Email.Enabled = true
	cfg.Channels.Email.Provider = "resend"
	cfg.Channels.Email.APIKey = apiKey
	cfg.Channels.Email.Address = address
	cfg.Channels.Email.DisplayName = displayName
	cfg.Channels.Email.BaseURL = baseURL
	cfg.Channels.Email.ReceiveEnabled = false
	cfg.Channels.Email.ReceiveMode = "poll"
	cfg.Channels.Email.PollIntervalSeconds = 30

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Println("  Saved. Email send-only is ready.")
	fmt.Printf("  Test it with: %s channels test email --to %s\n", invokedCLIName(), address)
	return nil
}

// cleanToken strips trailing backslashes, quotes, and whitespace from pasted tokens.
func cleanToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`+"`")
	s = strings.TrimRight(s, `\`)
	return strings.TrimSpace(s)
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

	// Quick connectivity check — flush any stale updates.
	if _, err := bot.GetUpdates(ctx, &telego.GetUpdatesParams{Timeout: 0}); err != nil {
		return nil, fmt.Errorf("Telegram API unreachable: %w", err)
	}

	updates, err := bot.UpdatesViaLongPolling(ctx, &telego.GetUpdatesParams{Timeout: 10})
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
