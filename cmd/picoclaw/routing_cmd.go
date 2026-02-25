package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/routing"
)

func routingCmd() {
	if len(os.Args) < 3 {
		routingHelp()
		return
	}

	sub := strings.ToLower(strings.TrimSpace(os.Args[2]))
	switch sub {
	case "status":
		routingStatusCmd()
	case "list":
		routingListCmd()
	case "add":
		routingAddCmd()
	case "remove":
		routingRemoveCmd()
	case "set-users":
		routingSetUsersCmd()
	case "validate":
		routingValidateCmd()
	case "explain":
		routingExplainCmd()
	case "enable":
		routingSetEnabledCmd(true)
	case "disable":
		routingSetEnabledCmd(false)
	case "export":
		routingExportCmd()
	case "import":
		routingImportCmd()
	case "reload":
		routingReloadCmd()
	case "help", "-h", "--help":
		routingHelp()
	default:
		fmt.Printf("Unknown routing command: %s\n", sub)
		routingHelp()
	}
}

func routingHelp() {
	commandName := invokedCLIName()
	fmt.Printf("\nRouting commands:\n")
	fmt.Printf("  %s routing status\n", commandName)
	fmt.Printf("  %s routing list\n", commandName)
	fmt.Printf("  %s routing add --channel <channel> --chat-id <id> --workspace <abs_path> --allow <id1,id2> [--label <name>] [--no-mention]\n", commandName)
	fmt.Printf("  %s routing remove --channel <channel> --chat-id <id>\n", commandName)
	fmt.Printf("  %s routing set-users --channel <channel> --chat-id <id> --allow <id1,id2>\n", commandName)
	fmt.Printf("  %s routing validate\n", commandName)
	fmt.Printf("  %s routing explain --channel <channel> --chat-id <id> --sender <id> [--mention] [--dm]\n", commandName)
	fmt.Printf("  %s routing enable|disable\n", commandName)
	fmt.Printf("  %s routing export --out <file>\n", commandName)
	fmt.Printf("  %s routing import --in <file> [--replace]\n", commandName)
	fmt.Printf("  %s routing reload\n", commandName)
}

func routingStatusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	invalid := 0
	for _, m := range cfg.Routing.Mappings {
		tmp := config.RoutingConfig{
			Enabled:          cfg.Routing.Enabled,
			UnmappedBehavior: cfg.Routing.UnmappedBehavior,
			Mappings:         []config.RoutingMapping{m},
		}
		if err := config.ValidateRoutingConfig(tmp); err != nil {
			invalid++
		}
	}

	fmt.Printf("Routing enabled: %t\n", cfg.Routing.Enabled)
	fmt.Printf("Unmapped behavior: %s\n", cfg.Routing.UnmappedBehavior)
	fmt.Printf("Mappings: %d\n", len(cfg.Routing.Mappings))
	fmt.Printf("Invalid mappings: %d\n", invalid)

	if err := config.ValidateRoutingConfig(cfg.Routing); err != nil {
		fmt.Printf("Validation: failed (%v)\n", err)
	} else {
		fmt.Println("Validation: ok")
	}
}

func routingListCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	if len(cfg.Routing.Mappings) == 0 {
		fmt.Println("No routing mappings configured.")
		return
	}

	mappings := append([]config.RoutingMapping(nil), cfg.Routing.Mappings...)
	sort.Slice(mappings, func(i, j int) bool {
		ki := strings.ToLower(mappings[i].Channel) + ":" + mappings[i].ChatID
		kj := strings.ToLower(mappings[j].Channel) + ":" + mappings[j].ChatID
		return ki < kj
	})

	fmt.Printf("Routing mappings (%d):\n", len(mappings))
	for _, m := range mappings {
		label := m.Label
		if strings.TrimSpace(label) == "" {
			label = "-"
		}
		fmt.Printf("- %s %s\n", m.Channel, m.ChatID)
		fmt.Printf("  workspace: %s\n", m.Workspace)
		fmt.Printf("  allowed_senders: %s\n", strings.Join(m.AllowedSenders, ","))
		fmt.Printf("  label: %s\n", label)
	}
}

func routingAddCmd() {
	channel, chatID, workspace, allowCSV, label := "", "", "", "", ""
	noMention := false
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--chat-id":
			if i+1 < len(args) {
				chatID = args[i+1]
				i++
			}
		case "--workspace":
			if i+1 < len(args) {
				workspace = args[i+1]
				i++
			}
		case "--allow":
			if i+1 < len(args) {
				allowCSV = args[i+1]
				i++
			}
		case "--label":
			if i+1 < len(args) {
				label = args[i+1]
				i++
			}
		case "--no-mention":
			noMention = true
		}
	}

	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" || strings.TrimSpace(workspace) == "" || strings.TrimSpace(allowCSV) == "" {
		fmt.Println("Usage: routing add --channel <channel> --chat-id <id> --workspace <abs_path> --allow <id1,id2> [--label <name>] [--no-mention]")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	allowed := parseAllowListCSV(allowCSV)
	if len(allowed) == 0 {
		fmt.Println("Error: --allow must include at least one sender id")
		return
	}

	m := config.RoutingMapping{
		Channel:        channel,
		ChatID:         chatID,
		Workspace:      workspace,
		AllowedSenders: allowed,
		Label:          label,
	}
	if noMention {
		f := false
		m.RequireMention = &f
	}

	key := routingMappingKey(channel, chatID)
	updated := false
	for i := range cfg.Routing.Mappings {
		if routingMappingKey(cfg.Routing.Mappings[i].Channel, cfg.Routing.Mappings[i].ChatID) == key {
			cfg.Routing.Mappings[i] = m
			updated = true
			break
		}
	}
	if !updated {
		cfg.Routing.Mappings = append(cfg.Routing.Mappings, m)
	}

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}

	if updated {
		fmt.Printf("Updated routing mapping for %s:%s\n", channel, chatID)
	} else {
		fmt.Printf("Added routing mapping for %s:%s\n", channel, chatID)
	}
}

func routingRemoveCmd() {
	channel, chatID := "", ""
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--chat-id":
			if i+1 < len(args) {
				chatID = args[i+1]
				i++
			}
		}
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" {
		fmt.Println("Usage: routing remove --channel <channel> --chat-id <id>")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	key := routingMappingKey(channel, chatID)
	next := make([]config.RoutingMapping, 0, len(cfg.Routing.Mappings))
	removed := false
	for _, m := range cfg.Routing.Mappings {
		if routingMappingKey(m.Channel, m.ChatID) == key {
			removed = true
			continue
		}
		next = append(next, m)
	}
	if !removed {
		fmt.Printf("No mapping found for %s:%s\n", channel, chatID)
		return
	}

	cfg.Routing.Mappings = next
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}
	fmt.Printf("Removed routing mapping for %s:%s\n", channel, chatID)
}

func routingSetUsersCmd() {
	channel, chatID, allowCSV := "", "", ""
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--chat-id":
			if i+1 < len(args) {
				chatID = args[i+1]
				i++
			}
		case "--allow":
			if i+1 < len(args) {
				allowCSV = args[i+1]
				i++
			}
		}
	}
	if strings.TrimSpace(channel) == "" || strings.TrimSpace(chatID) == "" || strings.TrimSpace(allowCSV) == "" {
		fmt.Println("Usage: routing set-users --channel <channel> --chat-id <id> --allow <id1,id2>")
		return
	}

	allowed := parseAllowListCSV(allowCSV)
	if len(allowed) == 0 {
		fmt.Println("Error: --allow must include at least one sender id")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	key := routingMappingKey(channel, chatID)
	found := false
	for i := range cfg.Routing.Mappings {
		if routingMappingKey(cfg.Routing.Mappings[i].Channel, cfg.Routing.Mappings[i].ChatID) == key {
			cfg.Routing.Mappings[i].AllowedSenders = allowed
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("No mapping found for %s:%s\n", channel, chatID)
		return
	}

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}
	fmt.Printf("Updated allowed_senders for %s:%s\n", channel, chatID)
}

func routingValidateCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	if err := config.ValidateRoutingConfig(cfg.Routing); err != nil {
		fmt.Printf("Routing config invalid: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Routing config is valid.")
}

func routingExplainCmd() {
	channel, chatID, sender := "", "", ""
	mention := false
	dm := false
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		case "--chat-id":
			if i+1 < len(args) {
				chatID = args[i+1]
				i++
			}
		case "--sender":
			if i+1 < len(args) {
				sender = args[i+1]
				i++
			}
		case "--mention":
			mention = true
		case "--dm":
			dm = true
		}
	}
	if channel == "" || chatID == "" || sender == "" {
		fmt.Println("Usage: routing explain --channel <channel> --chat-id <id> --sender <id> [--mention] [--dm]")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	resolver, err := routing.NewResolver(cfg)
	if err != nil {
		fmt.Printf("Routing config invalid: %v\n", err)
		return
	}

	metadata := map[string]string{
		"is_mention": fmt.Sprintf("%t", mention),
		"is_dm":      fmt.Sprintf("%t", dm),
	}
	d := resolver.Resolve(bus.InboundMessage{
		Channel:  channel,
		ChatID:   chatID,
		SenderID: sender,
		Metadata: metadata,
	})

	fmt.Println("Routing explain:")
	fmt.Printf("  event: %s\n", d.Event)
	fmt.Printf("  allowed: %t\n", d.Allowed)
	fmt.Printf("  workspace: %s\n", d.Workspace)
	fmt.Printf("  session_key: %s\n", d.SessionKey)
	fmt.Printf("  reason: %s\n", d.Reason)
	if strings.TrimSpace(d.MappingLabel) != "" {
		fmt.Printf("  mapping_label: %s\n", d.MappingLabel)
	}
}

func routingSetEnabledCmd(enabled bool) {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	cfg.Routing.Enabled = enabled
	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving config: %v\n", err)
		return
	}
	fmt.Printf("Routing %s.\n", map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

func routingExportCmd() {
	out := ""
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		if args[i] == "--out" && i+1 < len(args) {
			out = args[i+1]
			i++
		}
	}
	if strings.TrimSpace(out) == "" {
		fmt.Println("Usage: routing export --out <file>")
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	data, err := json.MarshalIndent(cfg.Routing, "", "  ")
	if err != nil {
		fmt.Printf("Error encoding routing config: %v\n", err)
		return
	}

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		return
	}
	if err := os.WriteFile(out, data, 0o600); err != nil {
		fmt.Printf("Error writing export file: %v\n", err)
		return
	}
	fmt.Printf("Exported routing config to %s\n", out)
}

func routingImportCmd() {
	inFile := ""
	replace := false
	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--in":
			if i+1 < len(args) {
				inFile = args[i+1]
				i++
			}
		case "--replace":
			replace = true
		}
	}
	if strings.TrimSpace(inFile) == "" {
		fmt.Println("Usage: routing import --in <file> [--replace]")
		return
	}

	payload, err := os.ReadFile(inFile)
	if err != nil {
		fmt.Printf("Error reading import file: %v\n", err)
		return
	}

	imported, err := decodeRoutingPayload(payload)
	if err != nil {
		fmt.Printf("Error decoding import file: %v\n", err)
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	if replace {
		cfg.Routing = imported
	} else {
		cfg.Routing = mergeRouting(cfg.Routing, imported)
	}

	if err := config.SaveConfig(getConfigPath(), cfg); err != nil {
		fmt.Printf("Error saving merged routing config: %v\n", err)
		return
	}
	fmt.Printf("Imported routing config from %s\n", inFile)
}

func routingReloadCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}
	if _, err := routing.NewResolver(cfg); err != nil {
		fmt.Printf("Routing config invalid: %v\n", err)
		return
	}

	triggerPath := routingReloadTriggerPath()
	if err := os.MkdirAll(filepath.Dir(triggerPath), 0o755); err != nil {
		fmt.Printf("Error preparing reload trigger path: %v\n", err)
		return
	}
	payload := []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	if err := os.WriteFile(triggerPath, payload, 0o600); err != nil {
		fmt.Printf("Error writing reload trigger: %v\n", err)
		return
	}
	fmt.Printf("Routing reload requested via %s\n", triggerPath)
}

func parseAllowListCSV(raw string) config.FlexibleStringSlice {
	parts := strings.Split(raw, ",")
	out := make(config.FlexibleStringSlice, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func routingMappingKey(channel, chatID string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + "\x00" + strings.TrimSpace(chatID)
}

func decodeRoutingPayload(payload []byte) (config.RoutingConfig, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return config.RoutingConfig{}, err
	}

	var out config.RoutingConfig
	if wrapped, ok := raw["routing"]; ok {
		if err := json.Unmarshal(wrapped, &out); err != nil {
			return config.RoutingConfig{}, err
		}
	} else {
		if err := json.Unmarshal(payload, &out); err != nil {
			return config.RoutingConfig{}, err
		}
	}
	if strings.TrimSpace(out.UnmappedBehavior) == "" {
		out.UnmappedBehavior = config.RoutingUnmappedBehaviorDefault
	}
	return out, nil
}

func mergeRouting(current, imported config.RoutingConfig) config.RoutingConfig {
	out := current
	if strings.TrimSpace(imported.UnmappedBehavior) != "" {
		out.UnmappedBehavior = imported.UnmappedBehavior
	}
	out.Enabled = imported.Enabled

	index := map[string]int{}
	for i, m := range out.Mappings {
		index[routingMappingKey(m.Channel, m.ChatID)] = i
	}
	for _, m := range imported.Mappings {
		key := routingMappingKey(m.Channel, m.ChatID)
		if idx, ok := index[key]; ok {
			out.Mappings[idx] = m
		} else {
			out.Mappings = append(out.Mappings, m)
			index[key] = len(out.Mappings) - 1
		}
	}
	return out
}
