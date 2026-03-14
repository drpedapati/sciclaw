package bus

type InboundMessage struct {
	Channel    string            `json:"channel"`
	SenderID   string            `json:"sender_id"`
	ChatID     string            `json:"chat_id"`
	Content    string            `json:"content"`
	Media      []string          `json:"media,omitempty"`
	SessionKey string            `json:"session_key"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type OutboundAttachment struct {
	Path     string `json:"path"`
	Filename string `json:"filename,omitempty"`
}

type OutboundEmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline,omitempty"`
}

type OutboundEmbed struct {
	Title         string               `json:"title,omitempty"`
	Description   string               `json:"description,omitempty"`
	Color         int                  `json:"color,omitempty"`
	Footer        string               `json:"footer,omitempty"`
	TimestampUnix int64                `json:"timestamp_unix,omitempty"`
	Fields        []OutboundEmbedField `json:"fields,omitempty"`
}

type OutboundMessage struct {
	Channel     string               `json:"channel"`
	ChatID      string               `json:"chat_id"`
	Subject     string               `json:"subject,omitempty"`
	Content     string               `json:"content"`
	Embeds      []OutboundEmbed      `json:"embeds,omitempty"`
	Attachments []OutboundAttachment `json:"attachments,omitempty"`
}
