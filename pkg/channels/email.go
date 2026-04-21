package channels

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const defaultEmailBaseURL = "https://api.resend.com"

type EmailChannel struct {
	*BaseChannel
	config config.EmailConfig
	client *http.Client
}

func NewEmailChannel(cfg config.EmailConfig, messageBus *bus.MessageBus) (*EmailChannel, error) {
	if strings.TrimSpace(strings.ToLower(cfg.Provider)) != "" && !strings.EqualFold(strings.TrimSpace(cfg.Provider), "resend") {
		return nil, fmt.Errorf("email provider %q is not supported in this build", cfg.Provider)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, fmt.Errorf("email api_key is required")
	}
	if _, err := parseSingleAddress(cfg.Address); err != nil {
		return nil, fmt.Errorf("email address is invalid: %w", err)
	}

	base := NewBaseChannel("email", cfg, messageBus, cfg.AllowFrom)
	return &EmailChannel{
		BaseChannel: base,
		config:      cfg,
		client:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *EmailChannel) Start(ctx context.Context) error {
	c.setRunning(true)
	if c.config.ReceiveEnabled {
		logger.WarnC("email", "Inbound email receive is configured but not supported in this build; email channel is send-only")
	}
	return nil
}

func (c *EmailChannel) Stop(ctx context.Context) error {
	c.setRunning(false)
	return nil
}

func (c *EmailChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("email channel not running")
	}
	return SendEmailMessage(ctx, c.client, c.config, msg)
}

type resendAttachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}

type resendPayload struct {
	From        string             `json:"from"`
	To          []string           `json:"to"`
	Subject     string             `json:"subject"`
	Text        string             `json:"text,omitempty"`
	HTML        string             `json:"html,omitempty"`
	Attachments []resendAttachment `json:"attachments,omitempty"`
}

func SendEmailMessage(ctx context.Context, client *http.Client, cfg config.EmailConfig, msg bus.OutboundMessage) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	if strings.TrimSpace(strings.ToLower(cfg.Provider)) != "" && !strings.EqualFold(strings.TrimSpace(cfg.Provider), "resend") {
		return fmt.Errorf("email provider %q is not supported in this build", cfg.Provider)
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return fmt.Errorf("email api_key is required")
	}

	fromAddress, err := parseSingleAddress(cfg.Address)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	recipients, err := parseRecipientAddresses(msg.ChatID)
	if err != nil {
		return err
	}

	subject := strings.TrimSpace(msg.Subject)
	if subject == "" {
		subject = "sciClaw message"
	}

	payload := resendPayload{
		From:    formatFromAddress(strings.TrimSpace(cfg.DisplayName), fromAddress),
		To:      recipients,
		Subject: subject,
		Text:    msg.Content,
		HTML:    renderEmailHTML(msg.Content),
	}

	if len(msg.Attachments) > 0 {
		attachments, err := buildResendAttachments(msg.Attachments)
		if err != nil {
			return err
		}
		payload.Attachments = attachments
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(emailBaseURL(cfg.BaseURL), "/")+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.APIKey))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(respBody) == 0 {
			return fmt.Errorf("resend send failed: %s", resp.Status)
		}
		return fmt.Errorf("resend send failed: %s: %s", resp.Status, strings.TrimSpace(string(respBody)))
	}

	logger.InfoCF("email", "Email sent", map[string]interface{}{
		"to":          strings.Join(recipients, ","),
		"subject":     subject,
		"attachments": len(payload.Attachments),
	})
	return nil
}

func parseSingleAddress(raw string) (string, error) {
	addr, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return addr.Address, nil
}

func parseRecipientAddresses(raw string) ([]string, error) {
	normalized := strings.NewReplacer(";", ",", "\n", ",").Replace(strings.TrimSpace(raw))
	if normalized == "" {
		return nil, fmt.Errorf("email recipient is required")
	}

	list, err := mail.ParseAddressList(normalized)
	if err != nil {
		parts := strings.Split(normalized, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			addr, parseErr := mail.ParseAddress(part)
			if parseErr != nil {
				return nil, fmt.Errorf("invalid email recipient %q: %w", part, parseErr)
			}
			out = append(out, addr.Address)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("email recipient is required")
		}
		return out, nil
	}

	out := make([]string, 0, len(list))
	for _, addr := range list {
		out = append(out, addr.Address)
	}
	return out, nil
}

func formatFromAddress(displayName, address string) string {
	// Always return the bare email address. Self-hosted Resend instances
	// (e.g., resend.cincineuro.com) reject any display-name decoration
	// in the from field, whether RFC 5322 quoted ("Name" <addr>) or
	// unquoted (Name <addr>). Cloud Resend accepts both, but the bare
	// address works everywhere. The display name is cosmetic anyway:
	// Resend controls what the recipient sees in their MUA via the
	// domain's DKIM/SPF settings, not the API payload.
	return address
}

func emailBaseURL(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		return defaultEmailBaseURL
	}
	return base
}

func renderEmailHTML(content string) string {
	if strings.TrimSpace(content) == "" {
		return "<p></p>"
	}
	return `<div style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;white-space:pre-wrap;line-height:1.5">` +
		html.EscapeString(content) +
		`</div>`
}

func buildResendAttachments(items []bus.OutboundAttachment) ([]resendAttachment, error) {
	attachments := make([]resendAttachment, 0, len(items))
	for _, item := range items {
		data, err := os.ReadFile(item.Path)
		if err != nil {
			return nil, fmt.Errorf("read attachment %q: %w", item.Path, err)
		}
		filename := strings.TrimSpace(item.Filename)
		if filename == "" {
			filename = filepath.Base(item.Path)
		}
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		attachments = append(attachments, resendAttachment{
			Filename:    filename,
			Content:     base64.StdEncoding.EncodeToString(data),
			ContentType: contentType,
		})
	}
	return attachments, nil
}
