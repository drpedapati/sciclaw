package bus

import (
	"context"
	"testing"
	"time"
)

func TestPublishConsumeInbound(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	ctx := context.Background()
	msg := InboundMessage{Channel: "test", Content: "hello"}

	if err := mb.PublishInbound(ctx, msg); err != nil {
		t.Fatalf("PublishInbound: %v", err)
	}

	got, ok := mb.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("ConsumeInbound returned false")
	}
	if got.Content != "hello" {
		t.Fatalf("got content %q, want %q", got.Content, "hello")
	}
}

func TestPublishConsumeOutbound(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	ctx := context.Background()
	msg := OutboundMessage{Channel: "test", Content: "world"}

	if err := mb.PublishOutbound(ctx, msg); err != nil {
		t.Fatalf("PublishOutbound: %v", err)
	}

	got, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("SubscribeOutbound returned false")
	}
	if got.Content != "world" {
		t.Fatalf("got content %q, want %q", got.Content, "world")
	}
}

func TestPublishInboundCancelled(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := mb.PublishInbound(ctx, InboundMessage{Content: "x"})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPublishOutboundCancelled(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	// Fill the buffer so the next send would block
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		mb.PublishOutbound(ctx, OutboundMessage{Content: "fill"})
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	err := mb.PublishOutbound(cancelCtx, OutboundMessage{Content: "x"})
	if err != context.Canceled {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestPublishInboundAfterClose(t *testing.T) {
	mb := NewMessageBus()
	mb.Close()

	err := mb.PublishInbound(context.Background(), InboundMessage{Content: "x"})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}

func TestPublishOutboundAfterClose(t *testing.T) {
	mb := NewMessageBus()
	mb.Close()

	err := mb.PublishOutbound(context.Background(), OutboundMessage{Content: "x"})
	if err != ErrBusClosed {
		t.Fatalf("expected ErrBusClosed, got %v", err)
	}
}

func TestConsumeInboundAfterClose(t *testing.T) {
	mb := NewMessageBus()
	mb.Close()

	_, ok := mb.ConsumeInbound(context.Background())
	if ok {
		t.Fatal("expected false from ConsumeInbound after Close")
	}
}

func TestSubscribeOutboundAfterClose(t *testing.T) {
	mb := NewMessageBus()
	mb.Close()

	_, ok := mb.SubscribeOutbound(context.Background())
	if ok {
		t.Fatal("expected false from SubscribeOutbound after Close")
	}
}

func TestCloseIdempotent(t *testing.T) {
	mb := NewMessageBus()
	mb.Close()
	mb.Close() // should not panic
}

func TestConsumeInboundContextCancel(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, ok := mb.ConsumeInbound(ctx)
	if ok {
		t.Fatal("expected false from ConsumeInbound with cancelled context")
	}
}

func TestPublishDoesNotBlockOnClose(t *testing.T) {
	mb := NewMessageBus()

	// Fill the buffer
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		mb.PublishInbound(ctx, InboundMessage{Content: "fill"})
	}

	// Now publish should block — close the bus from another goroutine
	done := make(chan error, 1)
	go func() {
		done <- mb.PublishInbound(context.Background(), InboundMessage{Content: "blocked"})
	}()

	time.Sleep(10 * time.Millisecond)
	mb.Close()

	select {
	case err := <-done:
		if err != ErrBusClosed {
			t.Fatalf("expected ErrBusClosed, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PublishInbound did not unblock after Close")
	}
}
