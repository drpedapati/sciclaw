package tui

import (
	"encoding/json"
	"strings"
	"sync"
)

var configEditMu sync.Mutex

// readConfigMap reads config.json through the executor and returns a generic map
// for read-modify-write operations that preserve all existing fields.
func readConfigMap(exec Executor) (map[string]interface{}, error) {
	raw, err := exec.ReadFile(exec.ConfigPath())
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// writeConfigMap marshals the config map and writes it back through the executor.
func writeConfigMap(exec Executor, m map[string]interface{}) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return exec.WriteFile(exec.ConfigPath(), data, 0644)
}

// updateConfigMap serializes a read-modify-write cycle in-process so concurrent
// tab actions cannot clobber each other's config updates.
func updateConfigMap(exec Executor, mutate func(map[string]interface{}) error) error {
	configEditMu.Lock()
	defer configEditMu.Unlock()

	cfg, err := readConfigMap(exec)
	if err != nil {
		if isConfigNotFoundError(err) {
			cfg = map[string]interface{}{}
		} else {
			return err
		}
	}
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return writeConfigMap(exec, cfg)
}

// ensureMap navigates to or creates a nested map at the given key.
func ensureMap(parent map[string]interface{}, key string) map[string]interface{} {
	v, ok := parent[key]
	if ok {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
	}
	m := map[string]interface{}{}
	parent[key] = m
	return m
}

// getStringSlice returns the string slice at key, or nil.
func getStringSlice(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case []interface{}:
		out := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if val != "" {
			return []string{val}
		}
	}
	return nil
}

// setChannelConfig sets all fields for a channel, used for host→VM import.
func setChannelConfig(exec Executor, channel string, enabled bool, token, proxy string, allowFrom []string) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		cfg = map[string]interface{}{}
	}
	channels := ensureMap(cfg, "channels")
	ch := ensureMap(channels, channel)
	ch["enabled"] = enabled
	ch["token"] = token
	if strings.EqualFold(channel, "telegram") && proxy != "" {
		ch["proxy"] = proxy
	}
	if allowFrom != nil {
		ch["allow_from"] = allowFrom
	}
	return writeConfigMap(exec, cfg)
}

// saveChannelSetupConfig saves channel token and optionally adds a first user.
func saveChannelSetupConfig(exec Executor, channel, token, userEntry string) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		cfg = map[string]interface{}{}
	}
	channels := ensureMap(cfg, "channels")
	ch := ensureMap(channels, channel)
	ch["enabled"] = true
	ch["token"] = token

	if userEntry != "" {
		existing := getStringSlice(ch, "allow_from")
		found := false
		for _, e := range existing {
			if e == userEntry {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, userEntry)
		}
		ch["allow_from"] = existing
	}
	return writeConfigMap(exec, cfg)
}

// appendAllowFrom adds an entry to a channel's allow_from if not already present.
func appendAllowFrom(exec Executor, channel, entry string) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		cfg = map[string]interface{}{}
	}
	channels := ensureMap(cfg, "channels")
	ch := ensureMap(channels, channel)
	existing := getStringSlice(ch, "allow_from")
	for _, e := range existing {
		if e == entry {
			return writeConfigMap(exec, cfg)
		}
	}
	existing = append(existing, entry)
	ch["allow_from"] = existing
	return writeConfigMap(exec, cfg)
}

// removeAllowFrom removes an entry by index from a channel's allow_from.
func removeAllowFrom(exec Executor, channel string, idx int) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		return err
	}
	channels := ensureMap(cfg, "channels")
	ch := ensureMap(channels, channel)
	existing := getStringSlice(ch, "allow_from")
	if idx < 0 || idx >= len(existing) {
		return nil
	}
	existing = append(existing[:idx], existing[idx+1:]...)
	ch["allow_from"] = existing
	return writeConfigMap(exec, cfg)
}

// replaceAllowFrom replaces an entry by index in a channel's allow_from.
func replaceAllowFrom(exec Executor, channel string, idx int, entry string) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		return err
	}
	channels := ensureMap(cfg, "channels")
	ch := ensureMap(channels, channel)
	existing := getStringSlice(ch, "allow_from")
	if idx < 0 || idx >= len(existing) {
		return nil
	}
	existing[idx] = strings.TrimSpace(entry)
	ch["allow_from"] = existing
	return writeConfigMap(exec, cfg)
}
