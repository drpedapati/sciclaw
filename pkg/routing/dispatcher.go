package routing

import (
	"context"
	"fmt"
	"sync"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type Dispatcher struct {
	bus      *bus.MessageBus
	resolver *Resolver
	pool     *AgentLoopPool
	mu       sync.RWMutex
}

func NewDispatcher(messageBus *bus.MessageBus, resolver *Resolver, pool *AgentLoopPool) *Dispatcher {
	return &Dispatcher{
		bus:      messageBus,
		resolver: resolver,
		pool:     pool,
	}
}

func (d *Dispatcher) Run(ctx context.Context) error {
	for {
		msg, ok := d.bus.ConsumeInbound(ctx)
		if !ok {
			return nil
		}

		resolver := d.getResolver()
		decision := resolver.Resolve(msg)
		d.logDecision(decision)

		if !decision.Allowed {
			d.sendBlockNotice(ctx, msg, decision)
			continue
		}

		routed := msg
		routed.SessionKey = decision.SessionKey
		if err := d.pool.Dispatch(ctx, decision.Workspace, routed); err != nil {
			logger.ErrorCF("routing", "route_invalid", map[string]interface{}{
				"channel":   msg.Channel,
				"chat_id":   msg.ChatID,
				"sender_id": msg.SenderID,
				"workspace": decision.Workspace,
				"reason":    err.Error(),
			})
			d.sendOperationalError(ctx, msg)
		}
	}
}

func (d *Dispatcher) ReplaceResolver(resolver *Resolver) {
	if resolver == nil {
		return
	}
	d.mu.Lock()
	d.resolver = resolver
	d.mu.Unlock()
}

func (d *Dispatcher) getResolver() *Resolver {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.resolver
}

func (d *Dispatcher) logDecision(decision Decision) {
	fields := map[string]interface{}{
		"channel":       decision.Channel,
		"chat_id":       decision.ChatID,
		"sender_id":     decision.SenderID,
		"workspace":     decision.Workspace,
		"reason":        decision.Reason,
		"mapping_label": decision.MappingLabel,
		"session_key":   decision.SessionKey,
		"allowed":       decision.Allowed,
	}
	logger.InfoCF("routing", decision.Event, fields)
}

func (d *Dispatcher) sendBlockNotice(ctx context.Context, msg bus.InboundMessage, decision Decision) {
	if decision.Event == EventRouteMentionSkip {
		return
	}
	if constants.IsInternalChannel(msg.Channel) {
		return
	}

	content := "This chat is not mapped to a workspace yet."
	switch decision.Event {
	case EventRouteDeny:
		content = "You are not authorized for this chat mapping."
	case EventRouteInvalid:
		content = "This chat mapping is invalid right now (workspace unavailable). Ask an operator to run `sciclaw routing validate`."
	default:
		content = fmt.Sprintf(
			"This chat is not mapped to a workspace yet.\n\nOperator setup hint:\n  sciclaw routing add --channel %s --chat-id %s --workspace /absolute/path --allow <sender_id>",
			msg.Channel,
			msg.ChatID,
		)
	}

	d.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: content,
	})
}

func (d *Dispatcher) sendOperationalError(ctx context.Context, msg bus.InboundMessage) {
	if constants.IsInternalChannel(msg.Channel) {
		return
	}
	d.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: "Routing failed for this request due to an internal configuration error.",
	})
}
