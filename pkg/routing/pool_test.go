package routing

import (
	"context"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
)

type fakeHandler struct {
	received chan bus.InboundMessage
}

func (h *fakeHandler) HandleInbound(_ context.Context, msg bus.InboundMessage) {
	h.received <- msg
}

func TestAgentLoopPool_ReusesWorkspaceHandler(t *testing.T) {
	created := 0
	handlers := map[string]*fakeHandler{}

	pool := NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		created++
		h := &fakeHandler{received: make(chan bus.InboundMessage, 8)}
		handlers[target.key()] = h
		return h, nil
	})
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	msg1 := bus.InboundMessage{Channel: "discord", ChatID: "1", SessionKey: "s1"}
	msg2 := bus.InboundMessage{Channel: "discord", ChatID: "1", SessionKey: "s2"}
	target := LoopTarget{Workspace: "/tmp/ws-a"}
	if err := pool.Dispatch(ctx, target, msg1); err != nil {
		t.Fatalf("dispatch msg1: %v", err)
	}
	if err := pool.Dispatch(ctx, target, msg2); err != nil {
		t.Fatalf("dispatch msg2: %v", err)
	}

	h := handlers[target.key()]
	if h == nil {
		t.Fatal("expected handler for workspace")
	}

	select {
	case <-h.received:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first message")
	}
	select {
	case <-h.received:
	case <-ctx.Done():
		t.Fatal("timed out waiting for second message")
	}

	if created != 1 {
		t.Fatalf("expected 1 handler creation, got %d", created)
	}
	if pool.Size() != 1 {
		t.Fatalf("expected pool size 1, got %d", pool.Size())
	}
}

func TestAgentLoopPool_CreatesDistinctWorkspaceHandlers(t *testing.T) {
	created := 0
	pool := NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		_ = target
		created++
		return &fakeHandler{received: make(chan bus.InboundMessage, 1)}, nil
	})
	defer pool.Close()

	ctx := context.Background()
	if err := pool.Dispatch(ctx, LoopTarget{Workspace: "/tmp/ws-a"}, bus.InboundMessage{}); err != nil {
		t.Fatalf("dispatch ws-a: %v", err)
	}
	if err := pool.Dispatch(ctx, LoopTarget{Workspace: "/tmp/ws-b"}, bus.InboundMessage{}); err != nil {
		t.Fatalf("dispatch ws-b: %v", err)
	}

	if created != 2 {
		t.Fatalf("expected 2 handler creations, got %d", created)
	}
	if pool.Size() != 2 {
		t.Fatalf("expected pool size 2, got %d", pool.Size())
	}
}

func TestAgentLoopPool_ClosePreventsDispatch(t *testing.T) {
	pool := NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		_ = target
		return &fakeHandler{received: make(chan bus.InboundMessage, 1)}, nil
	})
	pool.Close()

	err := pool.Dispatch(context.Background(), LoopTarget{Workspace: "/tmp/ws-a"}, bus.InboundMessage{})
	if err == nil {
		t.Fatal("expected error after pool close")
	}
}

func TestAgentLoopPool_SameWorkspaceDifferentRuntimeCreatesDistinctHandlers(t *testing.T) {
	created := 0
	pool := NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		_ = target
		created++
		return &fakeHandler{received: make(chan bus.InboundMessage, 1)}, nil
	})
	defer pool.Close()

	ctx := context.Background()
	cloud := LoopTarget{Workspace: "/tmp/ws-a", Runtime: RuntimeProfile{Mode: "cloud"}}
	phi := LoopTarget{
		Workspace: "/tmp/ws-a",
		Runtime: RuntimeProfile{
			Mode:         "phi",
			LocalBackend: "ollama",
			LocalModel:   "qwen3.5:4b",
		},
	}
	if err := pool.Dispatch(ctx, cloud, bus.InboundMessage{}); err != nil {
		t.Fatalf("dispatch cloud target: %v", err)
	}
	if err := pool.Dispatch(ctx, phi, bus.InboundMessage{}); err != nil {
		t.Fatalf("dispatch phi target: %v", err)
	}

	if created != 2 {
		t.Fatalf("expected 2 handler creations, got %d", created)
	}
	if pool.Size() != 2 {
		t.Fatalf("expected pool size 2, got %d", pool.Size())
	}
}
