package channels

import (
	"context"
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
