package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type inboundHandler interface {
	HandleInbound(context.Context, bus.InboundMessage)
}

type loopFactory func(workspace string) (inboundHandler, error)

type loopEntry struct {
	handler inboundHandler
	inbound chan bus.InboundMessage
	cancel  context.CancelFunc
}

// AgentLoopPool keeps one agent loop per workspace and reuses it.
type AgentLoopPool struct {
	mu      sync.Mutex
	loops   map[string]*loopEntry
	closed  bool
	wg      sync.WaitGroup
	factory loopFactory
}

func NewAgentLoopPool(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider) *AgentLoopPool {
	return NewAgentLoopPoolWithFactory(func(workspace string) (inboundHandler, error) {
		cloned, err := cloneConfigForWorkspace(cfg, workspace)
		if err != nil {
			return nil, err
		}
		return agent.NewAgentLoop(cloned, msgBus, provider), nil
	})
}

func NewAgentLoopPoolWithFactory(factory loopFactory) *AgentLoopPool {
	return &AgentLoopPool{
		loops:   map[string]*loopEntry{},
		factory: factory,
	}
}

func (p *AgentLoopPool) Dispatch(ctx context.Context, workspace string, msg bus.InboundMessage) error {
	entry, err := p.getOrCreate(workspace)
	if err != nil {
		return err
	}

	select {
	case entry.inbound <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *AgentLoopPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.loops)
}

func (p *AgentLoopPool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true

	entries := make([]*loopEntry, 0, len(p.loops))
	for _, e := range p.loops {
		entries = append(entries, e)
	}
	p.mu.Unlock()

	for _, e := range entries {
		e.cancel()
	}
	p.wg.Wait()
}

func (p *AgentLoopPool) getOrCreate(workspace string) (*loopEntry, error) {
	workspace = filepath.Clean(workspace)

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("agent loop pool is closed")
	}

	if e, ok := p.loops[workspace]; ok {
		return e, nil
	}

	handler, err := p.factory(workspace)
	if err != nil {
		return nil, err
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	entry := &loopEntry{
		handler: handler,
		inbound: make(chan bus.InboundMessage, 64),
		cancel:  cancel,
	}
	p.loops[workspace] = entry

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			select {
			case <-workerCtx.Done():
				return
			case msg := <-entry.inbound:
				entry.handler.HandleInbound(workerCtx, msg)
			}
		}
	}()

	return entry, nil
}

func cloneConfigForWorkspace(cfg *config.Config, workspace string) (*config.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	cloned := config.DefaultConfig()
	if err := json.Unmarshal(payload, cloned); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cloned.Agents.Defaults.Workspace = workspace
	return cloned, nil
}
