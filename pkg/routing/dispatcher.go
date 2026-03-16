package routing

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/constants"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type Dispatcher struct {
	bus      *bus.MessageBus
	resolver *Resolver
	pool     *AgentLoopPool
	jobs     *JobManager
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
		target := LoopTarget{
			Workspace: decision.Workspace,
			Runtime:   decision.Runtime,
		}
		if err := prepareInboundMedia(target.Workspace, &routed); err != nil {
			logger.ErrorCF("routing", "inbound_media_stage_failed", map[string]interface{}{
				"channel":   msg.Channel,
				"chat_id":   msg.ChatID,
				"sender_id": msg.SenderID,
				"workspace": decision.Workspace,
				"reason":    err.Error(),
			})
			d.sendJobSubmitError(ctx, msg, fmt.Errorf("failed to stage inbound attachments: %w", err))
			continue
		}
		if jobs := d.getJobManager(); jobs != nil && jobs.ShouldHandleChannel(routed.Channel) {
			if err := jobs.Submit(ctx, target, routed); err != nil {
				logger.ErrorCF("routing", "job_submit_failed", map[string]interface{}{
					"channel":   msg.Channel,
					"chat_id":   msg.ChatID,
					"sender_id": msg.SenderID,
					"workspace": decision.Workspace,
					"mode":      decision.Runtime.Mode,
					"backend":   decision.Runtime.LocalBackend,
					"model":     decision.Runtime.LocalModel,
					"reason":    err.Error(),
				})
				d.sendJobSubmitError(ctx, msg, err)
			}
			continue
		}
		if err := d.pool.Dispatch(ctx, target, routed); err != nil {
			logger.ErrorCF("routing", "route_invalid", map[string]interface{}{
				"channel":   msg.Channel,
				"chat_id":   msg.ChatID,
				"sender_id": msg.SenderID,
				"workspace": decision.Workspace,
				"mode":      decision.Runtime.Mode,
				"backend":   decision.Runtime.LocalBackend,
				"model":     decision.Runtime.LocalModel,
				"reason":    err.Error(),
			})
			d.sendDispatchError(ctx, msg, decision, err)
		}
	}
}

func (d *Dispatcher) SetJobManager(jobs *JobManager) {
	d.mu.Lock()
	d.jobs = jobs
	d.mu.Unlock()
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

func (d *Dispatcher) getJobManager() *JobManager {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.jobs
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
		"mode":          decision.Runtime.Mode,
		"backend":       decision.Runtime.LocalBackend,
		"model":         decision.Runtime.LocalModel,
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
			"This chat is not mapped to a workspace yet.\n\nEasy setup:\n  Open `sciclaw app` in your terminal, go to Routing, and add this room there.\n\nOperator CLI:\n  sciclaw routing add --channel %s --chat-id %s --workspace /absolute/path --allow <sender_id>\n\nWant unmapped rooms to use the default workspace only when sciClaw is mentioned?\n  In `sciclaw app`, change Routing or Settings -> Unmapped behavior to `mention_only`.\n\nUse `default` only if you want every unmapped room to fall back automatically.",
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

func (d *Dispatcher) sendDispatchError(ctx context.Context, msg bus.InboundMessage, decision Decision, err error) {
	if constants.IsInternalChannel(msg.Channel) {
		return
	}
	if decision.Runtime.Mode == config.ModePhi {
		content := "This room is set to local PHI mode, but the local runtime is not ready."
		if decision.Runtime.LocalModel != "" {
			content += "\nModel: " + decision.Runtime.LocalModel
		}
		if decision.Runtime.LocalBackend != "" {
			content += "\nBackend: " + decision.Runtime.LocalBackend
		}
		if detail := strings.TrimSpace(err.Error()); detail != "" {
			content += "\n\nDetails: " + detail
		}
		d.bus.PublishOutbound(ctx, bus.OutboundMessage{
			Channel: msg.Channel,
			ChatID:  msg.ChatID,
			Content: content,
		})
		return
	}
	d.sendOperationalError(ctx, msg)
}

func (d *Dispatcher) sendJobSubmitError(ctx context.Context, msg bus.InboundMessage, err error) {
	if constants.IsInternalChannel(msg.Channel) {
		return
	}
	content := "Failed to start background job"
	if detail := strings.TrimSpace(err.Error()); detail != "" {
		content += ": " + detail
	}
	d.bus.PublishOutbound(ctx, bus.OutboundMessage{
		Channel: msg.Channel,
		ChatID:  msg.ChatID,
		Content: content,
	})
}
