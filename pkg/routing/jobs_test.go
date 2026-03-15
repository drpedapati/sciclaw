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
	started := r.started
	block := r.block
	r.mu.Unlock()
	if started != nil {
		close(started)
	}
	if onProgress != nil {
		onProgress("thinking", "Thinking")
	}
	if block != nil {
		select {
		case <-block:
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
	waitForQueuedWrite(t, jm, target.key(), 1)
	jm.mu.Lock()
	queuedID := jm.active[target.key()].queuedWrites[0].snapshot().ShortID
	jm.mu.Unlock()
	if len(queuedID) != 5 {
		t.Fatalf("expected stable 5-char queued ref, got %q", queuedID)
	}
	if !progressHas(progress, queuedID) {
		t.Fatalf("expected queued progress card for %s, calls=%v", queuedID, progress.calls)
	}
	if !progressHas(progress, "force "+queuedID) || !progressHas(progress, "drop from queue") {
		t.Fatalf("expected queued progress card to include force/cancel hint for %s, calls=%v", queuedID, progress.calls)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "status",
		Metadata: map[string]string{"has_direct_mention": "true"},
	}); err != nil {
		t.Fatalf("status submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "sciClaw jobs in this workspace") || !strings.Contains(out.Content, queuedID) {
		t.Fatalf("expected multi-job status reply, got %q", out.Content)
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "cancel " + queuedID,
		Metadata: map[string]string{"has_direct_mention": "true"},
	}); err != nil {
		t.Fatalf("cancel submit: %v", err)
	}
	out = mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "removed queued job "+queuedID) {
		t.Fatalf("expected queued cancel ack, got %q", out.Content)
	}

	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.runs != 1 {
		t.Fatalf("expected queued job to stay cancelled, runs=%d", runner.runs)
	}
}

func TestJobManagerAllowsBTWOverlap(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	writeRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	readRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         2,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
		AllowBTWDuringWrite:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetSideLaneResolver(func(target LoopTarget) (JobRunner, error) {
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
		Content: "/btw collect some images of related probes",
	}); err != nil {
		t.Fatalf("/btw submit: %v", err)
	}
	select {
	case <-readRunner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for /btw job start")
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
		Enabled:               true,
		MaxConcurrent:         2,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
		AllowBTWDuringWrite:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetSideLaneResolver(func(target LoopTarget) (JobRunner, error) {
		return readRunner, nil
	})

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	_ = jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "please update the workspace files"})
	<-writeRunner.started
	_ = jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "/btw collect some images of related probes"})
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
	if !strings.Contains(out.Content, "sciClaw jobs in this workspace") {
		t.Fatalf("expected multiple job list, got %q", out.Content)
	}
	if !strings.Contains(out.Content, "/btw lane") {
		t.Fatalf("expected job list to label /btw lane, got %q", out.Content)
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
	if !strings.Contains(progress.calls[0], "sciClaw ·") || !strings.Contains(progress.calls[0], "> do it") || !strings.Contains(progress.calls[0], "<t:") {
		t.Fatalf("expected progress metadata, got %q", progress.calls[0])
	}
	if !strings.Contains(progress.calls[len(progress.calls)-1], "Done. Reply below.") {
		t.Fatalf("expected final progress update, got %q", progress.calls[len(progress.calls)-1])
	}
}

func TestJobManagerBTWBusyMessageExplainsQueueOptions(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	writeRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	btwRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         2,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
		AllowBTWDuringWrite:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetSideLaneResolver(func(target LoopTarget) (JobRunner, error) {
		return btwRunner, nil
	})

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel: "discord",
		ChatID:  "room-1",
		Content: "/btw collect some images of related probes",
	}); err != nil {
		t.Fatalf("first /btw submit: %v", err)
	}
	select {
	case <-btwRunner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first /btw job start")
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel: "discord",
		ChatID:  "room-1",
		Content: "/btw collect more probe images",
	}); err != nil {
		t.Fatalf("second /btw submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "The /btw lane is already in use") {
		t.Fatalf("expected /btw busy message, got %q", out.Content)
	}
	if !strings.Contains(out.Content, "cancel <job-id>") || !strings.Contains(out.Content, "place it in the main queue") {
		t.Fatalf("expected /btw busy message to explain cancel or main-queue fallback, got %q", out.Content)
	}

	close(btwRunner.block)
	waitForNoActiveJobs(t, jm, target.key())
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

func TestJobManagerOrdinaryResearchTurnQueuesMainLane(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	progress := &fakeProgressMessenger{}
	writeRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	readRunner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(filepathJoin(t.TempDir(), "jobs.json"), config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         2,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
		AllowBTWDuringWrite:   true,
	}, mb, progress, func(target LoopTarget) (JobRunner, error) {
		return writeRunner, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}
	jm.SetSideLaneResolver(func(target LoopTarget) (JobRunner, error) {
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
		t.Fatalf("ordinary research submit: %v", err)
	}
	waitForQueuedWrite(t, jm, target.key(), 1)

	select {
	case <-readRunner.started:
		t.Fatal("ordinary research turn should not start the explicit /btw lane implicitly")
	case <-time.After(100 * time.Millisecond):
	}

	writeRunner.mu.Lock()
	if writeRunner.runs != 1 {
		writeRunner.mu.Unlock()
		t.Fatalf("expected only first write to be running, runs=%d", writeRunner.runs)
	}
	writeRunner.mu.Unlock()

	nextBlock := make(chan struct{})
	nextStarted := make(chan struct{})
	writeRunner.started = nextStarted
	oldBlock := writeRunner.block
	writeRunner.block = nextBlock
	close(oldBlock)

	select {
	case <-nextStarted:
	case <-time.After(time.Second):
		t.Fatal("queued main-lane job did not start after the first write completed")
	}

	close(nextBlock)
	waitForNoActiveJobs(t, jm, target.key())
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

func progressHas(progress *fakeProgressMessenger, needle string) bool {
	progress.mu.Lock()
	defer progress.mu.Unlock()
	for _, call := range progress.calls {
		if strings.Contains(call, needle) {
			return true
		}
	}
	return false
}

func waitForQueuedWrite(t *testing.T, jm *JobManager, targetKey string, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		jm.mu.Lock()
		activeSet := jm.active[targetKey]
		got := 0
		if activeSet != nil {
			got = len(activeSet.queuedWrites)
		}
		jm.mu.Unlock()
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d queued writes for %s", want, targetKey)
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
