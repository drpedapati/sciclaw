package routing

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/phi"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type inboundHandler interface {
	HandleInbound(context.Context, bus.InboundMessage)
}

type LoopTarget struct {
	Workspace string
	Runtime   RuntimeProfile
}

func (t LoopTarget) normalized() LoopTarget {
	return LoopTarget{
		Workspace: filepath.Clean(strings.TrimSpace(t.Workspace)),
		Runtime:   t.Runtime.normalized(),
	}
}

func (t LoopTarget) key() string {
	n := t.normalized()
	return n.Workspace + "\x00" + n.Runtime.Key()
}

type loopFactory func(target LoopTarget) (inboundHandler, error)

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

// LoopSetupFunc is an optional callback invoked on each new AgentLoop created by the pool.
type LoopSetupFunc func(al *agent.AgentLoop)

func NewAgentLoopPool(cfg *config.Config, msgBus *bus.MessageBus, setup ...LoopSetupFunc) *AgentLoopPool {
	return NewAgentLoopPoolWithFactory(func(target LoopTarget) (inboundHandler, error) {
		cloned, err := cloneConfigForTarget(cfg, target)
		if err != nil {
			return nil, err
		}
		loopProvider, err := providers.CreateProvider(cloned)
		if err != nil {
			return nil, fmt.Errorf("creating provider for route target: %w", err)
		}
		al := agent.NewAgentLoop(cloned, msgBus, loopProvider)
		for _, fn := range setup {
			fn(al)
		}
		return al, nil
	})
}

func NewAgentLoopPoolWithFactory(factory loopFactory) *AgentLoopPool {
	return &AgentLoopPool{
		loops:   map[string]*loopEntry{},
		factory: factory,
	}
}

func (p *AgentLoopPool) Dispatch(ctx context.Context, target LoopTarget, msg bus.InboundMessage) error {
	entry, err := p.getOrCreate(target)
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

func (p *AgentLoopPool) getOrCreate(target LoopTarget) (*loopEntry, error) {
	target = target.normalized()
	key := target.key()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("agent loop pool is closed")
	}

	if e, ok := p.loops[key]; ok {
		return e, nil
	}

	handler, err := p.factory(target)
	if err != nil {
		return nil, err
	}

	workerCtx, cancel := context.WithCancel(context.Background())
	entry := &loopEntry{
		handler: handler,
		inbound: make(chan bus.InboundMessage, 64),
		cancel:  cancel,
	}
	p.loops[key] = entry

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

func cloneConfigForTarget(cfg *config.Config, target LoopTarget) (*config.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	target = target.normalized()
	payload, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	cloned := config.DefaultConfig()
	if err := json.Unmarshal(payload, cloned); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cloned.Agents.Defaults.Workspace = target.Workspace
	cloned.Agents.Defaults.Mode = target.Runtime.Mode
	cloned.Agents.Defaults.LocalBackend = target.Runtime.LocalBackend
	cloned.Agents.Defaults.LocalModel = target.Runtime.LocalModel
	cloned.Agents.Defaults.LocalPreset = target.Runtime.LocalPreset

	if err := validateLocalRouteRuntime(cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func validateLocalRouteRuntime(cfg *config.Config) error {
	if cfg.EffectiveMode() != config.ModePhi {
		return nil
	}

	backend := strings.TrimSpace(strings.ToLower(cfg.Agents.Defaults.LocalBackend))
	model := strings.TrimSpace(cfg.Agents.Defaults.LocalModel)
	if backend == "" || model == "" {
		return fmt.Errorf("local PHI route requires local_backend and local_model")
	}

	status := phi.CheckBackend(backend)
	if !status.Installed || !status.Running {
		detail := strings.TrimSpace(status.Error)
		if detail == "" {
			detail = "backend is not available"
		}
		return fmt.Errorf("local backend %q unavailable: %s", backend, detail)
	}

	if backend == config.BackendOllama && !phi.CheckModelReady(model) {
		return fmt.Errorf("local model %q is not available in Ollama", model)
	}
	return nil
}
