package routing

import (
	"context"
	"sync"
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
	pool := NewAgentLoopPoolWithFactory(func(_ LoopTarget) (inboundHandler, error) {
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
	pool := NewAgentLoopPoolWithFactory(func(_ LoopTarget) (inboundHandler, error) {
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
	pool := NewAgentLoopPoolWithFactory(func(_ LoopTarget) (inboundHandler, error) {
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

func TestAgentLoopPool_SlowFactoryDoesNotHoldMutex(t *testing.T) {
	var (
		createdMu sync.Mutex
		created   int
		startOnce sync.Once
	)
	factoryStarted := make(chan struct{})
	releaseFactory := make(chan struct{})

	pool := NewAgentLoopPoolWithFactory(func(_ LoopTarget) (inboundHandler, error) {
		createdMu.Lock()
		created++
		createdMu.Unlock()
		startOnce.Do(func() { close(factoryStarted) })
		<-releaseFactory
		return &fakeHandler{received: make(chan bus.InboundMessage, 8)}, nil
	})
	defer pool.Close()

	target := LoopTarget{Workspace: "/tmp/ws-a"}
	errCh := make(chan error, 2)
	go func() {
		errCh <- pool.Dispatch(context.Background(), target, bus.InboundMessage{SessionKey: "s1"})
	}()
	go func() {
		errCh <- pool.Dispatch(context.Background(), target, bus.InboundMessage{SessionKey: "s2"})
	}()

	select {
	case <-factoryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for factory start")
	}

	sizeDone := make(chan struct{})
	go func() {
		_ = pool.Size()
		close(sizeDone)
	}()
	select {
	case <-sizeDone:
	case <-time.After(time.Second):
		t.Fatal("pool mutex is blocked while factory is in progress")
	}

	close(releaseFactory)
	for i := 0; i < 2; i++ {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatalf("dispatch %d failed: %v", i+1, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for dispatch %d", i+1)
		}
	}

	createdMu.Lock()
	gotCreated := created
	createdMu.Unlock()
	if gotCreated != 1 {
		t.Fatalf("expected exactly 1 handler creation, got %d", gotCreated)
	}
	if pool.Size() != 1 {
		t.Fatalf("expected pool size 1, got %d", pool.Size())
	}
}

func TestAgentLoopPool_FactoryPanicDoesNotLeakInflight(t *testing.T) {
	var (
		mu    sync.Mutex
		calls int
	)

	pool := NewAgentLoopPoolWithFactory(func(_ LoopTarget) (inboundHandler, error) {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			panic("factory boom")
		}
		return &fakeHandler{received: make(chan bus.InboundMessage, 8)}, nil
	})
	defer pool.Close()

	target := LoopTarget{Workspace: "/tmp/ws-panic"}
	panicDone := make(chan struct{})
	go func() {
		defer close(panicDone)
		defer func() {
			_ = recover()
		}()
		_ = pool.Dispatch(context.Background(), target, bus.InboundMessage{SessionKey: "panic"})
	}()

	select {
	case <-panicDone:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for panic dispatch to return")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pool.Dispatch(ctx, target, bus.InboundMessage{SessionKey: "ok"}); err != nil {
		t.Fatalf("dispatch after panic failed: %v", err)
	}

	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls < 2 {
		t.Fatalf("expected factory to be retried after panic, got %d calls", gotCalls)
	}
	if pool.Size() != 1 {
		t.Fatalf("expected pool size 1 after retry, got %d", pool.Size())
	}
}
