package hooks

import (
	"context"
	"testing"
)

type testHandler struct {
	name string
	fn   func(context.Context, Event, Context) Result
}

func (h testHandler) Name() string { return h.name }
func (h testHandler) Handle(ctx context.Context, ev Event, data Context) Result {
	return h.fn(ctx, ev, data)
}

type memorySink struct {
	entries []AuditEntry
}

func (m *memorySink) Write(entry AuditEntry) error {
	m.entries = append(m.entries, entry)
	return nil
}

func TestDispatcherDispatchAndAudit(t *testing.T) {
	sink := &memorySink{}
	d := NewDispatcher(sink)
	d.Register(EventBeforeTurn, testHandler{name: "a", fn: func(_ context.Context, _ Event, _ Context) Result {
		return Result{Status: StatusOK, Message: "ok"}
	}})

	results := d.Dispatch(context.Background(), EventBeforeTurn, Context{TurnID: "t1", SessionKey: "s1"})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if len(sink.entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(sink.entries))
	}
	if sink.entries[0].TurnID != "t1" {
		t.Fatalf("turn id = %q, want t1", sink.entries[0].TurnID)
	}
}

func TestDispatcherFailOpenOnPanic(t *testing.T) {
	d := NewDispatcher(nil)
	d.Register(EventBeforeTurn, testHandler{name: "panic", fn: func(_ context.Context, _ Event, _ Context) Result {
		panic("boom")
	}})

	results := d.Dispatch(context.Background(), EventBeforeTurn, Context{TurnID: "t2"})
	if len(results) != 1 {
		t.Fatalf("results len = %d, want 1", len(results))
	}
	if results[0].Status != StatusError {
		t.Fatalf("status = %q, want error", results[0].Status)
	}
	if results[0].Err == nil {
		t.Fatalf("expected panic converted to error")
	}
}
