package channels

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newTestDiscordChannel() *DiscordChannel {
	ch := &DiscordChannel{
		BaseChannel: NewBaseChannel("discord", config.DiscordConfig{}, bus.NewMessageBus(), nil),
		ctx:         context.Background(),
		typing:      make(map[string]*typingState),
		typingEvery: 10 * time.Millisecond,
	}
	ch.sendTypingFn = func(channelID string) error { return nil }
	ch.sendMessageFn = func(channelID, content string) error { return nil }
	ch.setRunning(true)
	return ch
}

func TestNormalizeDiscordBotToken(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "raw", in: "abc123", want: "abc123"},
		{name: "bot prefix", in: "Bot abc123", want: "abc123"},
		{name: "bot prefix lowercase", in: "bot abc123", want: "abc123"},
		{name: "quoted", in: "\"abc123\"", want: "abc123"},
		{name: "quoted with bot prefix", in: "'Bot abc123'", want: "abc123"},
		{name: "spaces", in: "   Bot   abc123   ", want: "abc123"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeDiscordBotToken(tc.in); got != tc.want {
				t.Fatalf("NormalizeDiscordBotToken(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestDiscordTypingLifecycleReferenceCount(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	calls := 0
	ch.sendTypingFn = func(channelID string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil
	}

	ch.startTyping("chan-1")
	time.Sleep(25 * time.Millisecond)

	ch.startTyping("chan-1")
	ch.stopTyping("chan-1")

	mu.Lock()
	beforeFinalStop := calls
	mu.Unlock()

	time.Sleep(25 * time.Millisecond)
	mu.Lock()
	afterSingleStop := calls
	mu.Unlock()

	if afterSingleStop <= beforeFinalStop {
		t.Fatalf("typing should continue after one stop with pending requests, got before=%d after=%d", beforeFinalStop, afterSingleStop)
	}

	ch.stopTyping("chan-1")
	time.Sleep(25 * time.Millisecond)

	mu.Lock()
	afterFinalStop := calls
	mu.Unlock()
	time.Sleep(25 * time.Millisecond)
	mu.Lock()
	afterWait := calls
	mu.Unlock()

	if afterWait != afterFinalStop {
		t.Fatalf("typing should stop after final stop, got afterFinalStop=%d afterWait=%d", afterFinalStop, afterWait)
	}
}

func TestDiscordSendStopsTypingPerReply(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.startTyping("chan-2")
	ch.startTyping("chan-2")

	ch.typingMu.Lock()
	if got := ch.typing["chan-2"].pending; got != 2 {
		ch.typingMu.Unlock()
		t.Fatalf("expected pending=2, got %d", got)
	}
	ch.typingMu.Unlock()

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-2",
		Content: "done",
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	ch.typingMu.Lock()
	state, ok := ch.typing["chan-2"]
	if !ok {
		ch.typingMu.Unlock()
		t.Fatalf("expected typing state to remain for one pending request")
	}
	if state.pending != 1 {
		ch.typingMu.Unlock()
		t.Fatalf("expected pending=1 after first reply, got %d", state.pending)
	}
	ch.typingMu.Unlock()

	err = ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-2",
		Content: "done-2",
	})
	if err != nil {
		t.Fatalf("unexpected second send error: %v", err)
	}

	ch.typingMu.Lock()
	_, ok = ch.typing["chan-2"]
	ch.typingMu.Unlock()
	if ok {
		t.Fatalf("expected typing state removed after final reply")
	}
}

func TestDiscordStopCancelsAllTypingLoops(t *testing.T) {
	ch := newTestDiscordChannel()
	ch.startTyping("chan-a")
	ch.startTyping("chan-b")

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	ch.typingMu.Lock()
	n := len(ch.typing)
	ch.typingMu.Unlock()
	if n != 0 {
		t.Fatalf("expected no typing loops after stop, got %d", n)
	}
}

func TestSplitDiscordMessage_RespectsLimit(t *testing.T) {
	msg := strings.Repeat("a", 4205)
	chunks := splitDiscordMessage(msg, discordMaxRunes)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if got := len([]rune(chunks[0])); got != 2000 {
		t.Fatalf("expected first chunk length 2000, got %d", got)
	}
	if got := len([]rune(chunks[1])); got != 2000 {
		t.Fatalf("expected second chunk length 2000, got %d", got)
	}
	if got := len([]rune(chunks[2])); got != 205 {
		t.Fatalf("expected third chunk length 205, got %d", got)
	}
}

func TestDiscordSend_SendsChunkedMessages(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	var sent []string
	ch.sendMessageFn = func(channelID, content string) error {
		mu.Lock()
		sent = append(sent, content)
		mu.Unlock()
		return nil
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-3",
		Content: strings.Repeat("b", 4100),
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	mu.Lock()
	count := len(sent)
	copySent := append([]string(nil), sent...)
	mu.Unlock()

	if count != 3 {
		t.Fatalf("expected 3 sent chunks, got %d", count)
	}
	for i, chunk := range copySent {
		if n := len([]rune(chunk)); n > discordMaxRunes {
			t.Fatalf("chunk %d exceeds limit: %d", i, n)
		}
	}
}

func TestDiscordSend_WithAttachments_RoutesCaptionsAndRemainingText(t *testing.T) {
	ch := newTestDiscordChannel()
	var mu sync.Mutex
	var sentFiles []struct {
		Content    string
		Attachment bus.OutboundAttachment
	}
	var sentMessages []string

	ch.sendFileFn = func(channelID, content string, attachment bus.OutboundAttachment) error {
		mu.Lock()
		sentFiles = append(sentFiles, struct {
			Content    string
			Attachment bus.OutboundAttachment
		}{
			Content:    content,
			Attachment: attachment,
		})
		mu.Unlock()
		return nil
	}
	ch.sendMessageFn = func(channelID, content string) error {
		mu.Lock()
		sentMessages = append(sentMessages, content)
		mu.Unlock()
		return nil
	}

	content := strings.Repeat("x", discordMaxRunes+35)
	chunks := splitDiscordMessage(content, discordMaxRunes)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks for setup, got %d", len(chunks))
	}

	err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "discord",
		ChatID:  "chan-attach",
		Content: content,
		Attachments: []bus.OutboundAttachment{
			{Path: "/tmp/a.docx", Filename: "a.docx"},
			{Path: "/tmp/b.pdf", Filename: "b.pdf"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected send error: %v", err)
	}

	mu.Lock()
	gotFiles := append([]struct {
		Content    string
		Attachment bus.OutboundAttachment
	}(nil), sentFiles...)
	gotMessages := append([]string(nil), sentMessages...)
	mu.Unlock()

	if len(gotFiles) != 2 {
		t.Fatalf("expected 2 file sends, got %d", len(gotFiles))
	}
	if gotFiles[0].Content != chunks[0] {
		t.Fatalf("expected first attachment caption to equal first chunk")
	}
	if gotFiles[1].Content != "" {
		t.Fatalf("expected second attachment without caption, got %q", gotFiles[1].Content)
	}
	if len(gotMessages) != 1 {
		t.Fatalf("expected 1 trailing text message, got %d", len(gotMessages))
	}
	if gotMessages[0] != chunks[1] {
		t.Fatalf("expected trailing message to equal second chunk")
	}
}

func TestSendDiscordAttachment_ValidatesPathsAndSize(t *testing.T) {
	tmp := t.TempDir()
	dirPath := filepath.Join(tmp, "dir")
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: dirPath}); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("expected directory error, got %v", err)
	}

	largePath := filepath.Join(tmp, "large.bin")
	f, err := os.Create(largePath)
	if err != nil {
		t.Fatalf("create large file: %v", err)
	}
	if err := f.Truncate(discordMaxFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("truncate large file: %v", err)
	}
	_ = f.Close()

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: largePath}); err == nil || !strings.Contains(err.Error(), "exceeds Discord limit") {
		t.Fatalf("expected size limit error, got %v", err)
	}

	okPath := filepath.Join(tmp, "ok.txt")
	if err := os.WriteFile(okPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("write small file: %v", err)
	}

	if err := sendDiscordAttachment(nil, "chan", "", bus.OutboundAttachment{Path: okPath}); err == nil || !strings.Contains(err.Error(), "session is nil") {
		t.Fatalf("expected session nil error for valid file, got %v", err)
	}
}
