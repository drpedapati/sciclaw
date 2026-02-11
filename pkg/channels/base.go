package channels

import (
	"context"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/bus"
)

type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg bus.OutboundMessage) error
	IsRunning() bool
	IsAllowed(senderID string) bool
}

type BaseChannel struct {
	config    interface{}
	bus       *bus.MessageBus
	running   bool
	name      string
	allowList []string
}

func NewBaseChannel(name string, config interface{}, bus *bus.MessageBus, allowList []string) *BaseChannel {
	return &BaseChannel{
		config:    config,
		bus:       bus,
		name:      name,
		allowList: allowList,
		running:   false,
	}
}

func (c *BaseChannel) Name() string {
	return c.name
}

func (c *BaseChannel) IsRunning() bool {
	return c.running
}

func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return true
	}

	for _, allowed := range c.allowList {
		if senderID == allowed {
			return true
		}
	}

	return false
}

func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]string) {
	if !c.IsAllowed(senderID) {
		return
	}

	// 生成 SessionKey: channel:chatID
	sessionKey := fmt.Sprintf("%s:%s", c.name, chatID)

	msg := bus.InboundMessage{
		Channel:    c.name,
		SenderID:   senderID,
		ChatID:     chatID,
		Content:    content,
		Media:      media,
		Metadata:   metadata,
		SessionKey: sessionKey,
	}

	c.bus.PublishInbound(msg)
}

func (c *BaseChannel) setRunning(running bool) {
	c.running = running
}
