package auth

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	opKeyAccessToken = "access_token"
	opKeyRefresh     = "refresh_token"
	opKeyAccountID   = "account_id"
	opKeyAuthMethod  = "auth_method"
	opKeyExpiresAt   = "expires_at"
	opKeyExpiresIn   = "expires_in"
	opKeyProvider    = "provider"
)

// ParseOPItemCredential parses either a direct credential JSON object or a
// 1Password item JSON payload and returns an AuthCredential.
func ParseOPItemCredential(itemJSON []byte, provider string, defaultAuthMethod string) (*AuthCredential, error) {
	var item map[string]interface{}
	if err := json.Unmarshal(itemJSON, &item); err != nil {
		return nil, fmt.Errorf("parsing item JSON: %w", err)
	}

	sources := []map[string]interface{}{
		extractCredentialValuesFromObject(item),
		extractCredentialValuesFromFields(item["fields"]),
		extractCredentialValuesFromNotes(item["notesPlain"]),
	}

	accessToken := firstNonEmptyString(collectCandidateValues(sources, opKeyAccessToken))
	if accessToken == "" {
		return nil, fmt.Errorf("access token is required")
	}

	refreshToken := firstNonEmptyString(collectCandidateValues(sources, opKeyRefresh))
	accountID := firstNonEmptyString(collectCandidateValues(sources, opKeyAccountID))

	authMethod := firstNonEmptyString(collectCandidateValues(sources, opKeyAuthMethod))
	if authMethod == "" {
		if strings.TrimSpace(defaultAuthMethod) != "" {
			authMethod = strings.TrimSpace(defaultAuthMethod)
		} else {
			authMethod = "oauth"
		}
	}

	expiresAt, hasExpiresAt, err := parseExpiresAtCandidates(collectCandidateValues(sources, opKeyExpiresAt))
	if err != nil {
		return nil, err
	}
	if !hasExpiresAt {
		expiresIn, hasExpiresIn, err := parseExpiresInCandidates(collectCandidateValues(sources, opKeyExpiresIn))
		if err != nil {
			return nil, err
		}
		if hasExpiresIn {
			expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
		}
	}

	credProvider := firstNonEmptyString(collectCandidateValues(sources, opKeyProvider))
	if credProvider == "" {
		credProvider = strings.TrimSpace(provider)
	}

	return &AuthCredential{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		AccountID:    accountID,
		ExpiresAt:    expiresAt,
		Provider:     credProvider,
		AuthMethod:   authMethod,
	}, nil
}

func extractCredentialValuesFromObject(obj map[string]interface{}) map[string]interface{} {
	if len(obj) == 0 {
		return nil
	}

	values := make(map[string]interface{})

	if v, ok := findValueByAliases(obj, "access_token", "accessToken", "token"); ok {
		values[opKeyAccessToken] = v
	}
	if v, ok := findValueByAliases(obj, "refresh_token", "refreshToken"); ok {
		values[opKeyRefresh] = v
	}
	if v, ok := findValueByAliases(obj, "account_id", "accountId"); ok {
		values[opKeyAccountID] = v
	}
	if v, ok := findValueByAliases(obj, "auth_method", "authMethod", "method"); ok {
		values[opKeyAuthMethod] = v
	}
	if v, ok := findValueByAliases(obj, "expires_at", "expiresAt"); ok {
		values[opKeyExpiresAt] = v
	}
	if v, ok := findValueByAliases(obj, "expires_in", "expiresIn"); ok {
		values[opKeyExpiresIn] = v
	}
	if v, ok := findValueByAliases(obj, "provider"); ok {
		values[opKeyProvider] = v
	}

	return values
}

func extractCredentialValuesFromFields(rawFields interface{}) map[string]interface{} {
	fields, ok := rawFields.([]interface{})
	if !ok || len(fields) == 0 {
		return nil
	}

	values := make(map[string]interface{})
	for _, field := range fields {
		m, ok := field.(map[string]interface{})
		if !ok {
			continue
		}

		fieldValue, hasValue := m["value"]
		if !hasValue {
			continue
		}

		for _, keyName := range []string{"id", "label", "title", "name"} {
			key, ok := anyToString(m[keyName])
			if !ok {
				continue
			}
			credKey := canonicalCredentialKey(key)
			if credKey == "" {
				continue
			}

			if existing, exists := values[credKey]; !exists || isNilOrEmpty(existing) {
				values[credKey] = fieldValue
			}
		}
	}

	return values
}

func extractCredentialValuesFromNotes(rawNotes interface{}) map[string]interface{} {
	if rawNotes == nil {
		return nil
	}

	if noteObj, ok := rawNotes.(map[string]interface{}); ok {
		return extractCredentialValuesFromObject(noteObj)
	}

	notes, ok := anyToString(rawNotes)
	if !ok {
		return nil
	}
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return nil
	}

	var noteObj map[string]interface{}
	if err := json.Unmarshal([]byte(notes), &noteObj); err != nil {
		return nil
	}
	return extractCredentialValuesFromObject(noteObj)
}

func collectCandidateValues(sources []map[string]interface{}, key string) []interface{} {
	values := make([]interface{}, 0, len(sources))
	for _, source := range sources {
		if source == nil {
			continue
		}
		if v, ok := source[key]; ok {
			values = append(values, v)
		}
	}
	return values
}

func firstNonEmptyString(values []interface{}) string {
	for _, value := range values {
		s, ok := anyToString(value)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			return s
		}
	}
	return ""
}

func findValueByAliases(obj map[string]interface{}, aliases ...string) (interface{}, bool) {
	bestIndex := len(aliases) + 1
	var bestValue interface{}
	found := false

	for k, v := range obj {
		key := normalizeAliasKey(k)
		for i, alias := range aliases {
			if key == normalizeAliasKey(alias) && i < bestIndex {
				bestIndex = i
				bestValue = v
				found = true
			}
		}
	}

	return bestValue, found
}

func canonicalCredentialKey(key string) string {
	switch normalizeAliasKey(key) {
	case "accesstoken", "token":
		return opKeyAccessToken
	case "refreshtoken":
		return opKeyRefresh
	case "accountid":
		return opKeyAccountID
	case "authmethod", "method":
		return opKeyAuthMethod
	case "expiresat":
		return opKeyExpiresAt
	case "expiresin":
		return opKeyExpiresIn
	case "provider":
		return opKeyProvider
	default:
		return ""
	}
}

func normalizeAliasKey(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseExpiresAtCandidates(values []interface{}) (time.Time, bool, error) {
	hadValue := false
	var lastErr error

	for _, value := range values {
		if isNilOrEmpty(value) {
			continue
		}
		hadValue = true

		ts, err := parseExpiresAtValue(value)
		if err == nil {
			return ts, true, nil
		}
		lastErr = err
	}

	if hadValue {
		if lastErr != nil {
			return time.Time{}, false, fmt.Errorf("invalid expires_at: %w", lastErr)
		}
		return time.Time{}, false, fmt.Errorf("invalid expires_at")
	}

	return time.Time{}, false, nil
}

func parseExpiresAtValue(value interface{}) (time.Time, error) {
	if s, ok := anyToString(value); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return time.Time{}, fmt.Errorf("empty value")
		}
		if ts, err := time.Parse(time.RFC3339, s); err == nil {
			return ts, nil
		}
		secs, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("expected RFC3339 or unix seconds")
		}
		return time.Unix(secs, 0).UTC(), nil
	}

	secs, err := anyToInt64(value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(secs, 0).UTC(), nil
}

func parseExpiresInCandidates(values []interface{}) (int64, bool, error) {
	hadValue := false
	var lastErr error

	for _, value := range values {
		if isNilOrEmpty(value) {
			continue
		}
		hadValue = true

		secs, err := anyToInt64(value)
		if err == nil {
			return secs, true, nil
		}
		lastErr = err
	}

	if hadValue {
		if lastErr != nil {
			return 0, false, fmt.Errorf("invalid expires_in: %w", lastErr)
		}
		return 0, false, fmt.Errorf("invalid expires_in")
	}

	return 0, false, nil
}

func anyToString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case int:
		return strconv.Itoa(v), true
	case int8:
		return strconv.FormatInt(int64(v), 10), true
	case int16:
		return strconv.FormatInt(int64(v), 10), true
	case int32:
		return strconv.FormatInt(int64(v), 10), true
	case int64:
		return strconv.FormatInt(v, 10), true
	case uint:
		return strconv.FormatUint(uint64(v), 10), true
	case uint8:
		return strconv.FormatUint(uint64(v), 10), true
	case uint16:
		return strconv.FormatUint(uint64(v), 10), true
	case uint32:
		return strconv.FormatUint(uint64(v), 10), true
	case uint64:
		return strconv.FormatUint(v, 10), true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case float32:
		f64 := float64(v)
		if f64 == float64(int64(f64)) {
			return strconv.FormatInt(int64(f64), 10), true
		}
		return strconv.FormatFloat(f64, 'f', -1, 64), true
	default:
		return "", false
	}
}

func anyToInt64(value interface{}) (int64, error) {
	switch v := value.(type) {
	case json.Number:
		return v.Int64()
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > uint64(^uint64(0)>>1) {
			return 0, fmt.Errorf("value overflows int64")
		}
		return int64(v), nil
	case float64:
		if v != float64(int64(v)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(v), nil
	case float32:
		f64 := float64(v)
		if f64 != float64(int64(f64)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(f64), nil
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return 0, fmt.Errorf("empty integer value")
		}
		return strconv.ParseInt(s, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported integer type %T", value)
	}
}

func isNilOrEmpty(value interface{}) bool {
	if value == nil {
		return true
	}
	if s, ok := value.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}
