package channels

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestMarkdownToTelegramHTML_PreservesDistinctInlineCodes(t *testing.T) {
	in := "Paths: `~/sciclaw/IDENTITY.md`, `~/sciclaw/AGENTS.md`, and `state/`."
	out := markdownToTelegramHTML(in)

	if !strings.Contains(out, "<code>~/sciclaw/IDENTITY.md</code>") {
		t.Fatalf("missing first inline code, got: %s", out)
	}
	if !strings.Contains(out, "<code>~/sciclaw/AGENTS.md</code>") {
		t.Fatalf("missing second inline code, got: %s", out)
	}
	if !strings.Contains(out, "<code>state/</code>") {
		t.Fatalf("missing third inline code, got: %s", out)
	}
}

func TestMarkdownToTelegramHTML_PreservesDistinctCodeBlocks(t *testing.T) {
	in := "```txt\nalpha\n```\n\n```txt\nbeta\n```"
	out := markdownToTelegramHTML(in)

	if !strings.Contains(out, "<pre><code>alpha\n</code></pre>") {
		t.Fatalf("missing first code block, got: %s", out)
	}
	if !strings.Contains(out, "<pre><code>beta\n</code></pre>") {
		t.Fatalf("missing second code block, got: %s", out)
	}
}

func TestTelegramAttachmentMethod(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		want     string
	}{
		{name: "photo", filename: "figure.png", want: "photo"},
		{name: "video", filename: "clip.mp4", want: "video"},
		{name: "audio", filename: "note.m4a", want: "audio"},
		{name: "document", filename: "report.docx", want: "document"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := telegramAttachmentMethod(tc.filename); got != tc.want {
				t.Fatalf("telegramAttachmentMethod(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}

func TestSendTelegramAttachment_ValidatesPathAndSize(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sendTelegramAttachment(context.Background(), nil, 1, bus.OutboundAttachment{Path: dirPath}); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory error, got %v", err)
	}

	largePath := filepath.Join(tmp, "large.bin")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatalf("create large file: %v", err)
	}
	if err := f.Truncate(telegramMaxFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate large file: %v", err)
	}
	_ = f.Close()

	if err := sendTelegramAttachment(context.Background(), nil, 1, bus.OutboundAttachment{Path: largePath}); err == nil || !strings.Contains(err.Error(), "exceeds Telegram limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	okPath := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(okPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	if err := sendTelegramAttachment(context.Background(), nil, 1, bus.OutboundAttachment{Path: okPath}); err == nil || !strings.Contains(err.Error(), "bot is nil") {
		t.Fatalf("expected bot nil error for valid file, got %v", err)
	}
}

func TestTelegramSend_AttachmentsOnly_UsesSendFileFn(t *testing.T) {
	ch := &TelegramChannel{
		BaseChannel: NewBaseChannel("telegram", config.TelegramConfig{}, bus.NewMessageBus(), nil),
		sendFileFn: func(ctx context.Context, chatID int64, attachment bus.OutboundAttachment) error {
			return nil
		},
	}
	ch.setRunning(true)

	var got []bus.OutboundAttachment
	ch.sendFileFn = func(ctx context.Context, chatID int64, attachment bus.OutboundAttachment) error {
		got = append(got, attachment)
		return nil
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "telegram",
		ChatID:  "12345",
		Attachments: []bus.OutboundAttachment{
			{Path: "/tmp/a.txt"},
			{Path: "/tmp/b.txt"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 attachments to be sent, got %d", len(got))
	}
}
