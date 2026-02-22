package auth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseOPItemCredential_DirectObject(t *testing.T) {
	expiresAt := time.Date(2030, time.January, 1, 12, 0, 0, 0, time.UTC)

	item := map[string]interface{}{
		"accessToken":  "direct-access-token",
		"refreshToken": "direct-refresh-token",
		"accountId":    "acct-direct",
		"authMethod":   "custom-method",
		"expiresAt":    expiresAt.Format(time.RFC3339),
		"provider":     "provider-in-item",
	}
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	cred, err := ParseOPItemCredential(body, "provider-arg", "oauth")
	if err != nil {
		t.Fatalf("ParseOPItemCredential() error = %v", err)
	}

	if cred.AccessToken != "direct-access-token" {
		t.Errorf("AccessToken = %q, want %q", cred.AccessToken, "direct-access-token")
	}
	if cred.RefreshToken != "direct-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", cred.RefreshToken, "direct-refresh-token")
	}
	if cred.AccountID != "acct-direct" {
		t.Errorf("AccountID = %q, want %q", cred.AccountID, "acct-direct")
	}
	if cred.AuthMethod != "custom-method" {
		t.Errorf("AuthMethod = %q, want %q", cred.AuthMethod, "custom-method")
	}
	if !cred.ExpiresAt.Equal(expiresAt) {
		t.Errorf("ExpiresAt = %s, want %s", cred.ExpiresAt.Format(time.RFC3339), expiresAt.Format(time.RFC3339))
	}
	if cred.Provider != "provider-in-item" {
		t.Errorf("Provider = %q, want %q", cred.Provider, "provider-in-item")
	}
}

func TestParseOPItemCredential_FieldsAliases(t *testing.T) {
	item := map[string]interface{}{
		"fields": []map[string]interface{}{
			{"id": "token", "value": "field-access-token"},
			{"label": "refreshToken", "value": "field-refresh-token"},
			{"id": "account_id", "value": "acct-field"},
			{"label": "expires_at", "value": "1893456000"},
		},
	}
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	cred, err := ParseOPItemCredential(body, "provider-arg", "default-method")
	if err != nil {
		t.Fatalf("ParseOPItemCredential() error = %v", err)
	}

	if cred.AccessToken != "field-access-token" {
		t.Errorf("AccessToken = %q, want %q", cred.AccessToken, "field-access-token")
	}
	if cred.RefreshToken != "field-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", cred.RefreshToken, "field-refresh-token")
	}
	if cred.AccountID != "acct-field" {
		t.Errorf("AccountID = %q, want %q", cred.AccountID, "acct-field")
	}
	if cred.AuthMethod != "default-method" {
		t.Errorf("AuthMethod = %q, want %q", cred.AuthMethod, "default-method")
	}
	if cred.Provider != "provider-arg" {
		t.Errorf("Provider = %q, want %q", cred.Provider, "provider-arg")
	}
	if cred.ExpiresAt.Unix() != 1893456000 {
		t.Errorf("ExpiresAt.Unix() = %d, want %d", cred.ExpiresAt.Unix(), int64(1893456000))
	}
}

func TestParseOPItemCredential_NotesPlainJSON(t *testing.T) {
	notesObj := map[string]interface{}{
		"access_token":  "notes-access-token",
		"refresh_token": "notes-refresh-token",
		"expiresIn":     "120",
	}
	notesJSON, err := json.Marshal(notesObj)
	if err != nil {
		t.Fatalf("json.Marshal(notesObj) error = %v", err)
	}

	item := map[string]interface{}{
		"notesPlain": string(notesJSON),
	}
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal(item) error = %v", err)
	}

	start := time.Now()
	cred, err := ParseOPItemCredential(body, "provider-arg", "")
	end := time.Now()
	if err != nil {
		t.Fatalf("ParseOPItemCredential() error = %v", err)
	}

	if cred.AccessToken != "notes-access-token" {
		t.Errorf("AccessToken = %q, want %q", cred.AccessToken, "notes-access-token")
	}
	if cred.RefreshToken != "notes-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", cred.RefreshToken, "notes-refresh-token")
	}
	if cred.AuthMethod != "oauth" {
		t.Errorf("AuthMethod = %q, want %q", cred.AuthMethod, "oauth")
	}
	if cred.Provider != "provider-arg" {
		t.Errorf("Provider = %q, want %q", cred.Provider, "provider-arg")
	}
	if cred.ExpiresAt.Before(start.Add(110*time.Second)) || cred.ExpiresAt.After(end.Add(130*time.Second)) {
		t.Errorf("ExpiresAt = %s, expected near now + 120s", cred.ExpiresAt.Format(time.RFC3339))
	}
}

func TestParseOPItemCredential_MissingTokenError(t *testing.T) {
	item := map[string]interface{}{
		"fields": []map[string]interface{}{
			{"id": "refresh_token", "value": "only-refresh-token"},
		},
	}
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = ParseOPItemCredential(body, "provider-arg", "oauth")
	if err == nil {
		t.Fatal("ParseOPItemCredential() error = nil, want non-nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "access token") {
		t.Errorf("error = %q, want to contain %q", err.Error(), "access token")
	}
}
