package routing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

type fakeProgressMessenger struct {
	mu       sync.Mutex
	calls    []string
	nextID   string
	editFail bool
}

func (m *fakeProgressMessenger) SendOrEditProgress(_ context.Context, channelName, chatID, messageID, content string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, fmt.Sprintf("%s|%s|%s", channelName, chatID, content))
	if messageID != "" && m.editFail {
		return "", fmt.Errorf("edit failed")
	}
	if messageID != "" {
		return messageID, nil
	}
	if m.nextID == "" {
		m.nextID = "progress-1"
	}
	return m.nextID, nil
}

type fakeJobRunner struct {
	started chan struct{}
	block   chan struct{}
}

func (r *fakeJobRunner) HandleInbound(context.Context, bus.InboundMessage) {}

func (r *fakeJobRunner) RunJob(ctx context.Context, msg bus.InboundMessage, onProgress func(phase, detail string)) (string, error) {
	if r.started != nil {
		close(r.started)
	}
	if onProgress != nil {
		onProgress("thinking", "Thinking")
	}
	if r.block != nil {
		select {
		case <-r.block:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "done", nil
}

func TestJobManagerBusyStatusAndCancel(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	runner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         1,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return runner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	msg := bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "please do work"}
	if err := jm.Submit(context.Background(), target, msg); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job start")
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "another request"}); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "already working in the background") {
		t.Fatalf("expected busy guidance, got %q", out.Content)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "status"}); err != nil {
		t.Fatalf("status submit: %v", err)
	}
	out = mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "Status:") {
		t.Fatalf("expected status reply, got %q", out.Content)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "cancel"}); err != nil {
		t.Fatalf("cancel submit: %v", err)
	}
	out = mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "stopping the background job") {
		t.Fatalf("expected cancel ack, got %q", out.Content)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		jm.mu.Lock()
		_, ok := jm.active[target.key()]
		jm.mu.Unlock()
		if !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for cancelled job cleanup")
}

func TestJobManagerWritesProgressAndFinalState(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         1,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return &fakeJobRunner{}, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "do it"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		progress.mu.Lock()
		callCount := len(progress.calls)
		progress.mu.Unlock()
		if callCount >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	progress.mu.Lock()
	defer progress.mu.Unlock()
	if len(progress.calls) < 2 {
		t.Fatalf("expected at least 2 progress updates, got %d", len(progress.calls))
	}
	if !strings.Contains(progress.calls[len(progress.calls)-1], "Done. The full reply is below.") {
		t.Fatalf("expected final progress update, got %q", progress.calls[len(progress.calls)-1])
	}
}

func mustNextOutbound(t *testing.T, mb *bus.MessageBus) bus.OutboundMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound message")
	}
	return msg
}

func filepathJoin(elem ...string) string {
	if len(elem) == 0 {
		return ""
	}
	out := elem[0]
	for _, part := range elem[1:] {
		if strings.HasSuffix(out, "/") {
			out += part
		} else {
			out += "/" + part
		}
	}
	return out
}
