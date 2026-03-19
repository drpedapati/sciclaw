package tui

// Exported wrappers for web_cmd.go to access configedit functions
// and snapshot helpers without duplicating logic.

// ReadConfigMap wraps readConfigMap for external callers.
func ReadConfigMap(exec Executor) (map[string]interface{}, error) {
	return readConfigMap(exec)
}

// UpdateConfigMap wraps updateConfigMap for external callers.
func UpdateConfigMap(exec Executor, mutate func(map[string]interface{}) error) error {
	return updateConfigMap(exec, mutate)
}

// SaveChannelSetupConfig wraps saveChannelSetupConfig for external callers.
func SaveChannelSetupConfig(exec Executor, channel, token, userEntry string) error {
	return saveChannelSetupConfig(exec, channel, token, userEntry)
}

// AppendAllowFrom wraps appendAllowFrom for external callers.
func AppendAllowFrom(exec Executor, channel, entry string) error {
	return appendAllowFrom(exec, channel, entry)
}

// RemoveAllowFrom wraps removeAllowFrom for external callers.
func RemoveAllowFrom(exec Executor, channel string, idx int) error {
	return removeAllowFrom(exec, channel, idx)
}

// EnsureMapNested navigates into nested maps, creating them as needed.
// Supports variable depth: EnsureMapNested(cfg, "channels", "email")
func EnsureMapNested(m map[string]interface{}, keys ...string) map[string]interface{} {
	cur := m
	for _, k := range keys {
		cur = ensureMap(cur, k)
	}
	return cur
}

// GetMapNested returns a nested map at the given key path, or an empty map.
func GetMapNested(m map[string]interface{}, keys ...string) map[string]interface{} {
	if m == nil {
		return map[string]interface{}{}
	}
	cur := m
	for _, k := range keys {
		v, ok := cur[k]
		if !ok {
			return map[string]interface{}{}
		}
		sub, ok := v.(map[string]interface{})
		if !ok {
			return map[string]interface{}{}
		}
		cur = sub
	}
	return cur
}

// getNestedValue traverses nested maps and returns the leaf value.
func getNestedValue(m map[string]interface{}, keys ...string) (interface{}, bool) {
	if len(keys) == 0 || m == nil {
		return nil, false
	}
	cur := m
	for i, k := range keys {
		if i == len(keys)-1 {
			v, ok := cur[k]
			return v, ok
		}
		sub, ok := cur[k].(map[string]interface{})
		if !ok {
			return nil, false
		}
		cur = sub
	}
	return nil, false
}

// GetString returns a string value from nested map keys, or "".
func GetString(m map[string]interface{}, keys ...string) string {
	v, ok := getNestedValue(m, keys...)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// GetBool returns a bool value from nested map keys.
func GetBool(m map[string]interface{}, keys ...string) bool {
	v, ok := getNestedValue(m, keys...)
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

// GetStringSliceValue returns a string slice from a map field.
func GetStringSliceValue(m map[string]interface{}, key string) []string {
	result := getStringSlice(m, key)
	if result == nil {
		return []string{}
	}
	return result
}
