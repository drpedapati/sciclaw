package routing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
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

func TestDispatcherSendJobSubmitError_UsesBackgroundJobMessage(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	d := NewDispatcher(mb, nil, nil)
	msg := bus.InboundMessage{Channel: "discord", ChatID: "room-1"}
	d.sendJobSubmitError(context.Background(), msg, context.DeadlineExceeded)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	out, ok := mb.SubscribeOutbound(ctx)
	if !ok {
		t.Fatal("expected outbound submit error")
	}
	if out.Content != "Failed to start background job: context deadline exceeded" {
		t.Fatalf("unexpected job submit error content: %q", out.Content)
	}
}

func TestDispatcherDispatchResolved_StagesInboundMediaBeforePoolDispatch(t *testing.T) {
	created := 0
	handlers := map[string]*fakeHandler{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stub doc"))
	}))
	defer server.Close()

	pool := NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		created++
		h := &fakeHandler{received: make(chan bus.InboundMessage, 1)}
		handlers[target.key()] = h
		return h, nil
	})
	defer pool.Close()

	mb := bus.NewMessageBus()
	defer mb.Close()
	d := NewDispatcher(mb, nil, pool)
	target := LoopTarget{Workspace: t.TempDir()}
	msg := bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "chan-1",
		Content:    "review this",
		SessionKey: "discord:chan-1",
		Media:      []string{server.URL + "/attachments/report.docx"},
		Metadata:   map[string]string{"message_id": "msg-1"},
	}

	d.dispatchResolved(context.Background(), dispatchTask{
		target:   target,
		decision: Decision{Workspace: target.Workspace, Runtime: RuntimeProfile{Mode: config.ModeCloud}},
		msg:      msg,
	})

	h := handlers[target.key()]
	if h == nil {
		t.Fatal("expected handler")
	}
	select {
	case got := <-h.received:
		if len(got.Media) != 1 {
			t.Fatalf("expected staged media path, got %#v", got.Media)
		}
		if !strings.HasPrefix(got.Media[0], filepath.Join(target.Workspace, ".sciclaw", "inbound")) {
			t.Fatalf("expected staged path under workspace, got %q", got.Media[0])
		}
		if _, err := os.Stat(got.Media[0]); err != nil {
			t.Fatalf("expected staged file to exist: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for dispatched message")
	}
	if created != 1 {
		t.Fatalf("expected one handler creation, got %d", created)
	}
}

func TestDispatcherDispatchResolved_StagesInboundMediaBeforeJobSubmit(t *testing.T) {
	mb := bus.NewMessageBus()
	defer mb.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stub doc"))
	}))
	defer server.Close()

	progress := &fakeProgressMessenger{}
	runner := &fakeJobRunner{}
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

	workspace := t.TempDir()
	d := NewDispatcher(mb, nil, nil)
	d.SetJobManager(jm)
	target := LoopTarget{Workspace: workspace, Runtime: RuntimeProfile{Mode: config.ModeCloud}}
	msg := bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "room-1",
		Content:    "please review this",
		SessionKey: "discord:room-1",
		Media:      []string{server.URL + "/attachments/report.docx"},
		Metadata:   map[string]string{"message_id": "msg-stage"},
	}

	d.dispatchResolved(context.Background(), dispatchTask{
		target:   target,
		decision: Decision{Workspace: workspace, Runtime: RuntimeProfile{Mode: config.ModeCloud}},
		msg:      msg,
	})
	waitForNoActiveJobs(t, jm, target.key())

	got := runner.snapshotLastMsg()
	if len(got.Media) != 1 {
		t.Fatalf("expected staged media path, got %#v", got.Media)
	}
	if !strings.HasPrefix(got.Media[0], filepath.Join(workspace, ".sciclaw", "inbound")) {
		t.Fatalf("expected staged media path under workspace, got %q", got.Media[0])
	}
	if _, err := os.Stat(got.Media[0]); err != nil {
		t.Fatalf("expected staged file to exist: %v", err)
	}
}
