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
	mu                sync.Mutex
	calls             []string
	messages          []fakeProgressCall
	nextID            string
	editReplacementID string
	idSeq             int
	sendFail          bool
	editFail          bool
}

type fakeProgressCall struct {
	channel   string
	chatID    string
	messageID string
	content   string
	title     string
}

func (m *fakeProgressMessenger) SendOrEditProgress(_ context.Context, channelName, chatID, messageID string, msg bus.OutboundMessage) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	title := ""
	if len(msg.Embeds) > 0 {
		title = msg.Embeds[0].Title
	}
	m.calls = append(m.calls, fmt.Sprintf("%s|%s|%s|%s", channelName, chatID, msg.Content, title))
	m.messages = append(m.messages, fakeProgressCall{
		channel:   channelName,
		chatID:    chatID,
		messageID: messageID,
		content:   msg.Content,
		title:     title,
	})
	if messageID == "" && m.sendFail {
		return "", fmt.Errorf("send failed")
	}
	if messageID != "" && m.editFail {
		return "", fmt.Errorf("edit failed")
	}
	if messageID != "" && m.editReplacementID != "" {
		id := m.editReplacementID
		m.editReplacementID = ""
		return id, nil
	}
	if messageID != "" {
		return messageID, nil
	}
	if m.nextID == "" {
		m.idSeq++
		m.nextID = fmt.Sprintf("progress-%d", m.idSeq)
	}
	id := m.nextID
	m.nextID = ""
	return id, nil
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
		select {
		case <-started:
		default:
			close(started)
		}
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
	if !progressHas(progress, "Reply `status` · `force` to move next · `cancel` to drop from queue") {
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
	if !strings.Contains(out.Content, "Reply on that job card with `status` or `cancel`") || !strings.Contains(out.Content, "place it in the main queue") {
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

func TestJobManagerQueuedSubmissionRollbackOnProgressFailure(t *testing.T) {
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
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "first job"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first job start")
	}

	progress.sendFail = true
	err = jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "second job"})
	if err == nil || !strings.Contains(err.Error(), "send queued status") {
		t.Fatalf("expected queued progress send failure, got %v", err)
	}

	jm.mu.Lock()
	activeSet := jm.active[target.key()]
	queued := 0
	if activeSet != nil {
		queued = len(activeSet.queuedWrites)
	}
	jm.mu.Unlock()
	if queued != 0 {
		t.Fatalf("expected queued job rollback, still have %d queued jobs", queued)
	}
	records, err := jm.store.All()
	if err != nil {
		t.Fatalf("store.All: %v", err)
	}
	for _, record := range records {
		if record.AskSummary == "second job" && record.State == JobStateQueued {
			t.Fatalf("expected failed queued submit to be rolled back, found stale queued record: %#v", record)
		}
	}

	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.runs != 1 {
		t.Fatalf("expected only original running job to execute, runs=%d", runner.runs)
	}
}

func TestJobManagerReplyToQueuedCardCancelsJob(t *testing.T) {
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
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "first job"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first job start")
	}

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "second job"}); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	waitForQueuedWrite(t, jm, target.key(), 1)
	jm.mu.Lock()
	queuedStatusID := jm.active[target.key()].queuedWrites[0].snapshot().StatusMessageID
	jm.mu.Unlock()
	progress.editReplacementID = "progress-replacement"

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "cancel",
		Metadata: map[string]string{"reply_message_id": queuedStatusID},
	}); err != nil {
		t.Fatalf("reply cancel submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "removed queued job") {
		t.Fatalf("expected queued cancel ack, got %q", out.Content)
	}
	if !progressHas(progress, "This job was cancelled.") {
		t.Fatalf("expected queued progress card to be updated to cancelled, calls=%v", progress.calls)
	}
	cancelled := findJobRecordByAskSummary(t, jm, "second job")
	if cancelled.State != JobStateCancelled {
		t.Fatalf("expected cancelled state, got %#v", cancelled)
	}
	if cancelled.StatusMessageID != "progress-replacement" {
		t.Fatalf("expected stale-safe card replacement id, got %q", cancelled.StatusMessageID)
	}

	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())
}

func TestJobManagerReplyToQueuedCardForcesJob(t *testing.T) {
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
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "first job"}); err != nil {
		t.Fatalf("first submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first job start")
	}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "second job"}); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "third job"}); err != nil {
		t.Fatalf("third submit: %v", err)
	}
	waitForQueuedWrite(t, jm, target.key(), 2)

	jm.mu.Lock()
	second := jm.active[target.key()].queuedWrites[0].snapshot()
	third := jm.active[target.key()].queuedWrites[1].snapshot()
	jm.mu.Unlock()

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "room-1",
		Content:  "force",
		Metadata: map[string]string{"reply_message_id": third.StatusMessageID},
	}); err != nil {
		t.Fatalf("reply force submit: %v", err)
	}
	out := mustNextOutbound(t, mb)
	if !strings.Contains(out.Content, "moved job "+third.ShortID+" to the front of the main queue") {
		t.Fatalf("expected force ack for %s, got %q", third.ShortID, out.Content)
	}

	jm.mu.Lock()
	queued := jm.active[target.key()].queuedWrites
	if len(queued) != 2 {
		jm.mu.Unlock()
		t.Fatalf("expected two queued jobs after force, got %d", len(queued))
	}
	if got := queued[0].snapshot().ID; got != third.ID {
		jm.mu.Unlock()
		t.Fatalf("expected forced job %s at front, got %s", third.ID, got)
	}
	if got := queued[1].snapshot().ID; got != second.ID {
		jm.mu.Unlock()
		t.Fatalf("expected original front job %s to move back, got %s", second.ID, got)
	}
	jm.mu.Unlock()
	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())
}

func TestJobManagerRestartInterruptsOnlyRunningJobsAndPreservesQueuedBacklog(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	storePath := filepathJoin(t.TempDir(), "jobs.json")
	store, err := newJobStore(storePath)
	if err != nil {
		t.Fatalf("newJobStore: %v", err)
	}

	target := LoopTarget{Workspace: "/tmp/work", Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	running := JobRecord{
		ID:         "job-1000-7",
		ShortID:    formatShortJobRef(7),
		Channel:    "discord",
		ChatID:     "room-1",
		Workspace:  target.Workspace,
		RuntimeKey: target.Runtime.Key(),
		Runtime:    target.Runtime,
		TargetKey:  target.key(),
		Class:      JobClassWrite,
		State:      JobStateRunning,
		Phase:      "thinking",
		Detail:     "Thinking",
		AskSummary: "running job",
		Message: bus.InboundMessage{
			Channel:    "discord",
			ChatID:     "room-1",
			Content:    "running job",
			SessionKey: "discord:room-1@abc123",
		},
		StartedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
		UpdatedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
	}
	queued := JobRecord{
		ID:         "job-1000-8",
		ShortID:    formatShortJobRef(8),
		Channel:    "discord",
		ChatID:     "room-1",
		Workspace:  target.Workspace,
		RuntimeKey: target.Runtime.Key(),
		Runtime:    target.Runtime,
		TargetKey:  target.key(),
		Class:      JobClassWrite,
		State:      JobStateQueued,
		Phase:      "queued",
		Detail:     "Queued in main lane · behind 00007 · position 1",
		AskSummary: "resume me",
		Message: bus.InboundMessage{
			Channel:    "discord",
			ChatID:     "room-1",
			Content:    "resume me",
			SessionKey: "discord:room-1@abc123",
		},
		StartedAt: time.Now().Add(-time.Minute).UnixMilli(),
		UpdatedAt: time.Now().Add(-time.Minute).UnixMilli(),
	}
	if err := store.Save(running); err != nil {
		t.Fatalf("Save running: %v", err)
	}
	if err := store.Save(queued); err != nil {
		t.Fatalf("Save queued: %v", err)
	}

	progress := &fakeProgressMessenger{}
	runner := &fakeJobRunner{started: make(chan struct{}), block: make(chan struct{})}
	jm, err := NewJobManager(storePath, config.JobsConfig{
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

	select {
	case <-runner.started:
		t.Fatal("queued backlog should remain queued on restart until new work resumes it")
	case <-time.After(150 * time.Millisecond):
	}

	records, err := jm.store.All()
	if err != nil {
		t.Fatalf("store.All: %v", err)
	}
	gotRunning := findRecordByID(t, records, running.ID)
	if gotRunning.State != JobStateInterrupted {
		t.Fatalf("expected running job to become interrupted, got %#v", gotRunning)
	}
	gotQueued := findRecordByID(t, records, queued.ID)
	if gotQueued.State != JobStateQueued {
		t.Fatalf("expected queued job to remain queued, got %#v", gotQueued)
	}
	if !strings.Contains(gotQueued.Detail, "next up") {
		t.Fatalf("expected preserved queue to be reindexed as next up, got %#v", gotQueued)
	}

	jm.mu.Lock()
	activeSet := jm.active[target.key()]
	if activeSet == nil || activeSet.write != nil || len(activeSet.queuedWrites) != 1 {
		jm.mu.Unlock()
		t.Fatalf("expected preserved backlog without active write, got %#v", activeSet)
	}
	if got := activeSet.queuedWrites[0].snapshot().ID; got != queued.ID {
		jm.mu.Unlock()
		t.Fatalf("expected queued backlog to preserve job %s, got %s", queued.ID, got)
	}
	jm.mu.Unlock()

	if err := jm.Submit(context.Background(), target, bus.InboundMessage{Channel: "discord", ChatID: "room-1", Content: "later job"}); err != nil {
		t.Fatalf("resume submit: %v", err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("expected preserved queue head to resume when new write traffic arrives")
	}
	waitForQueuedWrite(t, jm, target.key(), 1)

	jm.mu.Lock()
	activeSet = jm.active[target.key()]
	if activeSet == nil || activeSet.write == nil {
		jm.mu.Unlock()
		t.Fatal("expected resumed write to occupy main lane")
	}
	if got := activeSet.write.snapshot().AskSummary; got != "resume me" {
		jm.mu.Unlock()
		t.Fatalf("expected preserved queued job to resume first, got %q", got)
	}
	if got := activeSet.queuedWrites[0].snapshot().AskSummary; got != "later job" {
		jm.mu.Unlock()
		t.Fatalf("expected new submit to queue behind preserved backlog, got %q", got)
	}
	jm.mu.Unlock()

	close(runner.block)
	waitForNoActiveJobs(t, jm, target.key())
}

func TestJobManagerRestoresCounterFromStore(t *testing.T) {
	storePath := filepathJoin(t.TempDir(), "jobs.json")
	store, err := newJobStore(storePath)
	if err != nil {
		t.Fatalf("newJobStore: %v", err)
	}
	if err := store.Save(JobRecord{
		ID:        "job-1000-42",
		ShortID:   formatShortJobRef(42),
		State:     JobStateDone,
		Channel:   "discord",
		ChatID:    "room-1",
		StartedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	mb := bus.NewMessageBus()
	defer mb.Close()
	jm, err := NewJobManager(storePath, config.JobsConfig{
		Enabled:               true,
		MaxConcurrent:         1,
		ProgressUpdateSeconds: 1,
		DiscordAsyncDefault:   true,
	}, mb, &fakeProgressMessenger{}, func(target LoopTarget) (JobRunner, error) {
		return &fakeJobRunner{}, nil
	})
	if err != nil {
		t.Fatalf("NewJobManager: %v", err)
	}

	gotID, gotShort := jm.nextJobID()
	if !strings.HasSuffix(gotID, "-43") {
		t.Fatalf("expected counter to resume at 43, got %q", gotID)
	}
	if gotShort != formatShortJobRef(43) {
		t.Fatalf("expected short ref %q, got %q", formatShortJobRef(43), gotShort)
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

func findJobRecordByAskSummary(t *testing.T, jm *JobManager, askSummary string) JobRecord {
	t.Helper()
	records, err := jm.store.All()
	if err != nil {
		t.Fatalf("store.All: %v", err)
	}
	for _, record := range records {
		if record.AskSummary == askSummary {
			return record
		}
	}
	t.Fatalf("expected job record with ask summary %q", askSummary)
	return JobRecord{}
}

func findRecordByID(t *testing.T, records []JobRecord, id string) JobRecord {
	t.Helper()
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	t.Fatalf("expected job record %q in %#v", id, records)
	return JobRecord{}
}
