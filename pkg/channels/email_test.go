package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestSendEmailMessage_PostsResendPayload(t *testing.T) {
	tmp := t.TempDir()
	attachPath := filepath.Join(tmp, "note.txt")
	if err := os.WriteFile(attachPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write attachment: %v", err)
	}

	var gotAuth string
	var payload resendPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/emails" {
			t.Fatalf("path=%q want /emails", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_123"}`))
	}))
	defer srv.Close()

	cfg := config.EmailConfig{
		Enabled:     true,
		Provider:    "resend",
		APIKey:      "test-key",
		Address:     "support@example.com",
		DisplayName: "sciClaw",
		BaseURL:     srv.URL,
	}
	msg := bus.OutboundMessage{
		Channel: "email",
		ChatID:  "alice@example.com,bob@example.com",
		Subject: "Status update",
		Content: "Plain body",
		Attachments: []bus.OutboundAttachment{
			{Path: attachPath, Filename: "note.txt"},
		},
	}

	if err := SendEmailMessage(context.Background(), srv.Client(), cfg, msg); err != nil {
		t.Fatalf("SendEmailMessage returned error: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth header=%q want bearer token", gotAuth)
	}
	if payload.Subject != "Status update" {
		t.Fatalf("subject=%q want Status update", payload.Subject)
	}
	// formatFromAddress now always returns the bare email (no display
	// name decoration) because self-hosted Resend instances reject any
	// Name <addr> format in the from field.
	if payload.From != "support@example.com" {
		t.Fatalf("from=%q want bare email", payload.From)
	}
	if len(payload.To) != 2 || payload.To[0] != "alice@example.com" || payload.To[1] != "bob@example.com" {
		t.Fatalf("to=%v", payload.To)
	}
	if payload.Text != "Plain body" {
		t.Fatalf("text=%q", payload.Text)
	}
	if !strings.Contains(payload.HTML, "Plain body") {
		t.Fatalf("html=%q", payload.HTML)
	}
	if len(payload.Attachments) != 1 {
		t.Fatalf("attachments=%d want 1", len(payload.Attachments))
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.Attachments[0].Content)
	if err != nil {
		t.Fatalf("decode attachment: %v", err)
	}
	if string(decoded) != "hello" {
		t.Fatalf("decoded attachment=%q want hello", string(decoded))
	}
}

func TestSendEmailMessage_DefaultSubjectAndRecipientValidation(t *testing.T) {
	cfg := config.EmailConfig{
		Enabled:  true,
		Provider: "resend",
		APIKey:   "test-key",
		Address:  "support@example.com",
		BaseURL:  "https://api.example.test",
	}
	msg := bus.OutboundMessage{
		Channel: "email",
		ChatID:  "not-an-email",
		Content: "Body",
	}
	if err := SendEmailMessage(context.Background(), nil, cfg, msg); err == nil || !strings.Contains(err.Error(), "invalid email recipient") {
		t.Fatalf("expected invalid recipient error, got %v", err)
	}
}
