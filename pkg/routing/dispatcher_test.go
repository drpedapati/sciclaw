package routing

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestDispatcherSendBlockNotice_UnmappedIncludesAppAndMentionOnlyGuidance(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	d := NewDispatcher(mb, nil, nil)
	msg := bus.InboundMessage{Channel: "discord", ChatID: "1480213273453396101", SenderID: "12345"}
	d.sendBlockNotice(context.Background(), msg, Decision{Event: EventRouteUnmapped})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound routing notice")
	}
	if out.Channel != msg.Channel || out.ChatID != msg.ChatID {
		t.Fatalf("unexpected outbound target: %+v", out)
	}
	for _, want := range []string{
		"This chat is not mapped to a workspace yet.",
		"Open `sciclaw app` in your terminal, go to Routing",
		"sciclaw routing add --channel discord --chat-id 1480213273453396101 --workspace /absolute/path --allow <sender_id>",
		"Unmapped behavior to `mention_only`",
		"Use `default` only if you want every unmapped room to fall back automatically.",
	} {
		if !strings.Contains(out.Content, want) {
			t.Fatalf("expected notice to contain %q, got: %s", want, out.Content)
		}
	}
}

func TestDispatcherSendBlockNotice_InternalChannelSkipsNotice(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	d := NewDispatcher(mb, nil, nil)
	d.sendBlockNotice(context.Background(), bus.InboundMessage{Channel: "system", ChatID: "discord:123"}, Decision{Event: EventRouteUnmapped})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, ok := mb.SubscribeOutbound(ctx); ok {
		t.Fatal("expected no outbound notice for internal channel")
	}
}
