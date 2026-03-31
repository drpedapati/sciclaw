// One-shot Discord server scaffolding tool.
// Reads the bot token from sciClaw config.json, then creates categories,
// channels, roles, and permissions on the target guild.
//
// Usage:
//   go run ./cmd/discord-setup --guild 1488473301889191977
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/bwmarrin/discordgo"
)

// ---------- config.json token reader ----------

type discordCfg struct {
	Channels struct {
		Discord struct {
			Token string `json:"token"`
		} `json:"discord"`
	} `json:"channels"`
}

func readToken() string {
	home, _ := os.UserHomeDir()
	cfgPath := filepath.Join(home, "sciclaw", "config.json")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		// Fall back to legacy path
		cfgPath = filepath.Join(home, ".picoclaw", "config.json")
		data, err = os.ReadFile(cfgPath)
		if err != nil {
			log.Fatalf("cannot read config.json from ~/sciclaw or ~/.picoclaw: %v", err)
		}
	}
	var c discordCfg
	if err := json.Unmarshal(data, &c); err != nil {
		log.Fatalf("cannot parse config: %v", err)
	}
	if c.Channels.Discord.Token == "" {
		log.Fatal("channels.discord.token is empty in config.json")
	}
	return c.Channels.Discord.Token
}

// ---------- color constants ----------

const (
	colorPurple = 0x7B2D8E
	colorGreen  = 0x2ECC71
	colorGray   = 0x95A5A6
)

// ---------- channel definitions ----------

type channelDef struct {
	Name  string
	Topic string
	Type  discordgo.ChannelType // defaults to GuildText (0)
}

type categoryDef struct {
	Name     string
	Channels []channelDef
}

var categories = []categoryDef{
	{
		Name: "📌 INFORMATION",
		Channels: []channelDef{
			{Name: "welcome", Topic: "Start here — what sciClaw is, how to get involved, and useful links"},
			{Name: "rules", Topic: "Community guidelines and code of conduct"},
			{Name: "announcements", Topic: "Releases, breaking changes, and project updates"},
		},
	},
	{
		Name: "💬 COMMUNITY",
		Channels: []channelDef{
			{Name: "general", Topic: "Chat about anything sciClaw-related"},
			{Name: "support", Topic: "Get help with installation, configuration, and troubleshooting", Type: discordgo.ChannelTypeGuildForum},
			{Name: "show-and-tell", Topic: "Share your sciClaw setup, workflows, agents, and screenshots"},
		},
	},
	{
		Name: "🔧 DEVELOPMENT",
		Channels: []channelDef{
			{Name: "dev", Topic: "Architecture, design decisions, and contributor discussion"},
			{Name: "bugs", Topic: "Report and triage bugs", Type: discordgo.ChannelTypeGuildForum},
			{Name: "pull-requests", Topic: "Automated feed from github.com/drpedapati/sciclaw"},
		},
	},
	{
		Name: "🔇 META",
		Channels: []channelDef{
			{Name: "bot-commands", Topic: "Test bot commands here to keep other channels clean"},
		},
	},
}

// ---------- role definitions ----------

type roleDef struct {
	Name  string
	Color int
	Perms int64
	Hoist bool
}

var roles = []roleDef{
	{Name: "Maintainer", Color: colorPurple, Hoist: true, Perms: discordgo.PermissionAdministrator},
	{Name: "Contributor", Color: colorGreen, Hoist: true, Perms: discordgo.PermissionSendMessages | discordgo.PermissionViewChannel | discordgo.PermissionAttachFiles | discordgo.PermissionEmbedLinks | discordgo.PermissionAddReactions | discordgo.PermissionUseSlashCommands | discordgo.PermissionCreatePublicThreads},
	{Name: "User", Color: colorGray, Hoist: false, Perms: discordgo.PermissionSendMessages | discordgo.PermissionViewChannel | discordgo.PermissionAddReactions},
}

// ---------- forum tags ----------

var supportTags = []string{"gateway", "discord", "slack", "telegram", "cli", "web-ui", "config", "auth", "agents", "skills", "resolved", "needs-info"}
var bugTags = []string{"confirmed", "needs-repro", "gateway", "discord", "slack", "web-ui", "agents", "p0-critical", "p1-high", "p2-medium", "fixed"}

// ---------- main ----------

func main() {
	guildID := flag.String("guild", "", "Discord guild (server) ID")
	dryRun := flag.Bool("dry-run", false, "Print plan without making changes")
	flag.Parse()

	if *guildID == "" {
		log.Fatal("--guild is required")
	}

	token := readToken()
	if *dryRun {
		printPlan(*guildID)
		return
	}

	s, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Fatalf("session: %v", err)
	}
	if err := s.Open(); err != nil {
		log.Fatalf("open: %v", err)
	}
	defer s.Close()

	// Verify we can see the guild
	guild, err := s.Guild(*guildID)
	if err != nil {
		log.Fatalf("cannot access guild %s: %v", *guildID, err)
	}
	fmt.Printf("Connected to server: %s (%s)\n\n", guild.Name, guild.ID)

	// Create roles
	fmt.Println("Creating roles...")
	createdRoles := map[string]string{} // name -> ID
	for _, r := range roles {
		role, err := s.GuildRoleCreate(*guildID, &discordgo.RoleParams{
			Name:        r.Name,
			Color:       &r.Color,
			Hoist:       &r.Hoist,
			Permissions: &r.Perms,
		})
		if err != nil {
			log.Printf("  ✗ role %s: %v", r.Name, err)
			continue
		}
		createdRoles[r.Name] = role.ID
		fmt.Printf("  ✓ @%s (%s)\n", role.Name, role.ID)
	}

	// Find @everyone role ID
	everyoneRoleID := *guildID // @everyone role ID == guild ID

	// Create categories + channels
	fmt.Println("\nCreating channels...")
	readOnlyChannels := map[string]bool{"welcome": true, "rules": true, "announcements": true}

	for _, cat := range categories {
		catCh, err := s.GuildChannelCreateComplex(*guildID, discordgo.GuildChannelCreateData{
			Name: cat.Name,
			Type: discordgo.ChannelTypeGuildCategory,
		})
		if err != nil {
			log.Printf("  ✗ category %s: %v", cat.Name, err)
			continue
		}
		fmt.Printf("  ✓ Category: %s\n", cat.Name)

		for _, ch := range cat.Channels {
			chType := ch.Type
			if chType == 0 {
				chType = discordgo.ChannelTypeGuildText
			}

			data := discordgo.GuildChannelCreateData{
				Name:     ch.Name,
				Type:     chType,
				Topic:    ch.Topic,
				ParentID: catCh.ID,
			}

			// Read-only channels: deny SendMessages for @everyone
			if readOnlyChannels[ch.Name] {
				deny := int64(discordgo.PermissionSendMessages)
				data.PermissionOverwrites = []*discordgo.PermissionOverwrite{
					{
						ID:   everyoneRoleID,
						Type: discordgo.PermissionOverwriteTypeRole,
						Deny: deny,
					},
				}
			}

			// Slow mode on #general
			if ch.Name == "general" {
				rate := 5
				data.RateLimitPerUser = rate
			}

			created, err := s.GuildChannelCreateComplex(*guildID, data)
			if err != nil {
				log.Printf("    ✗ #%s: %v", ch.Name, err)
				continue
			}
			fmt.Printf("    ✓ #%s (%s)\n", ch.Name, created.ID)

			// Add forum tags
			if chType == discordgo.ChannelTypeGuildForum {
				tags := forumTagsFor(ch.Name)
				if len(tags) > 0 {
					forumTags := make([]discordgo.ForumTag, len(tags))
					for i, t := range tags {
						forumTags[i] = discordgo.ForumTag{Name: t}
					}
					_, err := s.ChannelEdit(created.ID, &discordgo.ChannelEdit{
						AvailableTags: &forumTags,
					})
					if err != nil {
						log.Printf("      ✗ tags for #%s: %v", ch.Name, err)
					} else {
						fmt.Printf("      ✓ %d tags added\n", len(tags))
					}
				}
			}
		}

		// Brief pause between categories to avoid rate limits
		time.Sleep(500 * time.Millisecond)
	}

	// Create a permanent invite on #general
	fmt.Println("\nCreating invite...")
	channels, _ := s.GuildChannels(*guildID)
	for _, ch := range channels {
		if ch.Name == "general" && ch.Type == discordgo.ChannelTypeGuildText {
			invite, err := s.ChannelInviteCreate(ch.ID, discordgo.Invite{
				MaxAge:  0, // never expires
				MaxUses: 0, // unlimited
			})
			if err != nil {
				log.Printf("  ✗ invite: %v", err)
			} else {
				fmt.Printf("  ✓ https://discord.gg/%s\n", invite.Code)
			}
			break
		}
	}

	fmt.Println("\nDone! Server is ready.")
}

func forumTagsFor(name string) []string {
	switch name {
	case "support":
		return supportTags
	case "bugs":
		return bugTags
	default:
		return nil
	}
}

func printPlan(guildID string) {
	fmt.Printf("DRY RUN — would set up guild %s:\n\n", guildID)
	fmt.Println("Roles:")
	for _, r := range roles {
		fmt.Printf("  @%s (color: #%06X, hoist: %v)\n", r.Name, r.Color, r.Hoist)
	}
	fmt.Println("\nChannels:")
	for _, cat := range categories {
		fmt.Printf("  %s\n", cat.Name)
		for _, ch := range cat.Channels {
			typ := "text"
			if ch.Type == discordgo.ChannelTypeGuildForum {
				typ = "forum"
			}
			fmt.Printf("    #%-20s [%s] %s\n", ch.Name, typ, ch.Topic)
		}
	}
}
