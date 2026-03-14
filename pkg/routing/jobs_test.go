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

func (m *fakeProgressMessenger) SendOrEditProgress(_ context.Context, channelName, chatID, messageID string, msg bus.OutboundMessage) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	title := ""
	if len(msg.Embeds) > 0 {
		title = msg.Embeds[0].Title
	}
	m.calls = append(m.calls, fmt.Sprintf("%s|%s|%s|%s", channelName, chatID, msg.Content, title))
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
	runs    int
	mu      sync.Mutex
}

func (r *fakeJobRunner) HandleInbound(context.Context, bus.InboundMessage) {}

func (r *fakeJobRunner) RunJob(ctx context.Context, msg bus.InboundMessage, onProgress func(phase, detail string)) (string, error) {
	r.mu.Lock()
	r.runs++
	r.mu.Unlock()
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
	if !strings.Contains(out.Content, "**J") || !strings.Contains(out.Content, "Another write-capable job will wait until this one finishes.") {
		t.Fatalf("expected busy guidance, got %q", out.Content)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "status",
		Metadata: map[string]string{"has_direct_mention": "true"},
	}); err != nil {
		t.Fatalf("status submit: %v", err)
	}
	out = mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "**J") || !strings.Contains(out.Content, "> please do work") {
		t.Fatalf("expected status reply, got %q", out.Content)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "cancel",
		Metadata: map[string]string{"has_direct_mention": "true"},
	}); err != nil {
		t.Fatalf("cancel submit: %v", err)
	}
	out = mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "stopping job J") {
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

func TestJobManagerAllowsExternalReadOnlyOverlap(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	writeRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	readRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:                  true,
		MaxConcurrent:            2,
		ProgressUpdateSeconds:    1,
		DiscordAsyncDefault:      true,
		AllowReadOnlyDuringWrite: true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetExternalReadOnlyResolver(func(target LoopTarget) (JobRunner, error) {
		return readRunner, nil
	})

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel: "discord",
		ChatID:  "room-1",
		Content: "please update the workspace files",
	}); err != nil {
		t.Fatalf("write submit: %v", err)
	}
	select {
	case <-writeRunner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for write job start")
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel: "discord",
		ChatID:  "room-1",
		Content: "collect some images of related probes",
	}); err != nil {
		t.Fatalf("readonly submit: %v", err)
	}
	select {
	case <-readRunner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for readonly job start")
	}

	close(writeRunner.block)
	close(readRunner.block)
	waitForNoActiveJobs(t, jm, target.key())
}

func TestJobManagerPlainStatusWithoutDirectionStartsNormalJob(t *testing.T) {
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
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel: "discord",
		ChatID:  "room-1",
		Content: "status",
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("expected plain status to start a normal job when not bot-directed")
	}

	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())
}

func TestJobManagerStatusListsMultipleJobs(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	writeRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	readRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:                  true,
		MaxConcurrent:            2,
		ProgressUpdateSeconds:    1,
		DiscordAsyncDefault:      true,
		AllowReadOnlyDuringWrite: true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetExternalReadOnlyResolver(func(target LoopTarget) (JobRunner, error) {
		return readRunner, nil
	})

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	_ = jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "please update the workspace files"})
	<-writeRunner.started
	_ = jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "collect some images of related probes"})
	<-readRunner.started

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "status",
		Metadata: map[string]string{"has_direct_mention": "true"},
	}); err != nil {
		t.Fatalf("status submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "Multiple sciClaw jobs are active here") {
		t.Fatalf("expected multiple job list, got %q", out.Content)
	}

	close(writeRunner.block)
	close(readRunner.block)
	waitForNoActiveJobs(t, jm, target.key())
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
	if !strings.Contains(progress.calls[0], "**J") || !strings.Contains(progress.calls[0], "> do it") || !strings.Contains(progress.calls[0], "<t:") {
		t.Fatalf("expected progress metadata, got %q", progress.calls[0])
	}
	if !strings.Contains(progress.calls[len(progress.calls)-1], "Done. The full reply is below.") {
		t.Fatalf("expected final progress update, got %q", progress.calls[len(progress.calls)-1])
	}
}

func TestJobManagerRefreshesProgressDuringLongRunningPhase(t *testing.T) {
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
	jm.progressInterval = 50 * time.Millisecond

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "do slow work"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}

	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for job start")
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMatchingProgress(progress, "**Thinking**") >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if got := countMatchingProgress(progress, "**Thinking**"); got < 2 {
		t.Fatalf("expected repeated thinking progress updates, got %d calls: %#v", got, progress.calls)
	}

	close(runner.block)

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		jm.mu.Lock()
		_, ok := jm.active[target.key()]
		jm.mu.Unlock()
		if !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for long-running job cleanup")
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

func countMatchingProgress(progress *fakeProgressMessenger, needle string) int {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	count := 0
	for _, call := range progress.calls {
		if strings.Contains(call, needle) {
			count++
		}
	}
	return count
}

func waitForNoActiveJobs(t *testing.T, jm *JobManager, targetKey string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		jm.mu.Lock()
		_, ok := jm.active[targetKey]
		jm.mu.Unlock()
		if !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for active jobs to clear for %s", targetKey)
}
