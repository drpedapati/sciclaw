package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Dispatcher routes hook events to registered handlers.
type Dispatcher struct {
	mu       sync.RWMutex
	handlers map[Event][]Handler
	audit    AuditSink
}

func NewDispatcher(audit AuditSink) *Dispatcher {
	return &Dispatcher{
		handlers: make(map[Event][]Handler),
		audit:    audit,
	}
}

func (d *Dispatcher) Register(event Event, handler Handler) {
	if handler == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handlers[event] = append(d.handlers[event], handler)
}

func (d *Dispatcher) Dispatch(ctx context.Context, event Event, data Context) []Result {
	d.mu.RLock()
	handlers := append([]Handler(nil), d.handlers[event]...)
	audit := d.audit
	d.mu.RUnlock()

	results := make([]Result, 0, len(handlers))
	for _, handler := range handlers {
		result := runHandler(ctx, handler, event, data)
		results = append(results, result)

		if audit == nil {
			continue
		}
		entry := AuditEntry{
			TurnID:     data.TurnID,
			Event:      event,
			Handler:    handler.Name(),
			Status:     result.Status,
			Message:    result.Message,
			DurationMs: result.DurationMs,
			Timestamp:  time.Now(),
			SessionKey: data.SessionKey,
			Channel:    data.Channel,
			ChatID:     data.ChatID,
			Metadata:   result.Metadata,
		}
		if result.Err != nil {
			entry.Error = result.Err.Error()
		}
		_ = audit.Write(entry)
	}

	return results
}

func (d *Dispatcher) HandlerCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, hs := range d.handlers {
		count += len(hs)
	}
	return count
}

func (d *Dispatcher) EventCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	count := 0
	for _, hs := range d.handlers {
		if len(hs) > 0 {
			count++
		}
	}
	return count
}

func runHandler(ctx context.Context, handler Handler, event Event, data Context) (result Result) {
	start := time.Now()
	defer func() {
		result.DurationMs = time.Since(start).Milliseconds()
		if rec := recover(); rec != nil {
			result = Result{
				Status:     StatusError,
				Message:    "hook panic recovered",
				Err:        fmt.Errorf("panic in hook %s: %v", handler.Name(), rec),
				DurationMs: time.Since(start).Milliseconds(),
			}
		}
		if result.Status == "" {
			result.Status = StatusOK
		}
	}()

	result = handler.Handle(ctx, event, data)
	if result.Status == "" {
		if result.Err != nil {
			result.Status = StatusError
		} else {
			result.Status = StatusOK
		}
	}
	if result.Status == StatusError && result.Err == nil {
		result.Err = fmt.Errorf("hook error: %s", result.Message)
	}
	return result
}
